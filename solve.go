package boxz

import (
	"fmt"
	"sort"
)

// solve alternates layout, route selection, and physical track allocation
// until every local routing domain has enough room. It returns exact display
// paths; SVG rendering makes no further routing decisions.
func solve(doc *Document, cfg Config) (*layout, *routeResult, map[portKey]point, error) {
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		return nil, nil, nil, err
	}
	// Channel, connector, and cyclic-seam demand is only known after routing,
	// while routing needs coordinates. Allocations only grow, so the loop cannot
	// oscillate between smaller and larger layouts.
	allocated := make(map[string]int)
	for iteration := 0; iteration < len(doc.Edges)*3+8; iteration++ {
		l, err := buildLayout(doc, cfg, allocated, plan)
		if err != nil {
			return nil, nil, nil, err
		}
		routes, err := routeDocument(doc, l, plan, cfg)
		if err != nil {
			return nil, nil, nil, err
		}
		grew := false
		for domain, count := range routes.ChannelCapacity {
			if count > allocated[domain] {
				allocated[domain] = count
				grew = true
			}
		}

		ports := allocatePorts(l, routes)
		seamGrew, err := assignSeamTracks(doc, l, plan, ports, cfg)
		if err != nil {
			return nil, nil, nil, err
		}
		grew = grew || seamGrew
		if err := rerouteSeams(doc, l, plan, routes, ports, cfg); err != nil {
			return nil, nil, nil, err
		}
		for domain, count := range assignConnectorTracks(routes, plan, ports, cfg) {
			if count > allocated[domain] {
				allocated[domain] = count
				grew = true
			}
		}
		if grew {
			continue
		}

		for _, route := range routes.Edges {
			route.Display = materializeRoute(route, routes, ports, cfg)
		}
		return l, routes, ports, nil
	}
	return nil, nil, nil, fmt.Errorf("boxz: routing-space sizing did not converge")
}

type portKey struct {
	node string
	side Side
	edge int
	to   bool
}

// allocatePorts gives every routed endpoint a distinct, stable point on its
// chosen node side.
func allocatePorts(l *layout, routes *routeResult) map[portKey]point {
	type use struct {
		edge int
		to   bool
	}
	groups := make(map[string]map[Side][]use)
	for _, route := range routes.Edges {
		if groups[route.From] == nil {
			groups[route.From] = make(map[Side][]use)
		}
		if groups[route.To] == nil {
			groups[route.To] = make(map[Side][]use)
		}
		groups[route.From][route.FromSide] = append(groups[route.From][route.FromSide], use{edge: route.EdgeIndex})
		groups[route.To][route.ToSide] = append(groups[route.To][route.ToSide], use{edge: route.EdgeIndex, to: true})
	}

	// Routing chooses sides, then ports are spread evenly and deterministically
	// along each side. Measurement has already reserved enough side length.
	result := make(map[portKey]point)
	for nodeID, sides := range groups {
		placed := l.ByID[nodeID]
		if placed == nil {
			continue
		}
		for side, uses := range sides {
			sort.SliceStable(uses, func(i, j int) bool {
				if uses[i].edge != uses[j].edge {
					return uses[i].edge < uses[j].edge
				}
				return !uses[i].to && uses[j].to
			})
			for index, endpointUse := range uses {
				fraction := float64(index+1) / float64(len(uses)+1)
				var p point
				switch side {
				case North:
					p = point{X: placed.Rect.X + placed.Rect.W*fraction, Y: placed.Rect.Y}
				case South:
					p = point{X: placed.Rect.X + placed.Rect.W*fraction, Y: placed.Rect.Y + placed.Rect.H}
				case West:
					p = point{X: placed.Rect.X, Y: placed.Rect.Y + placed.Rect.H*fraction}
				case East:
					p = point{X: placed.Rect.X + placed.Rect.W, Y: placed.Rect.Y + placed.Rect.H*fraction}
				}
				result[portKey{node: nodeID, side: side, edge: endpointUse.edge, to: endpointUse.to}] = p
			}
		}
	}
	return result
}

// displayRoute is retained as the internal inspection helper used by tests.
// Solved routes already own their exact geometry.
func displayRoute(route *routedEdge, routes *routeResult, ports map[portKey]point, cfg Config) []point {
	if route.Display != nil {
		return route.Display
	}
	return materializeRoute(route, routes, ports, cfg)
}

// materializeRoute converts a domain-aware route to its assigned channel lanes
// and exact node ports without introducing lane-width jogs.
func materializeRoute(route *routedEdge, routes *routeResult, ports map[portKey]point, cfg Config) []point {
	type straightRun struct {
		a          point
		b          point
		horizontal bool
	}
	if len(route.Domains) == 0 {
		return route.Points
	}

	// Offset each maximal straight run as one unit. A run can cross a graph-edge
	// boundary without changing physical channel domain; splitting its offsets
	// there would create a meaningless lane-width jog.
	var runs []straightRun
	for first := 0; first < len(route.Domains); {
		horizontal := route.Points[first].Y == route.Points[first+1].Y
		last := first
		for last+1 < len(route.Domains) {
			nextHorizontal := route.Points[last+1].Y == route.Points[last+2].Y
			if nextHorizontal != horizontal {
				break
			}
			last++
		}

		a, b := route.Points[first], route.Points[last+1]
		offset := routeRunOffset(route, routes, first, last, cfg)
		if horizontal {
			a.Y += offset
			b.Y += offset
		} else {
			a.X += offset
			b.X += offset
		}
		runs = append(runs, straightRun{a: a, b: b, horizontal: horizontal})
		first = last + 1
	}

	points := []point{runs[0].a}
	for index := 1; index < len(runs); index++ {
		previous, current := runs[index-1], runs[index]
		joint := point{X: previous.b.X, Y: current.a.Y}
		if previous.horizontal {
			joint = point{X: current.a.X, Y: previous.b.Y}
		}
		points = append(points, joint)
	}
	points = append(points, runs[len(runs)-1].b)

	fromPort := ports[portKey{node: route.From, side: route.FromSide, edge: route.EdgeIndex}]
	toPort := ports[portKey{node: route.To, side: route.ToSide, edge: route.EdgeIndex, to: true}]
	if len(points) >= 2 {
		projection := point{X: fromPort.X, Y: points[1].Y}
		if route.FromSide == West || route.FromSide == East {
			projection = point{X: points[1].X, Y: fromPort.Y}
		}
		points = append([]point{fromPort, projection}, points[1:]...)
		last := len(points) - 1
		projection = point{X: toPort.X, Y: points[last-1].Y}
		if route.ToSide == West || route.ToSide == East {
			projection = point{X: points[last-1].X, Y: toPort.Y}
		}
		points = append(points[:last], projection, toPort)
	}
	return simplifyPoints(points)
}

func simplifyPoints(points []point) []point {
	result := make([]point, 0, len(points))
	for _, p := range points {
		result = append(result, p)
		for {
			if len(result) >= 2 && result[len(result)-2] == result[len(result)-1] {
				result = result[:len(result)-1]
				continue
			}
			if len(result) >= 3 {
				a, b, c := result[len(result)-3], result[len(result)-2], result[len(result)-1]
				if a.X == b.X && b.X == c.X || a.Y == b.Y && b.Y == c.Y {
					result[len(result)-2] = c
					result = result[:len(result)-1]
					continue
				}
			}
			break
		}
	}
	return result
}

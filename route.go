package boxz

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
)

type segment struct {
	A       point
	B       point
	channel string
}

func (s segment) horizontal() bool { return s.A.Y == s.B.Y }

type graphEdge struct {
	to      int
	length  float64
	dir     direction
	channel string
}

type routeGraph struct {
	// points and adj form the rectilinear graph; ports select legal endpoint
	// vertices for each node side.
	points []point
	adj    [][]graphEdge
	ports  map[string]map[Side]int
}

type direction uint8

const (
	dirNone direction = iota
	dirHorizontal
	dirVertical
)

type routedEdge struct {
	EdgeIndex int
	From      string
	To        string
	FromSide  Side
	ToSide    Side
	Points    []point
	Channels  []string
}

type routeResult struct {
	Edges      []*routedEdge
	LaneCounts map[string]int
	// LaneByEdge and ChannelUses together turn center-line routes into parallel
	// display lanes after routing is complete.
	LaneByEdge  map[int]map[string]int
	ChannelUses map[string][]int
}

// routeDocument routes in source order. Earlier outer routes contribute to the
// congestion tie-breaker for later ones; seam routes are already fixed by the
// topology plan and do not consume outer-channel lanes.
func routeDocument(doc *Document, l *layout, plan *routingPlan, cfg Config) (*routeResult, error) {
	graph, err := makeRouteGraph(l)
	if err != nil {
		return nil, err
	}
	result := &routeResult{
		LaneCounts:  make(map[string]int),
		LaneByEdge:  make(map[int]map[string]int),
		ChannelUses: make(map[string][]int),
	}
	usage := make(map[string]int)
	for index, edge := range doc.Edges {
		if spec := plan.Seams[index]; spec != nil {
			route, routeErr := routeThroughSeam(l, edge, index, spec, cfg)
			if routeErr != nil {
				return nil, routeErr
			}
			result.Edges = append(result.Edges, route)
			continue
		}
		route, routeErr := shortestRoute(graph, edge, index, usage)
		if routeErr != nil {
			return nil, routeErr
		}
		result.Edges = append(result.Edges, route)
		seen := make(map[string]bool)
		for _, channelName := range route.Channels {
			if channelName == "" || seen[channelName] {
				continue
			}
			seen[channelName] = true
			usage[channelName]++
			result.ChannelUses[channelName] = append(result.ChannelUses[channelName], index)
		}
	}
	for channelName, edges := range result.ChannelUses {
		result.LaneCounts[channelName] = len(edges)
		for lane, edgeIndex := range edges {
			if result.LaneByEdge[edgeIndex] == nil {
				result.LaneByEdge[edgeIndex] = make(map[string]int)
			}
			result.LaneByEdge[edgeIndex][channelName] = lane
		}
	}
	return result, nil
}

// routeThroughSeam projects both endpoints to their shared sibling gap and
// joins them on one track, or on two tracks connected by a local dogleg.
func routeThroughSeam(l *layout, edge *Edge, edgeIndex int, spec *seamSpec, cfg Config) (*routedEdge, error) {
	parent := l.ByID[spec.ParentID]
	if parent == nil || spec.FirstChild+1 >= len(parent.Children) {
		return nil, fmt.Errorf("boxz: internal routing error: seam %q is missing", spec.ID)
	}
	first, second := parent.Children[spec.FirstChild], parent.Children[spec.FirstChild+1]
	fromInFirst := containsElement(first.Element, l.ByID[edge.From].Element)
	firstNode, secondNode := l.ByID[edge.From].Element, l.ByID[edge.To].Element
	firstSide, secondSide := spec.FromSide, spec.ToSide
	if !fromInFirst {
		firstNode, secondNode = secondNode, firstNode
		firstSide, secondSide = secondSide, firstSide
	}

	// Each exposure path projects a nested endpoint outward through its
	// containers. The seam tracks then join their terminal points.
	firstPath, ok := exposurePath(first, firstNode, firstSide)
	if !ok {
		return nil, fmt.Errorf("boxz: internal routing error: %q is not exposed on side %s", firstNode.ID, firstSide)
	}
	secondPath, ok := exposurePath(second, secondNode, secondSide)
	if !ok {
		return nil, fmt.Errorf("boxz: internal routing error: %q is not exposed on side %s", secondNode.ID, secondSide)
	}

	firstOffset := trackOffset(spec.FirstTrack, spec.TrackCount, cfg.LaneSpacing)
	secondOffset := trackOffset(spec.SecondTrack, spec.TrackCount, cfg.LaneSpacing)
	firstEnd, secondEnd := firstPath[len(firstPath)-1], secondPath[len(secondPath)-1]
	points := append([]point(nil), firstPath...)
	if parent.Element.Kind == KindVBox {
		seamCenter := (first.Rect.Y + first.Rect.H + second.Rect.Y) / 2
		firstY, secondY := seamCenter+firstOffset, seamCenter+secondOffset
		points = append(points, point{X: firstEnd.X, Y: firstY})
		if spec.FirstTrack == spec.SecondTrack {
			points = append(points, point{X: secondEnd.X, Y: firstY})
		} else {
			points = append(points,
				point{X: spec.DoglegCoordinate, Y: firstY},
				point{X: spec.DoglegCoordinate, Y: secondY},
				point{X: secondEnd.X, Y: secondY})
		}
	} else {
		seamCenter := (first.Rect.X + first.Rect.W + second.Rect.X) / 2
		firstX, secondX := seamCenter+firstOffset, seamCenter+secondOffset
		points = append(points, point{X: firstX, Y: firstEnd.Y})
		if spec.FirstTrack == spec.SecondTrack {
			points = append(points, point{X: firstX, Y: secondEnd.Y})
		} else {
			points = append(points,
				point{X: firstX, Y: spec.DoglegCoordinate},
				point{X: secondX, Y: spec.DoglegCoordinate},
				point{X: secondX, Y: secondEnd.Y})
		}
	}
	reverse(secondPath)
	points = append(points, secondPath...)
	if !fromInFirst {
		reverse(points)
	}
	points = simplifyPoints(points)
	return &routedEdge{
		EdgeIndex: edgeIndex,
		From:      edge.From,
		To:        edge.To,
		FromSide:  spec.FromSide,
		ToSide:    spec.ToSide,
		Points:    points,
		Channels:  make([]string, maxInt(0, len(points)-1)),
	}, nil
}

func trackOffset(track, count int, spacing float64) float64 {
	return (float64(track) - float64(count-1)/2) * spacing
}

func rerouteSeams(doc *Document, l *layout, plan *routingPlan, routes *routeResult, cfg Config) error {
	for index, route := range routes.Edges {
		spec := plan.Seams[route.EdgeIndex]
		if spec == nil {
			continue
		}
		rebuilt, err := routeThroughSeam(l, doc.Edges[route.EdgeIndex], route.EdgeIndex, spec, cfg)
		if err != nil {
			return err
		}
		routes.Edges[index] = rebuilt
	}
	return nil
}

// exposurePath projects a frontier node through every enclosing rectangle up
// to root's requested side.
func exposurePath(root *placement, node *Element, side Side) ([]point, bool) {
	if root.Element.Kind == KindNode {
		if root.Element != node {
			return nil, false
		}
		center := root.Rect.center()
		switch side {
		case North:
			return []point{{X: center.X, Y: root.Rect.Y}}, true
		case East:
			return []point{{X: root.Rect.X + root.Rect.W, Y: center.Y}}, true
		case South:
			return []point{{X: center.X, Y: root.Rect.Y + root.Rect.H}}, true
		case West:
			return []point{{X: root.Rect.X, Y: center.Y}}, true
		}
	}

	// Follow the unique containing child and extend its path to this container's
	// matching channel, or to the rectangle boundary when that side has no
	// channel of its own.
	var child *placement
	for _, candidate := range root.Children {
		if containsElement(candidate.Element, node) {
			child = candidate
			break
		}
	}
	if child == nil {
		return nil, false
	}
	points, ok := exposurePath(child, node, side)
	if !ok {
		return nil, false
	}
	end := points[len(points)-1]
	destination := end
	if c := root.Channels[side]; c != nil {
		if side == North || side == South {
			destination.Y = c.A.Y
		} else {
			destination.X = c.A.X
		}
	} else {
		switch side {
		case North:
			destination.Y = root.Rect.Y
		case East:
			destination.X = root.Rect.X + root.Rect.W
		case South:
			destination.Y = root.Rect.Y + root.Rect.H
		case West:
			destination.X = root.Rect.X
		}
	}
	if destination != end {
		points = append(points, destination)
	}
	return points, true
}

// makeRouteGraph splits channel center lines and hierarchy risers at every
// intersection, producing the graph searched by outer routes.
func makeRouteGraph(l *layout) (*routeGraph, error) {
	// Center lines are sufficient for path finding. Physical lane offsets are a
	// display concern applied only after every route and lane count is known.
	var segments []segment
	for _, channelName := range sortedChannelIDs(l.Channels) {
		c := l.Channels[channelName]
		segments = append(segments, segment{A: c.A, B: c.B, channel: c.ID})
	}
	ports := make(map[string]map[Side]point)
	addHierarchySegments(l.Root, &segments, ports)

	// Split every segment at all crossings and overlaps. Shared coordinates then
	// become graph vertices at which a route may turn or change hierarchy level.
	pointsBySegment := make([][]point, len(segments))
	for index, s := range segments {
		if s.A == s.B {
			continue
		}
		pointsBySegment[index] = []point{s.A, s.B}
	}
	for left := range segments {
		for right := left + 1; right < len(segments); right++ {
			for _, intersection := range segmentIntersections(segments[left], segments[right]) {
				pointsBySegment[left] = appendUniquePoint(pointsBySegment[left], intersection)
				pointsBySegment[right] = appendUniquePoint(pointsBySegment[right], intersection)
			}
		}
	}

	graph := &routeGraph{ports: make(map[string]map[Side]int)}
	vertexByPoint := make(map[point]int)
	vertex := func(p point) int {
		if existing, ok := vertexByPoint[p]; ok {
			return existing
		}
		index := len(graph.points)
		vertexByPoint[p] = index
		graph.points = append(graph.points, p)
		graph.adj = append(graph.adj, nil)
		return index
	}
	for index, s := range segments {
		points := pointsBySegment[index]
		if s.horizontal() {
			sort.Slice(points, func(i, j int) bool { return points[i].X < points[j].X })
		} else {
			sort.Slice(points, func(i, j int) bool { return points[i].Y < points[j].Y })
		}
		for i := 1; i < len(points); i++ {
			from, to := vertex(points[i-1]), vertex(points[i])
			length := manhattan(points[i-1], points[i])
			if length == 0 {
				continue
			}
			dir := dirVertical
			if s.horizontal() {
				dir = dirHorizontal
			}
			graph.adj[from] = append(graph.adj[from], graphEdge{to: to, length: length, dir: dir, channel: s.channel})
			graph.adj[to] = append(graph.adj[to], graphEdge{to: from, length: length, dir: dir, channel: s.channel})
		}
	}
	for nodeID, bySide := range ports {
		graph.ports[nodeID] = make(map[Side]int)
		for side, p := range bySide {
			index, ok := vertexByPoint[p]
			if !ok {
				return nil, fmt.Errorf("boxz: internal routing error: port %s:%s is disconnected", nodeID, side)
			}
			graph.ports[nodeID][side] = index
		}
	}
	for vertexIndex := range graph.adj {
		sort.SliceStable(graph.adj[vertexIndex], func(i, j int) bool {
			left, right := graph.adj[vertexIndex][i], graph.adj[vertexIndex][j]
			lp, rp := graph.points[left.to], graph.points[right.to]
			if lp.Y != rp.Y {
				return lp.Y < rp.Y
			}
			if lp.X != rp.X {
				return lp.X < rp.X
			}
			return left.channel < right.channel
		})
	}
	return graph, nil
}

// addHierarchySegments connects child boundary portals to the channels owned by
// each parent and records node portals as legal endpoints.
func addHierarchySegments(parent *placement, segments *[]segment, ports map[string]map[Side]point) {
	if parent.Element.Kind == KindNode {
		return
	}
	// A riser connects every exposed child portal to each channel owned by the
	// parent. Recursing builds a connected channel hierarchy without allowing
	// the router to cross node interiors arbitrarily.
	for _, child := range parent.Children {
		if parent.Element.Kind == KindHBox {
			for _, side := range []Side{North, South} {
				parentChannel := parent.Channels[side]
				for _, portal := range boundaryPortals(child, side) {
					destination := point{X: portal.X, Y: parentChannel.A.Y}
					*segments = append(*segments, segment{A: portal, B: destination})
					if child.Element.Kind == KindNode {
						setPort(ports, child.Element.ID, side, portal)
					}
				}
			}
		} else {
			for _, side := range []Side{West, East} {
				parentChannel := parent.Channels[side]
				for _, portal := range boundaryPortals(child, side) {
					destination := point{X: parentChannel.A.X, Y: portal.Y}
					*segments = append(*segments, segment{A: portal, B: destination})
					if child.Element.Kind == KindNode {
						setPort(ports, child.Element.ID, side, portal)
					}
				}
			}
		}
		addHierarchySegments(child, segments, ports)
	}
}

func setPort(ports map[string]map[Side]point, nodeID string, side Side, p point) {
	if ports[nodeID] == nil {
		ports[nodeID] = make(map[Side]point)
	}
	ports[nodeID][side] = p
}

// boundaryPortals returns the points through which a child can meet a parent
// channel on side.
func boundaryPortals(child *placement, side Side) []point {
	if child.Element.Kind == KindNode {
		center := child.Rect.center()
		switch side {
		case North:
			return []point{{X: center.X, Y: child.Rect.Y}}
		case South:
			return []point{{X: center.X, Y: child.Rect.Y + child.Rect.H}}
		case West:
			return []point{{X: child.Rect.X, Y: center.Y}}
		case East:
			return []point{{X: child.Rect.X + child.Rect.W, Y: center.Y}}
		}
	}
	// Prefer the child's matching channel. When the child owns only orthogonal
	// channels, their extreme endpoints form the portals on this boundary.
	if matching := child.Channels[side]; matching != nil {
		return []point{{X: (matching.A.X + matching.B.X) / 2, Y: (matching.A.Y + matching.B.Y) / 2}}
	}

	var result []point
	for _, channelSide := range []Side{North, East, South, West} {
		c := child.Channels[channelSide]
		if c == nil {
			continue
		}
		if side == North || side == South {
			p := c.A
			if (side == North && c.B.Y < p.Y) || (side == South && c.B.Y > p.Y) {
				p = c.B
			}
			result = append(result, p)
		} else {
			p := c.A
			if (side == West && c.B.X < p.X) || (side == East && c.B.X > p.X) {
				p = c.B
			}
			result = append(result, p)
		}
	}
	return result
}

func segmentIntersections(a, b segment) []point {
	if a.horizontal() != b.horizontal() {
		horizontal, vertical := a, b
		if !horizontal.horizontal() {
			horizontal, vertical = b, a
		}
		p := point{X: vertical.A.X, Y: horizontal.A.Y}
		if pointOnSegment(p, horizontal) && pointOnSegment(p, vertical) {
			return []point{p}
		}
		return nil
	}
	if a.horizontal() {
		if a.A.Y != b.A.Y {
			return nil
		}
	} else if a.A.X != b.A.X {
		return nil
	}
	var result []point
	for _, p := range []point{a.A, a.B, b.A, b.B} {
		if pointOnSegment(p, a) && pointOnSegment(p, b) {
			result = appendUniquePoint(result, p)
		}
	}
	return result
}

func pointOnSegment(p point, s segment) bool {
	const epsilon = 1e-9
	return p.X >= math.Min(s.A.X, s.B.X)-epsilon && p.X <= math.Max(s.A.X, s.B.X)+epsilon &&
		p.Y >= math.Min(s.A.Y, s.B.Y)-epsilon && p.Y <= math.Max(s.A.Y, s.B.Y)+epsilon &&
		(math.Abs(s.A.X-s.B.X) < epsilon && math.Abs(p.X-s.A.X) < epsilon ||
			math.Abs(s.A.Y-s.B.Y) < epsilon && math.Abs(p.Y-s.A.Y) < epsilon)
}

func appendUniquePoint(points []point, candidate point) []point {
	for _, existing := range points {
		if existing == candidate {
			return points
		}
	}
	return append(points, candidate)
}

func sortedChannelIDs(channels map[string]*channel) []string {
	ids := make([]string, 0, len(channels))
	for id := range channels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type routeCost struct {
	distance   float64
	bends      int
	congestion int
}

// less defines the routing policy lexicographically. Distance dominates bends,
// and bends dominate the weak preference for less-used channels.
func (c routeCost) less(other routeCost) bool {
	if c.distance != other.distance {
		return c.distance < other.distance
	}
	if c.bends != other.bends {
		return c.bends < other.bends
	}
	return c.congestion < other.congestion
}

func (c routeCost) equal(other routeCost) bool {
	return c.distance == other.distance && c.bends == other.bends && c.congestion == other.congestion
}

type routeState struct {
	// Direction belongs in the state because reaching one point horizontally is
	// not equivalent to reaching it vertically when the next bend has a cost.
	vertex int
	dir    direction
}

type previousStep struct {
	state routeState
	edge  graphEdge
}

type queueItem struct {
	state routeState
	cost  routeCost
	order int
}

type routeQueue []*queueItem

func (q routeQueue) Len() int { return len(q) }
func (q routeQueue) Less(i, j int) bool {
	if q[i].cost.equal(q[j].cost) {
		return q[i].order < q[j].order
	}
	return q[i].cost.less(q[j].cost)
}
func (q routeQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *routeQueue) Push(value any) {
	*q = append(*q, value.(*queueItem))
}
func (q *routeQueue) Pop() any {
	old := *q
	item := old[len(old)-1]
	*q = old[:len(old)-1]
	return item
}

// shortestRoute searches every legal source side; each search may terminate on
// any legal destination side.
func shortestRoute(graph *routeGraph, edge *Edge, edgeIndex int, usage map[string]int) (*routedEdge, error) {
	fromSides := endpointSides(graph, edge.From, edge.FromSide)
	toSides := endpointSides(graph, edge.To, edge.ToSide)
	if len(fromSides) == 0 || len(toSides) == 0 {
		return nil, fmt.Errorf("boxz: cannot route edge %s -> %s: endpoint has no channel", edge.From, edge.To)
	}

	// Side constraints reduce these sets to one. Otherwise, try every legal
	// source side and let the same cost ordering choose both endpoints.
	var best *routedEdge
	var bestCost routeCost
	for _, fromSide := range fromSides {
		candidate, cost, ok := dijkstra(graph, graph.ports[edge.From][fromSide], edge.To, toSides, usage)
		if !ok {
			continue
		}
		candidate.EdgeIndex = edgeIndex
		candidate.From = edge.From
		candidate.To = edge.To
		candidate.FromSide = fromSide
		if best == nil || cost.less(bestCost) {
			best, bestCost = candidate, cost
		}
	}
	if best == nil {
		return nil, fmt.Errorf("boxz: no channel route from %q to %q", edge.From, edge.To)
	}
	return best, nil
}

func endpointSides(graph *routeGraph, nodeID string, constrained *Side) []Side {
	if constrained != nil {
		if _, ok := graph.ports[nodeID][*constrained]; ok {
			return []Side{*constrained}
		}
		return nil
	}
	var result []Side
	for _, side := range []Side{North, East, South, West} {
		if _, ok := graph.ports[nodeID][side]; ok {
			result = append(result, side)
		}
	}
	return result
}

// dijkstra finds the lexicographically cheapest path from start to any target
// side while retaining incoming direction as part of the state.
func dijkstra(graph *routeGraph, start int, targetNode string, targetSides []Side, usage map[string]int) (*routedEdge, routeCost, bool) {
	targetByVertex := make(map[int]Side)
	for _, side := range targetSides {
		targetByVertex[graph.ports[targetNode][side]] = side
	}
	startState := routeState{vertex: start, dir: dirNone}
	distance := map[routeState]routeCost{startState: {}}
	previous := make(map[routeState]previousStep)
	// Adjacency and endpoint sides are sorted before search. The monotonically
	// increasing order field therefore makes equal-cost selection reproducible.
	queue := &routeQueue{}
	heap.Init(queue)
	order := 0
	heap.Push(queue, &queueItem{state: startState, order: order})

	var destination routeState
	var destinationSide Side
	found := false
	for queue.Len() != 0 {
		item := heap.Pop(queue).(*queueItem)
		known, ok := distance[item.state]
		if !ok || !known.equal(item.cost) {
			continue
		}
		if side, isTarget := targetByVertex[item.state.vertex]; isTarget && item.state != startState {
			destination, destinationSide, found = item.state, side, true
			break
		}
		for _, next := range graph.adj[item.state.vertex] {
			cost := item.cost
			cost.distance += next.length
			if item.state.dir != dirNone && item.state.dir != next.dir {
				cost.bends++
			}
			if next.channel != "" {
				cost.congestion += usage[next.channel]
			}
			nextState := routeState{vertex: next.to, dir: next.dir}
			old, visited := distance[nextState]
			if visited && !cost.less(old) {
				continue
			}
			distance[nextState] = cost
			previous[nextState] = previousStep{state: item.state, edge: next}
			order++
			heap.Push(queue, &queueItem{state: nextState, cost: cost, order: order})
		}
	}
	if !found {
		return nil, routeCost{}, false
	}

	var reversePoints []point
	var reverseChannels []string
	for state := destination; state != startState; {
		reversePoints = append(reversePoints, graph.points[state.vertex])
		step := previous[state]
		reverseChannels = append(reverseChannels, step.edge.channel)
		state = step.state
	}
	reversePoints = append(reversePoints, graph.points[start])
	reverse(reversePoints)
	reverse(reverseChannels)
	return &routedEdge{ToSide: destinationSide, Points: reversePoints, Channels: reverseChannels}, distance[destination], true
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func manhattan(a, b point) float64 { return math.Abs(a.X-b.X) + math.Abs(a.Y-b.Y) }

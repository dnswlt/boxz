package boxz

import (
	"regexp"
	"strconv"
	"testing"
)

const exampleSource = `
vbox root {
  hbox services {
    node client "Client"
    node api "API Server"
  }
  node database
}
edges {
  client -> api
  api -> database
}
`

var pathPattern = regexp.MustCompile(`<path class="boxz-edge"[^>]* d="([^"]+)"`)

var coordinatePattern = regexp.MustCompile(`(?:M|L) ([0-9.]+) ([0-9.]+)`)

func assertOrthogonalPaths(t *testing.T, svg string) {
	t.Helper()
	paths := pathPattern.FindAllStringSubmatch(svg, -1)
	if len(paths) == 0 {
		t.Fatal("no edge paths found")
	}
	for _, path := range paths {
		coordinates := coordinatePattern.FindAllStringSubmatch(path[1], -1)
		for index := 1; index < len(coordinates); index++ {
			previousX, _ := strconv.ParseFloat(coordinates[index-1][1], 64)
			previousY, _ := strconv.ParseFloat(coordinates[index-1][2], 64)
			currentX, _ := strconv.ParseFloat(coordinates[index][1], 64)
			currentY, _ := strconv.ParseFloat(coordinates[index][2], 64)
			if previousX != currentX && previousY != currentY {
				t.Fatalf("non-orthogonal segment in path %q: (%g,%g) to (%g,%g)", path[1], previousX, previousY, currentX, currentY)
			}
		}
	}
}

func assertNoCollinearEdgeOverlaps(t *testing.T, svg string) {
	t.Helper()
	paths := pathPattern.FindAllStringSubmatch(svg, -1)
	for left := range paths {
		leftPoints := coordinatePattern.FindAllStringSubmatch(paths[left][1], -1)
		for right := left + 1; right < len(paths); right++ {
			rightPoints := coordinatePattern.FindAllStringSubmatch(paths[right][1], -1)
			for leftSegment := 1; leftSegment < len(leftPoints); leftSegment++ {
				lx1, _ := strconv.ParseFloat(leftPoints[leftSegment-1][1], 64)
				ly1, _ := strconv.ParseFloat(leftPoints[leftSegment-1][2], 64)
				lx2, _ := strconv.ParseFloat(leftPoints[leftSegment][1], 64)
				ly2, _ := strconv.ParseFloat(leftPoints[leftSegment][2], 64)
				for rightSegment := 1; rightSegment < len(rightPoints); rightSegment++ {
					rx1, _ := strconv.ParseFloat(rightPoints[rightSegment-1][1], 64)
					ry1, _ := strconv.ParseFloat(rightPoints[rightSegment-1][2], 64)
					rx2, _ := strconv.ParseFloat(rightPoints[rightSegment][1], 64)
					ry2, _ := strconv.ParseFloat(rightPoints[rightSegment][2], 64)
					verticalOverlap := lx1 == lx2 && rx1 == rx2 && lx1 == rx1 &&
						intervalOverlap(ly1, ly2, ry1, ry2) > 1e-9
					horizontalOverlap := ly1 == ly2 && ry1 == ry2 && ly1 == ry1 &&
						intervalOverlap(lx1, lx2, rx1, rx2) > 1e-9
					if verticalOverlap || horizontalOverlap {
						t.Fatalf("collinear overlap between paths %q and %q", paths[left][1], paths[right][1])
					}
				}
			}
		}
	}
}

func assertNoNodeRectOverlaps(t *testing.T, l *layout) {
	t.Helper()
	var nodes []*placement
	collectNodePlacements(l.Root, &nodes)
	for left := range nodes {
		for right := left + 1; right < len(nodes); right++ {
			if rectInteriorsOverlap(nodes[left].Rect, nodes[right].Rect) {
				t.Fatalf("nodes %q and %q overlap: %#v and %#v",
					nodes[left].Element.ID, nodes[right].Element.ID, nodes[left].Rect, nodes[right].Rect)
			}
		}
	}
}

func assertRoutesAvoidOtherNodes(t *testing.T, l *layout, routes *routeResult, ports map[portKey]point, cfg Config) {
	t.Helper()
	var nodes []*placement
	collectNodePlacements(l.Root, &nodes)
	for _, route := range routes.Edges {
		points := displayRoute(route, routes, ports, cfg)
		for index := 1; index < len(points); index++ {
			for _, node := range nodes {
				if node.Element.ID == route.From || node.Element.ID == route.To {
					continue
				}
				if segmentEntersRectInterior(points[index-1], points[index], node.Rect) {
					t.Fatalf("route %s -> %s segment %#v -> %#v enters node %q at %#v",
						route.From, route.To, points[index-1], points[index], node.Element.ID, node.Rect)
				}
			}
		}
	}
}

func assertRoutesMeetPortsCleanly(t *testing.T, l *layout, routes *routeResult, ports map[portKey]point, cfg Config) {
	t.Helper()
	for _, route := range routes.Edges {
		points := displayRoute(route, routes, ports, cfg)
		if len(points) < 2 {
			t.Fatalf("route %s -> %s has fewer than two display points: %v", route.From, route.To, points)
		}
		fromPort := ports[portKey{node: route.From, side: route.FromSide, edge: route.EdgeIndex}]
		toPort := ports[portKey{node: route.To, side: route.ToSide, edge: route.EdgeIndex, to: true}]
		if points[0] != fromPort || points[len(points)-1] != toPort {
			t.Fatalf("display route %s -> %s endpoints = %v/%v, want ports %v/%v",
				route.From, route.To, points[0], points[len(points)-1], fromPort, toPort)
		}
		if !endpointSegmentIsPerpendicular(fromPort, points[1], l.ByID[route.From].Rect, route.FromSide) {
			t.Fatalf("route %s -> %s leaves side %s along the node border: %v",
				route.From, route.To, route.FromSide, points[:2])
		}
		last := len(points) - 1
		if !endpointSegmentIsPerpendicular(toPort, points[last-1], l.ByID[route.To].Rect, route.ToSide) {
			t.Fatalf("route %s -> %s enters side %s along the node border: %v",
				route.From, route.To, route.ToSide, points[last-1:])
		}
	}
}

func assertNoDisplayRouteOverlaps(t *testing.T, routes *routeResult, ports map[portKey]point, cfg Config, tolerance float64) {
	t.Helper()
	display := make([][]point, len(routes.Edges))
	for index, route := range routes.Edges {
		display[index] = displayRoute(route, routes, ports, cfg)
	}
	for left := range display {
		for right := left + 1; right < len(display); right++ {
			for leftIndex := 1; leftIndex < len(display[left]); leftIndex++ {
				la, lb := display[left][leftIndex-1], display[left][leftIndex]
				for rightIndex := 1; rightIndex < len(display[right]); rightIndex++ {
					ra, rb := display[right][rightIndex-1], display[right][rightIndex]
					vertical := sameCoordinate(la.X, lb.X) && sameCoordinate(ra.X, rb.X) && sameCoordinate(la.X, ra.X) &&
						intervalOverlap(la.Y, lb.Y, ra.Y, rb.Y) > tolerance
					horizontal := sameCoordinate(la.Y, lb.Y) && sameCoordinate(ra.Y, rb.Y) && sameCoordinate(la.Y, ra.Y) &&
						intervalOverlap(la.X, lb.X, ra.X, rb.X) > tolerance
					if vertical || horizontal {
						t.Fatalf("display routes %d and %d overlap: %v and %v", left, right, display[left], display[right])
					}
				}
			}
		}
	}
}

func endpointSegmentIsPerpendicular(port, outside point, node rect, side Side) bool {
	return endpointLegIsPerpendicular(port, outside, node, side)
}

func collectNodePlacements(p *placement, nodes *[]*placement) {
	if p.Element.Kind == KindNode {
		*nodes = append(*nodes, p)
		return
	}
	for _, child := range p.Children {
		collectNodePlacements(child, nodes)
	}
}

func rectInteriorsOverlap(left, right rect) bool {
	return intervalOverlap(left.X, left.X+left.W, right.X, right.X+right.W) > 1e-9 &&
		intervalOverlap(left.Y, left.Y+left.H, right.Y, right.Y+right.H) > 1e-9
}

func segmentEntersRectInterior(a, b point, r rect) bool {
	if a.X == b.X {
		return a.X > r.X+1e-9 && a.X < r.X+r.W-1e-9 &&
			intervalOverlap(a.Y, b.Y, r.Y, r.Y+r.H) > 1e-9
	}
	if a.Y == b.Y {
		return a.Y > r.Y+1e-9 && a.Y < r.Y+r.H-1e-9 &&
			intervalOverlap(a.X, b.X, r.X, r.X+r.W) > 1e-9
	}
	return true
}

func intervalOverlap(a1, a2, b1, b2 float64) float64 {
	if a1 > a2 {
		a1, a2 = a2, a1
	}
	if b1 > b2 {
		b1, b2 = b2, b1
	}
	left, right := a1, a2
	if b1 > left {
		left = b1
	}
	if b2 < right {
		right = b2
	}
	return right - left
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

package boxz

import "math"

const maxShortcutSegments = 5

type routeRefiner struct {
	l          *layout
	routes     *routeResult
	nodes      []*placement
	boundaries []rect
	clearance  float64
}

type polylineScore struct {
	bends  int
	length float64
}

type refinementRule func([]point, int, int) [][]point

var refinementRules = []refinementRule{minimalManhattanRule}

// refineDisplayRoutes is a deliberately local post-routing pass. It can remove
// metric artifacts introduced by lane allocation, but it cannot change ports,
// bounded-region crossings, or any layout capacity.
func refineDisplayRoutes(l *layout, routes *routeResult, cfg Config) {
	refiner := routeRefiner{l: l, routes: routes, clearance: math.Max(1, cfg.LaneSpacing)}
	collectRefinementGeometry(l.Root, &refiner.nodes, &refiner.boundaries)
	for _, route := range routes.Edges {
		refiner.refine(route)
	}
}

func collectRefinementGeometry(p *placement, nodes *[]*placement, boundaries *[]rect) {
	if p.Element.Kind == KindNode {
		*nodes = append(*nodes, p)
		return
	}
	if p.Element.ContainerAttributes.Bounded {
		*boundaries = append(*boundaries, p.Rect)
	}
	for _, child := range p.Children {
		collectRefinementGeometry(child, nodes, boundaries)
	}
}

// refine applies the best local shortcut available, then rescans. Every
// accepted rewrite strictly improves a finite polyline score, so this reaches
// a deterministic fixed point without enumerating combinations of rewrites.
func (r *routeRefiner) refine(route *routedEdge) {
	for {
		current := route.Display
		best := current
		bestScore := scorePolyline(current)
		for first := 0; first+2 < len(current); first++ {
			lastLimit := first + maxShortcutSegments
			if lastLimit >= len(current) {
				lastLimit = len(current) - 1
			}
			for last := first + 2; last <= lastLimit; last++ {
				for _, rule := range refinementRules {
					for _, replacement := range rule(current, first, last) {
						candidate := replacePolylineSpan(current, first, last, replacement)
						candidateScore := scorePolyline(candidate)
						if !candidateScore.betterThan(bestScore) ||
							!r.canReplace(route, current, candidate) {
							continue
						}
						best, bestScore = candidate, candidateScore
					}
				}
			}
		}
		if samePolyline(best, current) {
			return
		}
		route.Display = best
	}
}

// minimalManhattanRule returns only the two minimal rectilinear paths between
// the window endpoints. Coordinates therefore come from existing vertices;
// this pass never searches arbitrary free-space coordinates.
func minimalManhattanRule(points []point, first, last int) [][]point {
	a, b := points[first], points[last]
	if sameCoordinate(a.X, b.X) || sameCoordinate(a.Y, b.Y) {
		return [][]point{{a, b}}
	}
	return [][]point{
		{a, {X: b.X, Y: a.Y}, b},
		{a, {X: a.X, Y: b.Y}, b},
	}
}

func replacePolylineSpan(points []point, first, last int, replacement []point) []point {
	result := make([]point, 0, len(points)-(last-first)+len(replacement)-1)
	result = append(result, points[:first]...)
	result = append(result, replacement...)
	result = append(result, points[last+1:]...)
	return simplifyPoints(result)
}

func scorePolyline(points []point) polylineScore {
	score := polylineScore{}
	if len(points) > 2 {
		score.bends = len(points) - 2
	}
	for index := 1; index < len(points); index++ {
		score.length += segmentLength(points[index-1], points[index])
	}
	return score
}

func (s polylineScore) betterThan(other polylineScore) bool {
	return s.bends < other.bends || s.bends == other.bends && s.length < other.length-1e-9
}

func (r *routeRefiner) canReplace(route *routedEdge, current, candidate []point) bool {
	if len(candidate) < 2 || !samePoint(candidate[0], current[0]) ||
		!samePoint(candidate[len(candidate)-1], current[len(current)-1]) {
		return false
	}
	if !r.validEndpointLegs(route, current, candidate) || !orthogonalPolyline(candidate) {
		return false
	}
	for _, boundary := range r.boundaries {
		if !sameBoundaryCrossings(current, candidate, boundary) {
			return false
		}
	}
	if !r.routeAvoidsNodes(route, candidate) {
		return false
	}
	if polylineSelfIntersects(candidate, r.clearance) {
		return false
	}
	for index := 1; index < len(candidate); index++ {
		a, b := candidate[index-1], candidate[index]
		for _, other := range r.routes.Edges {
			if other == route {
				continue
			}
			for otherIndex := 1; otherIndex < len(other.Display); otherIndex++ {
				if displaySegmentsConflict(a, b, other.Display[otherIndex-1], other.Display[otherIndex], r.clearance, true) {
					return false
				}
			}
		}
	}
	return true
}

func (r *routeRefiner) routeAvoidsNodes(route *routedEdge, points []point) bool {
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		for _, node := range r.nodes {
			if segmentEntersBoxInterior(a, b, node.Rect) {
				return false
			}
			trace := traceBoundary([]point{a, b}, node.Rect)
			if trace.collinear {
				return false
			}
			for _, contact := range trace.points {
				fromPort := index == 1 && node.Element.ID == route.From && samePoint(contact, points[0])
				toPort := index == len(points)-1 && node.Element.ID == route.To && samePoint(contact, points[len(points)-1])
				if !fromPort && !toPort {
					return false
				}
			}
		}
	}
	return true
}

func (r *routeRefiner) validEndpointLegs(route *routedEdge, current, candidate []point) bool {
	from := r.l.ByID[route.From]
	to := r.l.ByID[route.To]
	if from == nil || to == nil || len(current) < 2 || len(candidate) < 2 {
		return false
	}
	currentLast, candidateLast := len(current)-1, len(candidate)-1
	if !endpointLegIsPerpendicular(candidate[0], candidate[1], from.Rect, route.FromSide) ||
		!endpointLegIsPerpendicular(candidate[candidateLast], candidate[candidateLast-1], to.Rect, route.ToSide) {
		return false
	}
	// Arrowheads and node exits should not lose their existing clearance merely
	// as a side effect of simplifying a more distant part of the route.
	return segmentLength(candidate[0], candidate[1])+1e-9 >= segmentLength(current[0], current[1]) &&
		segmentLength(candidate[candidateLast-1], candidate[candidateLast])+1e-9 >=
			segmentLength(current[currentLast-1], current[currentLast])
}

func endpointLegIsPerpendicular(port, outside point, node rect, side Side) bool {
	switch side {
	case North:
		return sameCoordinate(port.Y, node.Y) && sameCoordinate(port.X, outside.X) && outside.Y < port.Y
	case East:
		return sameCoordinate(port.X, node.X+node.W) && sameCoordinate(port.Y, outside.Y) && outside.X > port.X
	case South:
		return sameCoordinate(port.Y, node.Y+node.H) && sameCoordinate(port.X, outside.X) && outside.Y > port.Y
	case West:
		return sameCoordinate(port.X, node.X) && sameCoordinate(port.Y, outside.Y) && outside.X < port.X
	default:
		return false
	}
}

func orthogonalPolyline(points []point) bool {
	for index := 1; index < len(points); index++ {
		if !sameCoordinate(points[index-1].X, points[index].X) &&
			!sameCoordinate(points[index-1].Y, points[index].Y) {
			return false
		}
	}
	return true
}

func segmentEntersBoxInterior(a, b point, box rect) bool {
	if sameCoordinate(a.X, b.X) {
		return a.X > box.X+1e-9 && a.X < box.X+box.W-1e-9 &&
			openIntervalOverlap(a.Y, b.Y, box.Y, box.Y+box.H) > 1e-9
	}
	if sameCoordinate(a.Y, b.Y) {
		return a.Y > box.Y+1e-9 && a.Y < box.Y+box.H-1e-9 &&
			openIntervalOverlap(a.X, b.X, box.X, box.X+box.W) > 1e-9
	}
	return true
}

func polylineSelfIntersects(points []point, clearance float64) bool {
	for first := 1; first < len(points); first++ {
		for second := first + 2; second < len(points); second++ {
			if displaySegmentsConflict(points[first-1], points[first], points[second-1], points[second], clearance, false) {
				return true
			}
		}
	}
	return false
}

// displaySegmentsConflict permits only a proper perpendicular crossing between
// different edges. Collinear endpoints count as occupied, preventing two
// half-domain uses from appearing to join at one point.
func displaySegmentsConflict(a, b, c, d point, clearance float64, allowCrossing bool) bool {
	aVertical := sameCoordinate(a.X, b.X)
	cVertical := sameCoordinate(c.X, d.X)
	if aVertical == cVertical {
		var crossDistance, alongDistance float64
		if aVertical {
			crossDistance = math.Abs(a.X - c.X)
			alongDistance = intervalDistance(a.Y, b.Y, c.Y, d.Y)
		} else {
			crossDistance = math.Abs(a.Y - c.Y)
			alongDistance = intervalDistance(a.X, b.X, c.X, d.X)
		}
		return crossDistance < clearance-1e-9 && alongDistance < clearance-1e-9
	}

	verticalA, verticalB, horizontalA, horizontalB := a, b, c, d
	if !aVertical {
		verticalA, verticalB, horizontalA, horizontalB = c, d, a, b
	}
	intersection := point{X: verticalA.X, Y: horizontalA.Y}
	if !coordinateWithin(intersection.Y, verticalA.Y, verticalB.Y) ||
		!coordinateWithin(intersection.X, horizontalA.X, horizontalB.X) {
		return false
	}
	if !allowCrossing {
		return true
	}
	return pointIsSegmentEndpoint(intersection, verticalA, verticalB) ||
		pointIsSegmentEndpoint(intersection, horizontalA, horizontalB)
}

func intervalDistance(a1, a2, b1, b2 float64) float64 {
	aLow, aHigh := math.Min(a1, a2), math.Max(a1, a2)
	bLow, bHigh := math.Min(b1, b2), math.Max(b1, b2)
	if aHigh < bLow {
		return bLow - aHigh
	}
	if bHigh < aLow {
		return aLow - bHigh
	}
	return 0
}

func coordinateWithin(value, first, second float64) bool {
	return value >= math.Min(first, second)-1e-9 && value <= math.Max(first, second)+1e-9
}

func pointIsSegmentEndpoint(p, a, b point) bool {
	return samePoint(p, a) || samePoint(p, b)
}

func samePoint(a, b point) bool {
	return sameCoordinate(a.X, b.X) && sameCoordinate(a.Y, b.Y)
}

func segmentLength(a, b point) float64 {
	return math.Abs(a.X-b.X) + math.Abs(a.Y-b.Y)
}

func openIntervalOverlap(a1, a2, b1, b2 float64) float64 {
	return math.Min(math.Max(a1, a2), math.Max(b1, b2)) - math.Max(math.Min(a1, a2), math.Min(b1, b2))
}

type boundaryTrace struct {
	points    []point
	collinear bool
}

func sameBoundaryCrossings(first, second []point, boundary rect) bool {
	a, b := traceBoundary(first, boundary), traceBoundary(second, boundary)
	if a.collinear || b.collinear || len(a.points) != len(b.points) {
		return false
	}
	for index := range a.points {
		if !sameCoordinate(a.points[index].X, b.points[index].X) ||
			!sameCoordinate(a.points[index].Y, b.points[index].Y) {
			return false
		}
	}
	return true
}

func traceBoundary(points []point, boundary rect) boundaryTrace {
	trace := boundaryTrace{}
	appendPoint := func(p point) {
		if len(trace.points) == 0 || !samePoint(trace.points[len(trace.points)-1], p) {
			trace.points = append(trace.points, p)
		}
	}
	for index := 1; index < len(points); index++ {
		a, b := points[index-1], points[index]
		if sameCoordinate(a.X, b.X) {
			if (sameCoordinate(a.X, boundary.X) || sameCoordinate(a.X, boundary.X+boundary.W)) &&
				openIntervalOverlap(a.Y, b.Y, boundary.Y, boundary.Y+boundary.H) > 1e-9 {
				trace.collinear = true
				continue
			}
			if !coordinateWithin(a.X, boundary.X, boundary.X+boundary.W) {
				continue
			}
			values := []float64{boundary.Y, boundary.Y + boundary.H}
			if b.Y < a.Y {
				values[0], values[1] = values[1], values[0]
			}
			for _, y := range values {
				if coordinateWithin(y, a.Y, b.Y) {
					appendPoint(point{X: a.X, Y: y})
				}
			}
			continue
		}
		if sameCoordinate(a.Y, b.Y) {
			if (sameCoordinate(a.Y, boundary.Y) || sameCoordinate(a.Y, boundary.Y+boundary.H)) &&
				openIntervalOverlap(a.X, b.X, boundary.X, boundary.X+boundary.W) > 1e-9 {
				trace.collinear = true
				continue
			}
			if !coordinateWithin(a.Y, boundary.Y, boundary.Y+boundary.H) {
				continue
			}
			values := []float64{boundary.X, boundary.X + boundary.W}
			if b.X < a.X {
				values[0], values[1] = values[1], values[0]
			}
			for _, x := range values {
				if coordinateWithin(x, a.X, b.X) {
					appendPoint(point{X: x, Y: a.Y})
				}
			}
		}
	}
	return trace
}

func samePolyline(first, second []point) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if !samePoint(first[index], second[index]) {
			return false
		}
	}
	return true
}

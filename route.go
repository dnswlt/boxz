package boxz

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
)

type segment struct {
	A point
	B point
	// resource identifies this edge in the pathfinding graph and congestion
	// history. domain identifies the physical space that allocates tracks.
	// They usually match for channels, but several graph connectors may share
	// one physical domain. Prescribed seam paths can claim a domain without
	// having a graph resource at all.
	resource string
	domain   string
	kind     segmentKind
}

func (s segment) horizontal() bool { return s.A.Y == s.B.Y }

type segmentKind uint8

const (
	segmentChannel segmentKind = iota
	segmentRiser
	segmentCrossbar
)

type graphEdge struct {
	to       int
	length   float64
	dir      direction
	resource string
	domain   string
}

type routeGraph struct {
	// points and adj form the rectilinear graph; ports select legal endpoint
	// vertices for each node side.
	points     []point
	adj        [][]graphEdge
	ports      map[string]map[Side]int
	connectors map[string]connectorSpec
	domains    map[string]connectorDomain
	segments   []segment
}

type connectorKind uint8

const (
	connectorHierarchy connectorKind = iota
	connectorSibling
)

// connectorSpec describes one representative graph edge through a movable
// connector domain. Bounds are use-specific because different graph portals
// in one sibling gap can expose different intervals.
type connectorSpec struct {
	ResourceID string
	DomainID   string
	Kind       connectorKind
	Horizontal bool
	Low        float64
	High       float64
}

// connectorDomain owns physical track allocation for a shared region. A
// hierarchy domain can request more cross-axis room from its child container;
// sibling domains live in an already measured seam.
type connectorDomain struct {
	ID         string
	Kind       connectorKind
	Horizontal bool
	Low        float64
	High       float64
	ChildID    string
	Side       Side
}

type hierarchyPortal struct {
	point
	// continuation names an orthogonal child channel that this riser extends
	// collinearly. Sharing that resource keeps its lane offset consistent.
	continuation string
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
	// Resources and Domains are parallel to the segments between Points.
	// Resources describe graph traversal; Domains describe physical occupancy.
	Resources []string
	Domains   []string
	Display   []point
}

type routeResult struct {
	Edges           []*routedEdge
	ChannelCapacity map[string]int
	// LaneByEdge and ChannelUses turn channel-domain center lines into parallel
	// display lanes. Movable connectors are allocated separately.
	LaneByEdge  map[int]map[string]int
	ChannelUses map[string][]int
	// ResourceUses records graph use for congestion diagnostics. ConnectorUses
	// includes prescribed seam occupancy as well as searched outer routes.
	ResourceUses  map[string][]int
	ConnectorUses map[string][]int
	Connectors    map[string]connectorSpec
	Domains       map[string]connectorDomain
	// Segments retains the center-line graph solely for optional debug output.
	Segments []segment
}

// routeDocument routes in source order. Earlier outer routes contribute to the
// congestion tie-breaker for later ones. Seam routes are prescribed by the
// topology plan; after exact ports exist they still claim any connector domains
// crossed by their exposure paths.
func routeDocument(doc *Document, l *layout, plan *routingPlan, cfg Config) (*routeResult, error) {
	graph, err := makeRouteGraph(l)
	if err != nil {
		return nil, err
	}
	result := &routeResult{
		ChannelCapacity: make(map[string]int),
		LaneByEdge:      make(map[int]map[string]int),
		ChannelUses:     make(map[string][]int),
		ResourceUses:    make(map[string][]int),
		ConnectorUses:   make(map[string][]int),
		Connectors:      graph.connectors,
		Domains:         graph.domains,
	}
	if cfg.Debug {
		result.Segments = graph.segments
	}
	usage := make(map[string]int)
	for index, edge := range doc.Edges {
		if spec := plan.Seams[index]; spec != nil {
			fromStart := sideCenter(l.ByID[edge.From].Rect, spec.FromSide)
			toStart := sideCenter(l.ByID[edge.To].Rect, spec.ToSide)
			route, routeErr := routeThroughSeam(l, edge, index, spec, fromStart, toStart, cfg)
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
		seenResources := make(map[string]bool)
		seenChannels := make(map[string]bool)
		for segmentIndex, resource := range route.Resources {
			if resource != "" && !seenResources[resource] {
				seenResources[resource] = true
				usage[resource]++
				result.ResourceUses[resource] = append(result.ResourceUses[resource], index)
			}
			domain := route.Domains[segmentIndex]
			if domain == "" || seenChannels[domain] {
				continue
			}
			if _, connector := graph.domains[domain]; connector {
				continue
			}
			seenChannels[domain] = true
			result.ChannelUses[domain] = append(result.ChannelUses[domain], index)
		}
	}
	for domain, edges := range result.ChannelUses {
		result.ChannelCapacity[domain] = len(edges)
		for lane, edgeIndex := range edges {
			if result.LaneByEdge[edgeIndex] == nil {
				result.LaneByEdge[edgeIndex] = make(map[string]int)
			}
			result.LaneByEdge[edgeIndex][domain] = lane
		}
	}
	return result, nil
}

// routeThroughSeam projects both endpoints to their shared sibling gap and
// joins them on one track, or on two tracks connected by a local dogleg.
func routeThroughSeam(l *layout, edge *Edge, edgeIndex int, spec *seamSpec, fromStart, toStart point, cfg Config) (*routedEdge, error) {
	parent := l.ByID[spec.ParentID]
	if parent == nil || spec.FirstChild+1 >= len(parent.Children) {
		return nil, fmt.Errorf("boxz: internal routing error: seam %q is missing", spec.ID)
	}
	first, second := parent.Children[spec.FirstChild], parent.Children[spec.FirstChild+1]
	fromInFirst := containsElement(first.Element, l.ByID[edge.From].Element)
	firstNode, secondNode := l.ByID[edge.From].Element, l.ByID[edge.To].Element
	firstSide, secondSide := spec.FromSide, spec.ToSide
	firstStart, secondStart := fromStart, toStart
	if !fromInFirst {
		firstNode, secondNode = secondNode, firstNode
		firstSide, secondSide = secondSide, firstSide
		firstStart, secondStart = secondStart, firstStart
	}

	// Each exposure path projects a nested endpoint outward through its
	// containers. The seam tracks then join their terminal points.
	firstPath, ok := exposurePath(first, firstNode, firstSide, firstStart)
	if !ok {
		return nil, fmt.Errorf("boxz: internal routing error: %q is not exposed on side %s", firstNode.ID, firstSide)
	}
	secondPath, ok := exposurePath(second, secondNode, secondSide, secondStart)
	if !ok {
		return nil, fmt.Errorf("boxz: internal routing error: %q is not exposed on side %s", secondNode.ID, secondSide)
	}

	firstOffset := trackOffset(spec.FirstTrack, spec.TrackCount, cfg.LaneSpacing)
	secondOffset := trackOffset(spec.SecondTrack, spec.TrackCount, cfg.LaneSpacing)
	firstEnd := firstPath.Points[len(firstPath.Points)-1]
	secondEnd := secondPath.Points[len(secondPath.Points)-1]
	points := append([]point(nil), firstPath.Points...)
	resources := make([]string, len(firstPath.Domains))
	domains := append([]string(nil), firstPath.Domains...)
	appendPoint := func(p point, domain string) {
		if points[len(points)-1] == p {
			return
		}
		points = append(points, p)
		resources = append(resources, "")
		domains = append(domains, domain)
	}
	seamDomain := seamConnectorDomainID(spec.ID)
	if parent.Element.Kind == KindVBox {
		seamCenter := (first.Rect.Y + first.Rect.H + second.Rect.Y) / 2
		firstY, secondY := seamCenter+firstOffset, seamCenter+secondOffset
		appendPoint(point{X: firstEnd.X, Y: firstY}, seamDomain)
		if spec.FirstTrack == spec.SecondTrack {
			appendPoint(point{X: secondEnd.X, Y: firstY}, "")
		} else {
			appendPoint(point{X: spec.DoglegCoordinate, Y: firstY}, "")
			appendPoint(point{X: spec.DoglegCoordinate, Y: secondY}, seamDomain)
			appendPoint(point{X: secondEnd.X, Y: secondY}, "")
		}
		appendPoint(secondEnd, seamDomain)
	} else {
		seamCenter := (first.Rect.X + first.Rect.W + second.Rect.X) / 2
		firstX, secondX := seamCenter+firstOffset, seamCenter+secondOffset
		appendPoint(point{X: firstX, Y: firstEnd.Y}, seamDomain)
		if spec.FirstTrack == spec.SecondTrack {
			appendPoint(point{X: firstX, Y: secondEnd.Y}, "")
		} else {
			appendPoint(point{X: firstX, Y: spec.DoglegCoordinate}, "")
			appendPoint(point{X: secondX, Y: spec.DoglegCoordinate}, seamDomain)
			appendPoint(point{X: secondX, Y: secondEnd.Y}, "")
		}
		appendPoint(secondEnd, seamDomain)
	}
	reversePath(secondPath)
	for index := 1; index < len(secondPath.Points); index++ {
		appendPoint(secondPath.Points[index], secondPath.Domains[index-1])
	}
	if !fromInFirst {
		reverse(points)
		reverse(resources)
		reverse(domains)
	}
	points, resources, domains = simplifyRoute(points, resources, domains)
	return &routedEdge{
		EdgeIndex: edgeIndex,
		From:      edge.From,
		To:        edge.To,
		FromSide:  spec.FromSide,
		ToSide:    spec.ToSide,
		Points:    points,
		Resources: resources,
		Domains:   domains,
	}, nil
}

func trackOffset(track, count int, spacing float64) float64 {
	return (float64(track) - float64(count-1)/2) * spacing
}

// routeRunOffset returns the display lane selected by the first named routing
// channel domain in a collinear run.
func routeRunOffset(route *routedEdge, routes *routeResult, first, last int, cfg Config) float64 {
	for segmentIndex := first; segmentIndex <= last; segmentIndex++ {
		domain := route.Domains[segmentIndex]
		lane, ok := routes.LaneByEdge[route.EdgeIndex][domain]
		if domain == "" || !ok {
			continue
		}
		return trackOffset(lane, len(routes.ChannelUses[domain]), cfg.LaneSpacing)
	}
	return 0
}

func rerouteSeams(doc *Document, l *layout, plan *routingPlan, routes *routeResult, ports map[portKey]point, cfg Config) error {
	for index, route := range routes.Edges {
		spec := plan.Seams[route.EdgeIndex]
		if spec == nil {
			continue
		}
		edge := doc.Edges[route.EdgeIndex]
		fromPort, fromOK := ports[portKey{node: edge.From, side: spec.FromSide, edge: route.EdgeIndex}]
		toPort, toOK := ports[portKey{node: edge.To, side: spec.ToSide, edge: route.EdgeIndex, to: true}]
		if !fromOK || !toOK {
			return fmt.Errorf("boxz: internal routing error: seam %q has an unallocated port", spec.ID)
		}
		rebuilt, err := routeThroughSeam(l, edge, route.EdgeIndex, spec, fromPort, toPort, cfg)
		if err != nil {
			return err
		}
		routes.Edges[index] = rebuilt
	}
	return nil
}

type exposure struct {
	Points  []point
	Domains []string
}

// exposurePath projects an explicit point on a frontier node through every
// enclosing rectangle up to root's requested side. It also records hierarchy
// connector domains crossed by the prescribed path, so seam routes participate
// in the same physical allocation as searched routes.
func exposurePath(root *placement, node *Element, side Side, start point) (*exposure, bool) {
	if root.Element.Kind == KindNode {
		if root.Element != node {
			return nil, false
		}
		return &exposure{Points: []point{start}}, true
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
	points, ok := exposurePath(child, node, side, start)
	if !ok {
		return nil, false
	}
	end := points.Points[len(points.Points)-1]
	destination := end
	domain := ""
	if c := root.Channels[side]; c != nil {
		if side == North || side == South {
			destination.Y = c.A.Y
		} else {
			destination.X = c.A.X
		}
		if child.Element.Kind != KindNode && child.Channels[side] != nil {
			domain = riserID(child.Element.ID, side, 0)
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
		points.Points = append(points.Points, destination)
		points.Domains = append(points.Domains, domain)
	}
	return points, true
}

func reversePath(path *exposure) {
	reverse(path.Points)
	reverse(path.Domains)
}

func sideCenter(r rect, side Side) point {
	center := r.center()
	switch side {
	case North:
		return point{X: center.X, Y: r.Y}
	case East:
		return point{X: r.X + r.W, Y: center.Y}
	case South:
		return point{X: center.X, Y: r.Y + r.H}
	case West:
		return point{X: r.X, Y: center.Y}
	default:
		return center
	}
}

// makeRouteGraph splits channel center lines, hierarchy risers, and sibling
// crossbars at every intersection, producing the graph searched by outer routes.
func makeRouteGraph(l *layout) (*routeGraph, error) {
	// Center lines are sufficient for pathfinding. Physical track allocation is
	// a later solver phase after every route intent is known.
	var segments []segment
	for _, channelName := range sortedChannelIDs(l.Channels) {
		c := l.Channels[channelName]
		segments = append(segments, segment{A: c.A, B: c.B, resource: c.ID, domain: c.ID, kind: segmentChannel})
	}
	ports := make(map[string]map[Side]point)
	connectors := make(map[string]connectorSpec)
	domains := make(map[string]connectorDomain)
	addHierarchySegments(l.Root, &segments, ports, connectors, domains)
	baseSegments := append([]segment(nil), segments...)
	addSiblingCrossbars(l.Root, baseSegments, &segments, connectors, domains)

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

	graph := &routeGraph{
		ports:      make(map[string]map[Side]int),
		connectors: connectors,
		domains:    domains,
		segments:   segments,
	}
	// Exact point identity joins graph vertices. New intersection points copy one
	// coordinate from each stored segment; callers must not recompute equivalent
	// coordinates through different floating-point arithmetic.
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
			graph.adj[from] = append(graph.adj[from], graphEdge{to: to, length: length, dir: dir, resource: s.resource, domain: s.domain})
			graph.adj[to] = append(graph.adj[to], graphEdge{to: from, length: length, dir: dir, resource: s.resource, domain: s.domain})
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
			return left.resource < right.resource
		})
	}
	return graph, nil
}

// boundaryAccess describes the routable portion of one container side. A
// matching channel makes the whole side interval available; containers with
// only orthogonal channels expose their channel endpoints as discrete portals.
type boundaryAccess struct {
	line   *segment
	points []point
}

// addSiblingCrossbars connects facing routing networks across otherwise empty
// sibling gaps. Candidate coordinates come from existing structural vertices,
// keeping the graph finite and independent of arbitrary geometric visibility.
func addSiblingCrossbars(parent *placement, baseSegments []segment, segments *[]segment, connectors map[string]connectorSpec, domains map[string]connectorDomain) {
	if parent.Element.Kind == KindNode {
		return
	}
	for index := 0; index+1 < len(parent.Children); index++ {
		first, second := parent.Children[index], parent.Children[index+1]
		var firstSide, secondSide Side
		if parent.Element.Kind == KindHBox {
			firstSide, secondSide = East, West
		} else {
			firstSide, secondSide = South, North
		}
		firstAccess := routingBoundary(first, firstSide)
		secondAccess := routingBoundary(second, secondSide)
		if firstAccess == nil || secondAccess == nil {
			continue
		}

		coordinates := append(boundaryCoordinates(firstAccess, baseSegments, parent.Element.Kind),
			boundaryCoordinates(secondAccess, baseSegments, parent.Element.Kind)...)
		coordinates = sortedUniqueCoordinates(coordinates)
		crossbarIndex := 0
		domainID := seamConnectorDomainID(seamID(parent.Element.ID, index))
		for _, coordinate := range coordinates {
			a, firstOK := boundaryPoint(firstAccess, coordinate, parent.Element.Kind)
			b, secondOK := boundaryPoint(secondAccess, coordinate, parent.Element.Kind)
			if !firstOK || !secondOK || a == b {
				continue
			}
			id := crossbarID(parent.Element.ID, index, crossbarIndex)
			*segments = append(*segments, segment{
				A:        a,
				B:        b,
				resource: id,
				domain:   domainID,
				kind:     segmentCrossbar,
			})
			low, high := coordinate, coordinate
			if firstAccess.line != nil && secondAccess.line != nil {
				low, high = boundaryOverlap(*firstAccess.line, *secondAccess.line, parent.Element.Kind)
			}
			connectors[id] = connectorSpec{
				ResourceID: id,
				DomainID:   domainID,
				Kind:       connectorSibling,
				Horizontal: parent.Element.Kind == KindHBox,
				Low:        low,
				High:       high,
			}
			mergeConnectorDomain(domains, connectorDomain{
				ID: domainID, Kind: connectorSibling,
				Horizontal: parent.Element.Kind == KindHBox, Low: low, High: high,
			})
			crossbarIndex++
		}
	}
	for _, child := range parent.Children {
		addSiblingCrossbars(child, baseSegments, segments, connectors, domains)
	}
}

func boundaryOverlap(first, second segment, parentKind Kind) (float64, float64) {
	if parentKind == KindHBox {
		return math.Max(math.Min(first.A.Y, first.B.Y), math.Min(second.A.Y, second.B.Y)),
			math.Min(math.Max(first.A.Y, first.B.Y), math.Max(second.A.Y, second.B.Y))
	}
	return math.Max(math.Min(first.A.X, first.B.X), math.Min(second.A.X, second.B.X)),
		math.Min(math.Max(first.A.X, first.B.X), math.Max(second.A.X, second.B.X))
}

func routingBoundary(child *placement, side Side) *boundaryAccess {
	if child.Element.Kind == KindNode {
		// Node boundaries must remain route endpoints, never transit junctions.
		return nil
	}
	if c := child.Channels[side]; c != nil {
		line := segment{A: c.A, B: c.B, resource: c.ID, domain: c.ID, kind: segmentChannel}
		return &boundaryAccess{line: &line}
	}
	return &boundaryAccess{points: boundaryPortals(child, side)}
}

func boundaryCoordinates(access *boundaryAccess, segments []segment, parentKind Kind) []float64 {
	coordinate := func(p point) float64 {
		if parentKind == KindHBox {
			return p.Y
		}
		return p.X
	}
	if access.line == nil {
		result := make([]float64, 0, len(access.points))
		for _, p := range access.points {
			result = append(result, coordinate(p))
		}
		return result
	}

	result := []float64{coordinate(access.line.A), coordinate(access.line.B)}
	for _, candidate := range segments {
		for _, intersection := range segmentIntersections(*access.line, candidate) {
			result = append(result, coordinate(intersection))
		}
	}
	return result
}

func boundaryPoint(access *boundaryAccess, coordinate float64, parentKind Kind) (point, bool) {
	if access.line != nil {
		candidate := point{X: coordinate, Y: access.line.A.Y}
		if parentKind == KindHBox {
			candidate = point{X: access.line.A.X, Y: coordinate}
		}
		return candidate, pointOnSegment(candidate, *access.line)
	}
	for _, p := range access.points {
		value := p.X
		if parentKind == KindHBox {
			value = p.Y
		}
		if sameCoordinate(value, coordinate) {
			return p, true
		}
	}
	return point{}, false
}

func sortedUniqueCoordinates(values []float64) []float64 {
	sort.Float64s(values)
	result := make([]float64, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || !sameCoordinate(result[len(result)-1], value) {
			result = append(result, value)
		}
	}
	return result
}

func crossbarID(parentID string, firstChild, index int) string {
	return fmt.Sprintf("%s:crossbar:%d:%d", parentID, firstChild, index)
}

func seamConnectorDomainID(seamID string) string { return seamID + ":connector" }

func mergeConnectorDomain(domains map[string]connectorDomain, candidate connectorDomain) {
	if existing, ok := domains[candidate.ID]; ok {
		candidate.Low = math.Min(existing.Low, candidate.Low)
		candidate.High = math.Max(existing.High, candidate.High)
	}
	domains[candidate.ID] = candidate
}

// addHierarchySegments connects child boundary portals to the channels owned by
// each parent and records node portals as legal endpoints.
func addHierarchySegments(parent *placement, segments *[]segment, ports map[string]map[Side]point, connectors map[string]connectorSpec, domains map[string]connectorDomain) {
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
				portals := hierarchyPortals(child, side)
				for portalIndex, portal := range portals {
					destination := point{X: portal.X, Y: parentChannel.A.Y}
					resource, domain := hierarchyRiserResource(child, parentChannel, side, portalIndex, portal, connectors, domains)
					*segments = append(*segments, segment{A: portal.point, B: destination, resource: resource, domain: domain, kind: segmentRiser})
					if child.Element.Kind == KindNode {
						setPort(ports, child.Element.ID, side, portal.point)
					}
				}
			}
		} else {
			for _, side := range []Side{West, East} {
				parentChannel := parent.Channels[side]
				portals := hierarchyPortals(child, side)
				for portalIndex, portal := range portals {
					destination := point{X: parentChannel.A.X, Y: portal.Y}
					resource, domain := hierarchyRiserResource(child, parentChannel, side, portalIndex, portal, connectors, domains)
					*segments = append(*segments, segment{A: portal.point, B: destination, resource: resource, domain: domain, kind: segmentRiser})
					if child.Element.Kind == KindNode {
						setPort(ports, child.Element.ID, side, portal.point)
					}
				}
			}
		}
		addHierarchySegments(child, segments, ports, connectors, domains)
	}
}

func hierarchyRiserResource(child *placement, parentChannel *channel, side Side, portalIndex int, portal hierarchyPortal, connectors map[string]connectorSpec, domains map[string]connectorDomain) (string, string) {
	// Node access legs are adjusted to exact endpoint ports later. Only
	// container-to-parent transitions need shared connector allocation.
	if child.Element.Kind == KindNode {
		return "", ""
	}
	if portal.continuation != "" {
		return portal.continuation, portal.continuation
	}
	id := riserID(child.Element.ID, side, portalIndex)
	childChannel := child.Channels[side]
	low, high := connectorOverlap(childChannel, parentChannel)
	horizontal := side == West || side == East
	connectors[id] = connectorSpec{
		ResourceID: id, DomainID: id, Kind: connectorHierarchy,
		Horizontal: horizontal, Low: low, High: high,
	}
	domains[id] = connectorDomain{
		ID: id, Kind: connectorHierarchy, Horizontal: horizontal,
		Low: low, High: high, ChildID: child.Element.ID, Side: side,
	}
	return id, id
}

func connectorOverlap(first, second *channel) (float64, float64) {
	if first.A.Y == first.B.Y {
		return math.Max(math.Min(first.A.X, first.B.X), math.Min(second.A.X, second.B.X)),
			math.Min(math.Max(first.A.X, first.B.X), math.Max(second.A.X, second.B.X))
	}
	return math.Max(math.Min(first.A.Y, first.B.Y), math.Min(second.A.Y, second.B.Y)),
		math.Min(math.Max(first.A.Y, first.B.Y), math.Max(second.A.Y, second.B.Y))
}

func hierarchyPortals(child *placement, side Side) []hierarchyPortal {
	if child.Element.Kind == KindNode {
		points := boundaryPortals(child, side)
		return []hierarchyPortal{{point: points[0]}}
	}
	if matching := child.Channels[side]; matching != nil {
		return []hierarchyPortal{{point: point{
			X: (matching.A.X + matching.B.X) / 2,
			Y: (matching.A.Y + matching.B.Y) / 2,
		}}}
	}

	var portals []hierarchyPortal
	for _, channelSide := range []Side{North, East, South, West} {
		c := child.Channels[channelSide]
		if c == nil {
			continue
		}
		p := c.A
		if side == North && c.B.Y < p.Y || side == South && c.B.Y > p.Y ||
			side == West && c.B.X < p.X || side == East && c.B.X > p.X {
			p = c.B
		}
		portals = append(portals, hierarchyPortal{point: p, continuation: c.ID})
	}
	sort.Slice(portals, func(i, j int) bool {
		if side == North || side == South {
			return portals[i].X < portals[j].X
		}
		return portals[i].Y < portals[j].Y
	})
	return portals
}

func riserID(childID string, side Side, portalIndex int) string {
	return fmt.Sprintf("%s:%s:riser:%d", childID, side, portalIndex)
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
			if next.resource != "" {
				cost.congestion += usage[next.resource]
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
	var reverseResources []string
	var reverseDomains []string
	for state := destination; state != startState; {
		reversePoints = append(reversePoints, graph.points[state.vertex])
		step := previous[state]
		reverseResources = append(reverseResources, step.edge.resource)
		reverseDomains = append(reverseDomains, step.edge.domain)
		state = step.state
	}
	reversePoints = append(reversePoints, graph.points[start])
	reverse(reversePoints)
	reverse(reverseResources)
	reverse(reverseDomains)
	return &routedEdge{ToSide: destinationSide, Points: reversePoints, Resources: reverseResources, Domains: reverseDomains}, distance[destination], true
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

// simplifyRoute removes geometrically redundant vertices without erasing a
// resource or domain boundary needed by physical track allocation.
func simplifyRoute(points []point, resources, domains []string) ([]point, []string, []string) {
	if len(points) == 0 {
		return nil, nil, nil
	}
	resultPoints := []point{points[0]}
	resultResources := make([]string, 0, len(resources))
	resultDomains := make([]string, 0, len(domains))
	for index := range resources {
		end := points[index+1]
		if resultPoints[len(resultPoints)-1] == end {
			continue
		}
		if len(resultPoints) >= 2 && len(resultResources) != 0 {
			a, b := resultPoints[len(resultPoints)-2], resultPoints[len(resultPoints)-1]
			collinear := a.X == b.X && b.X == end.X || a.Y == b.Y && b.Y == end.Y
			last := len(resultResources) - 1
			if collinear && resultResources[last] == resources[index] && resultDomains[last] == domains[index] {
				resultPoints[len(resultPoints)-1] = end
				continue
			}
		}
		resultPoints = append(resultPoints, end)
		resultResources = append(resultResources, resources[index])
		resultDomains = append(resultDomains, domains[index])
	}
	return resultPoints, resultResources, resultDomains
}

func manhattan(a, b point) float64 { return math.Abs(a.X-b.X) + math.Abs(a.Y-b.Y) }

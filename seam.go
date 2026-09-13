package boxz

import (
	"fmt"
	"math"
	"sort"
)

type seamPins struct {
	edgeIndex int
	first     float64
	second    float64
	spec      *seamSpec
}

type connectorUse struct {
	route      *routedEdge
	first      int
	domain     connectorDomain
	preferred  float64
	low        float64
	high       float64
	priority   int
	coordinate float64
}

// assignSeamTracks solves each sibling seam as an independent channel-routing
// problem. A first-side access leg aligned with a second-side leg must use an
// earlier track, otherwise the two perpendicular legs overlap between tracks.
func assignSeamTracks(doc *Document, l *layout, plan *routingPlan, ports map[portKey]point, cfg Config) (bool, error) {
	groups := make(map[string][]seamPins)
	for edgeIndex, edge := range doc.Edges {
		spec := plan.Seams[edgeIndex]
		if spec == nil {
			continue
		}
		first, second, err := physicalSeamPins(l, edge, edgeIndex, spec, ports)
		if err != nil {
			return false, err
		}
		groups[spec.ID] = append(groups[spec.ID], seamPins{
			edgeIndex: edgeIndex,
			first:     first,
			second:    second,
			spec:      spec,
		})
	}

	grew := false
	for _, seamID := range sortedSeamIDs(groups) {
		pins := groups[seamID]
		order, acyclic := seamTrackOrder(pins)
		if acyclic {
			// Seam capacity is monotone across layout iterations. Keep using the
			// retained bank even if changed geometry makes a formerly cyclic seam
			// acyclic; searched seam-channel lanes are reserved after this bank.
			trackCount := plan.SeamTrackCount[seamID]
			firstTrack := (trackCount - len(pins)) / 2
			for track, pinIndex := range order {
				spec := pins[pinIndex].spec
				spec.FirstTrack = firstTrack + track
				spec.SecondTrack = firstTrack + track
				spec.TrackCount = trackCount
				spec.DoglegCoordinate = 0
			}
			continue
		}

		// A cyclic vertical-constraint graph cannot be routed with one track per
		// edge. Put all first-side tracks before all second-side tracks and join
		// each pair at a private dogleg coordinate. This is conservative but
		// guarantees that opposing access legs cannot overlap.
		trackCount := 2 * len(pins)
		if plan.SeamTrackCount[seamID] < trackCount {
			plan.SeamTrackCount[seamID] = trackCount
			grew = true
		}
		parent := l.ByID[pins[0].spec.ParentID]
		if parent == nil {
			return false, fmt.Errorf("boxz: internal routing error: seam %q has no parent", seamID)
		}
		coordinates := doglegCoordinates(parent, len(pins), pins, cfg)
		for index := range pins {
			spec := pins[index].spec
			spec.FirstTrack = index
			spec.SecondTrack = len(pins) + index
			spec.TrackCount = trackCount
			spec.DoglegCoordinate = coordinates[index]
		}
	}
	return grew, nil
}

// assignConnectorTracks allocates all collinear uses of a shared connector
// domain together. Prescribed seam uses are considered first, preserving their
// straight coordinate when possible; searched routes move locally around them.
// It returns hierarchy-domain capacity for the layout fixed point.
func assignConnectorTracks(routes *routeResult, plan *routingPlan, ports map[portKey]point, cfg Config) map[string]int {
	groups := collectConnectorUses(routes, plan, ports, cfg)
	routes.ConnectorUses = make(map[string][]int)
	capacity := make(map[string]int)
	assigned := make(map[*routedEdge]map[int]float64)

	for _, domainID := range sortedConnectorDomainIDs(groups) {
		uses := groups[domainID]
		sort.SliceStable(uses, func(i, j int) bool {
			if uses[i].priority != uses[j].priority {
				return uses[i].priority < uses[j].priority
			}
			if uses[i].route.EdgeIndex != uses[j].route.EdgeIndex {
				return uses[i].route.EdgeIndex < uses[j].route.EdgeIndex
			}
			return uses[i].first < uses[j].first
		})
		chosen := make([]float64, 0, len(uses))
		for _, use := range uses {
			use.coordinate = allocateConnectorCoordinate(use.preferred, use.low, use.high,
				chosen, cfg.LaneSpacing)
			chosen = append(chosen, use.coordinate)
			if assigned[use.route] == nil {
				assigned[use.route] = make(map[int]float64)
			}
			assigned[use.route][use.first] = use.coordinate
			routes.ConnectorUses[domainID] = append(routes.ConnectorUses[domainID], use.route.EdgeIndex)
		}
		if uses[0].domain.Kind == connectorHierarchy {
			capacity[domainID] = len(uses)
		}
	}
	for route, tracks := range assigned {
		rewriteConnectorTracks(route, tracks)
	}
	return capacity
}

func allocateConnectorCoordinate(preferred, low, high float64, chosen []float64, spacing float64) float64 {
	available := func(value float64, requireSpacing bool) bool {
		for _, other := range chosen {
			if sameCoordinate(value, other) || requireSpacing && math.Abs(value-other) < spacing {
				return false
			}
		}
		return true
	}
	if available(preferred, true) {
		return preferred
	}
	for distance := 1; distance <= len(chosen)+2; distance++ {
		for _, direction := range []float64{-1, 1} {
			candidate := preferred + direction*float64(distance)*spacing
			if candidate >= low && candidate <= high && available(candidate, true) {
				return candidate
			}
		}
	}
	// Custom configurations can make the domain narrower than its requested
	// lane spacing. Keep tracks distinct even when ideal spacing is impossible.
	for index := 1; index <= len(chosen)+1; index++ {
		candidate := low + (high-low)*float64(index)/float64(len(chosen)+2)
		if available(candidate, false) {
			return candidate
		}
	}
	return preferred
}

func collectConnectorUses(routes *routeResult, plan *routingPlan, ports map[portKey]point, cfg Config) map[string][]*connectorUse {
	groups := make(map[string][]*connectorUse)
	for _, route := range routes.Edges {
		for first := 0; first < len(route.Domains); {
			domain, ok := routes.Domains[route.Domains[first]]
			if !ok {
				first++
				continue
			}
			last := first
			for last+1 < len(route.Domains) && route.Domains[last+1] == domain.ID {
				last++
			}
			low, high := domain.Low, domain.High
			if resource := route.Resources[first]; resource != "" {
				if spec, found := routes.Connectors[resource]; found {
					low, high = spec.Low, spec.High
				}
			}
			preferred := physicalRunCoordinate(route, first, routes, ports, cfg)
			preferred = math.Max(low, math.Min(high, preferred))
			priority := 1
			if plan.Seams[route.EdgeIndex] != nil {
				priority = 0
			}
			groups[domain.ID] = append(groups[domain.ID], &connectorUse{
				route: route, first: first, domain: domain,
				preferred: preferred, low: low, high: high, priority: priority,
			})
			first = last + 1
		}
	}
	return groups
}

// physicalRunCoordinate predicts the final coordinate of the straight run
// containing segmentIndex. Endpoint ports override lane offsets in displayRoute,
// so endpoint-adjacent crossbars must use those exact coordinates too.
func physicalRunCoordinate(route *routedEdge, segmentIndex int, routes *routeResult, ports map[portKey]point, cfg Config) float64 {
	horizontal := route.Points[segmentIndex].Y == route.Points[segmentIndex+1].Y
	first, last := segmentIndex, segmentIndex
	for first > 0 && (route.Points[first-1].Y == route.Points[first].Y) == horizontal {
		first--
	}
	for last+1 < len(route.Domains) && (route.Points[last+1].Y == route.Points[last+2].Y) == horizontal {
		last++
	}

	coordinate := route.Points[segmentIndex].X
	if horizontal {
		coordinate = route.Points[segmentIndex].Y
	}
	coordinate += routeRunOffset(route, routes, first, last, cfg)
	if first == 0 {
		port := ports[portKey{node: route.From, side: route.FromSide, edge: route.EdgeIndex}]
		if horizontal {
			return port.Y
		}
		return port.X
	}
	if last == len(route.Domains)-1 {
		port := ports[portKey{node: route.To, side: route.ToSide, edge: route.EdgeIndex, to: true}]
		if horizontal {
			return port.Y
		}
		return port.X
	}
	return coordinate
}

func rewriteConnectorTracks(route *routedEdge, tracks map[int]float64) {
	points := []point{route.Points[0]}
	resources := make([]string, 0, len(route.Resources)+2*len(tracks))
	domains := make([]string, 0, len(route.Domains)+2*len(tracks))
	appendPoint := func(p point, resource, domain string) {
		if points[len(points)-1] == p {
			return
		}
		points = append(points, p)
		resources = append(resources, resource)
		domains = append(domains, domain)
	}

	for index := 0; index < len(route.Domains); {
		coordinate, allocated := tracks[index]
		resource, domain := route.Resources[index], route.Domains[index]
		if !allocated {
			appendPoint(route.Points[index+1], resource, domain)
			index++
			continue
		}
		last := index
		for last+1 < len(route.Domains) && route.Domains[last+1] == domain {
			last++
		}
		start, end := route.Points[index], route.Points[last+1]
		shiftedStart, shiftedEnd := start, end
		if start.Y == end.Y {
			shiftedStart.Y, shiftedEnd.Y = coordinate, coordinate
		} else {
			shiftedStart.X, shiftedEnd.X = coordinate, coordinate
		}
		if shiftedStart == start && shiftedEnd == end {
			appendPoint(end, resource, domain)
			index = last + 1
			continue
		}
		appendPoint(shiftedStart, "", "")
		appendPoint(shiftedEnd, resource, domain)
		appendPoint(end, "", "")
		index = last + 1
	}
	route.Points, route.Resources, route.Domains = simplifyRoute(points, resources, domains)
}

func sortedConnectorDomainIDs(groups map[string][]*connectorUse) []string {
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// physicalSeamPins returns coordinates along the seam, normalized to the
// parent's first and second child regardless of arrow direction.
func physicalSeamPins(l *layout, edge *Edge, edgeIndex int, spec *seamSpec, ports map[portKey]point) (float64, float64, error) {
	parent := l.ByID[spec.ParentID]
	if parent == nil || spec.FirstChild+1 >= len(parent.Children) {
		return 0, 0, fmt.Errorf("boxz: internal routing error: seam %q is missing", spec.ID)
	}
	from, fromOK := ports[portKey{node: edge.From, side: spec.FromSide, edge: edgeIndex}]
	to, toOK := ports[portKey{node: edge.To, side: spec.ToSide, edge: edgeIndex, to: true}]
	if !fromOK || !toOK {
		return 0, 0, fmt.Errorf("boxz: internal routing error: seam %q has an unallocated port", spec.ID)
	}

	firstChild := parent.Children[spec.FirstChild]
	first, second := from, to
	if !containsElement(firstChild.Element, l.ByID[edge.From].Element) {
		first, second = to, from
	}
	if parent.Element.Kind == KindVBox {
		return first.X, second.X, nil
	}
	return first.Y, second.Y, nil
}

// seamTrackOrder topologically sorts the vertical constraints. Stable edge
// indexes make otherwise unconstrained tracks follow declaration order.
func seamTrackOrder(pins []seamPins) ([]int, bool) {
	dependencies := make([]map[int]bool, len(pins))
	indegree := make([]int, len(pins))
	for first := range pins {
		dependencies[first] = make(map[int]bool)
		for second := range pins {
			if first == second || !sameCoordinate(pins[first].first, pins[second].second) {
				continue
			}
			dependencies[first][second] = true
			indegree[second]++
		}
	}

	order := make([]int, 0, len(pins))
	used := make([]bool, len(pins))
	for len(order) < len(pins) {
		next := -1
		for index := range pins {
			if used[index] || indegree[index] != 0 {
				continue
			}
			if next < 0 || pins[index].edgeIndex < pins[next].edgeIndex {
				next = index
			}
		}
		if next < 0 {
			return nil, false
		}
		used[next] = true
		order = append(order, next)
		for dependent := range dependencies[next] {
			indegree[dependent]--
		}
	}
	return order, true
}

func doglegCoordinates(parent *placement, count int, pins []seamPins, cfg Config) []float64 {
	first := parent.Children[pins[0].spec.FirstChild]
	second := parent.Children[pins[0].spec.FirstChild+1]
	var low, high float64
	if parent.Element.Kind == KindVBox {
		low = math.Min(first.Rect.X, second.Rect.X)
		high = math.Max(first.Rect.X+first.Rect.W, second.Rect.X+second.Rect.W)
	} else {
		low = math.Min(first.Rect.Y, second.Rect.Y)
		high = math.Max(first.Rect.Y+first.Rect.H, second.Rect.Y+second.Rect.H)
	}

	// Keep doglegs visibly separate. The measured frontier normally provides at
	// least this much room; fall back to equal fractions for unusually tight
	// custom configurations.
	spacing := cfg.LaneSpacing
	span := float64(maxInt(0, count-1)) * spacing
	start := (low + high - span) / 2
	if start < low || start+span > high {
		spacing = (high - low) / float64(count+1)
		start = low + spacing
	}

	forbidden := make([]float64, 0, 2*len(pins))
	for _, pin := range pins {
		forbidden = append(forbidden, pin.first, pin.second)
	}
	result := make([]float64, count)
	for index := range result {
		candidate := start + float64(index)*spacing
		result[index] = avoidCoordinates(candidate, low, high, forbidden, result[:index], cfg.LaneSpacing)
	}
	return result
}

func avoidCoordinates(candidate, low, high float64, forbidden, chosen []float64, spacing float64) float64 {
	available := func(value float64, keepSpacing bool) bool {
		for _, other := range forbidden {
			if sameCoordinate(value, other) {
				return false
			}
		}
		for _, other := range chosen {
			if sameCoordinate(value, other) || keepSpacing && math.Abs(value-other) < spacing/2 {
				return false
			}
		}
		return true
	}
	if available(candidate, true) {
		return candidate
	}
	step := spacing / 3
	if step == 0 {
		step = 1
	}
	for distance := 1; distance <= len(forbidden)+len(chosen)+2; distance++ {
		for _, direction := range []float64{-1, 1} {
			alternative := candidate + direction*float64(distance)*step
			if alternative >= low && alternative <= high && available(alternative, true) {
				return alternative
			}
		}
	}
	// A very narrow custom layout may not permit the preferred separation. A
	// finite set of occupied coordinates still leaves one of these evenly spaced
	// candidates available without an unbounded floating-point search.
	attempts := len(forbidden) + len(chosen) + 1
	for index := 1; index <= attempts; index++ {
		alternative := low + (high-low)*float64(index)/float64(attempts+1)
		if available(alternative, false) {
			return alternative
		}
	}
	// Unreachable unless floating-point rounding collapses the entire interval.
	return candidate
}

func sortedSeamIDs(groups map[string][]seamPins) []string {
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sameCoordinate(left, right float64) bool {
	return math.Abs(left-right) < 1e-9
}

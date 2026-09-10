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

type crossbarUse struct {
	route     *routedEdge
	spec      crossbarSpec
	preferred float64
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
			for track, pinIndex := range order {
				spec := pins[pinIndex].spec
				spec.FirstTrack = track
				spec.SecondTrack = track
				spec.TrackCount = len(pins)
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

// assignCrossbarTracks shares each sibling gap between direct seam access legs
// and generated crossbars. Direct endpoints keep their exact port coordinate;
// movable crossbars shift locally and gain short shoulders at both boundaries.
func assignCrossbarTracks(doc *Document, l *layout, plan *routingPlan, routes *routeResult, ports map[portKey]point, cfg Config) error {
	forbidden := make(map[string][]float64)
	for edgeIndex, edge := range doc.Edges {
		spec := plan.Seams[edgeIndex]
		if spec == nil {
			continue
		}
		first, second, err := physicalSeamPins(l, edge, edgeIndex, spec, ports)
		if err != nil {
			return err
		}
		forbidden[spec.ID] = append(forbidden[spec.ID], first, second)
	}

	groups := make(map[string][]crossbarUse)
	for _, route := range routes.Edges {
		seen := make(map[string]bool)
		for segmentIndex, resource := range route.Channels {
			spec, ok := routes.Crossbars[resource]
			if !ok || seen[resource] || sameCoordinate(spec.Low, spec.High) {
				continue
			}
			seen[resource] = true
			preferred := route.Points[segmentIndex].X
			if spec.Horizontal {
				preferred = route.Points[segmentIndex].Y
			}
			groups[spec.SeamID] = append(groups[spec.SeamID], crossbarUse{
				route: route, spec: spec, preferred: preferred,
			})
		}
	}

	assigned := make(map[*routedEdge]map[string]float64)
	for _, seamID := range sortedCrossbarSeamIDs(groups) {
		uses := groups[seamID]
		sort.SliceStable(uses, func(i, j int) bool {
			if uses[i].route.EdgeIndex != uses[j].route.EdgeIndex {
				return uses[i].route.EdgeIndex < uses[j].route.EdgeIndex
			}
			return uses[i].spec.ID < uses[j].spec.ID
		})
		chosen := make([]float64, 0, len(uses))
		for _, use := range uses {
			occupied := append([]float64(nil), forbidden[seamID]...)
			occupied = append(occupied, chosen...)
			coordinate := avoidCoordinates(use.preferred, use.spec.Low, use.spec.High,
				nil, occupied, cfg.LaneSpacing)
			if assigned[use.route] == nil {
				assigned[use.route] = make(map[string]float64)
			}
			assigned[use.route][use.spec.ID] = coordinate
			chosen = append(chosen, coordinate)
		}
	}
	for route, tracks := range assigned {
		rewriteCrossbarTracks(route, routes.Crossbars, tracks)
	}
	return nil
}

func rewriteCrossbarTracks(route *routedEdge, specs map[string]crossbarSpec, tracks map[string]float64) {
	points := []point{route.Points[0]}
	channels := make([]string, 0, len(route.Channels)+2*len(tracks))
	appendPoint := func(p point, resource string) {
		if points[len(points)-1] == p {
			return
		}
		points = append(points, p)
		channels = append(channels, resource)
	}

	for index := 0; index < len(route.Channels); {
		resource := route.Channels[index]
		coordinate, shifted := tracks[resource]
		if !shifted {
			appendPoint(route.Points[index+1], resource)
			index++
			continue
		}
		last := index
		for last+1 < len(route.Channels) && route.Channels[last+1] == resource {
			last++
		}
		spec := specs[resource]
		start, end := route.Points[index], route.Points[last+1]
		shiftedStart, shiftedEnd := start, end
		if spec.Horizontal {
			shiftedStart.Y, shiftedEnd.Y = coordinate, coordinate
		} else {
			shiftedStart.X, shiftedEnd.X = coordinate, coordinate
		}
		appendPoint(shiftedStart, "")
		appendPoint(shiftedEnd, "")
		appendPoint(end, "")
		index = last + 1
	}
	route.Points, route.Channels = points, channels
}

func sortedCrossbarSeamIDs(groups map[string][]crossbarUse) []string {
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

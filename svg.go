package boxz

import (
	"fmt"
	"html"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RenderSVG lays out and renders a parsed document as a standalone SVG. Set
// cfg.Debug to include structural and routing diagnostics.
func RenderSVG(w io.Writer, doc *Document, cfg Config) error {
	l, routes, err := solve(doc, cfg)
	if err != nil {
		return err
	}

	ports := allocatePorts(l, routes)
	displayRoutes := make([][]point, len(routes.Edges))
	for index, route := range routes.Edges {
		displayRoutes[index] = displayRoute(route, routes, ports, cfg)
	}
	labels := placeGroupLabels(l.Root, displayRoutes, cfg)
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s">`, number(l.Width), number(l.Height), number(l.Width), number(l.Height))
	svg.WriteString("\n  <defs>\n")
	svg.WriteString("    <marker id=\"arrow\" viewBox=\"0 0 10 10\" refX=\"9\" refY=\"5\" markerWidth=\"7\" markerHeight=\"7\" orient=\"auto-start-reverse\">\n")
	svg.WriteString("      <path d=\"M 0 0 L 10 5 L 0 10 z\" fill=\"#475569\"/>\n")
	svg.WriteString("    </marker>\n")
	svg.WriteString("    <style>\n")
	svg.WriteString("      .boxz-node { fill: #ffffff; stroke: #334155; stroke-width: 1.5; }\n")
	svg.WriteString("      .boxz-title { fill: #0f172a; font: 14px ui-sans-serif, system-ui, sans-serif; text-anchor: middle; dominant-baseline: middle; }\n")
	svg.WriteString("      .boxz-group-boundary { fill: none; stroke: #94a3b8; stroke-width: 1; }\n")
	svg.WriteString("      .boxz-group-label-bg { fill: #ffffff; fill-opacity: 0.6; }\n")
	svg.WriteString("      .boxz-group-title { fill: #334155; font: 13px ui-sans-serif, system-ui, sans-serif; text-anchor: middle; dominant-baseline: middle; }\n")
	svg.WriteString("      .boxz-edge { fill: none; stroke: #475569; stroke-width: 1.5; stroke-linejoin: round; stroke-linecap: round; marker-end: url(#arrow); }\n")
	if cfg.Debug {
		svg.WriteString("      .boxz-container { fill: none; stroke: #cbd5e1; stroke-width: 1; stroke-dasharray: 4 4; }\n")
		svg.WriteString("      .boxz-debug-route { fill: none; stroke-width: 1; stroke-linecap: round; opacity: 0.55; }\n")
		svg.WriteString("      .boxz-debug-channel { stroke: #0ea5e9; stroke-dasharray: 5 3; }\n")
		svg.WriteString("      .boxz-debug-riser { stroke: #8b5cf6; stroke-dasharray: 2 3; }\n")
		svg.WriteString("      .boxz-debug-crossbar { stroke: #f59e0b; stroke-dasharray: 2 2; }\n")
		svg.WriteString("      .boxz-debug-used { opacity: 0.9; stroke-width: 1.5; }\n")
		svg.WriteString("      .boxz-debug-port { fill: #ef4444; stroke: #ffffff; stroke-width: 0.75; }\n")
	}
	svg.WriteString("    </style>\n  </defs>\n")
	if cfg.Debug {
		svg.WriteString("  <g class=\"boxz-debug-routing\">\n")
		writeRoutingGraph(&svg, routes)
		svg.WriteString("  </g>\n")
		svg.WriteString("  <g class=\"boxz-containers\">\n")
		writeContainers(&svg, l.Root)
		svg.WriteString("  </g>\n")
	}
	svg.WriteString("  <g class=\"boxz-groups\">\n")
	writeGroups(&svg, l.Root)
	svg.WriteString("  </g>\n")
	svg.WriteString("  <g class=\"boxz-edges\">\n")
	for index, route := range routes.Edges {
		edge := doc.Edges[route.EdgeIndex]
		fmt.Fprintf(&svg, "    <path class=\"boxz-edge\" data-from=\"%s\" data-to=\"%s\" d=\"%s\"/>\n",
			html.EscapeString(edge.From), html.EscapeString(edge.To), svgPath(displayRoutes[index]))
	}
	svg.WriteString("  </g>\n  <g class=\"boxz-nodes\">\n")
	writeNodes(&svg, l.Root)
	svg.WriteString("  </g>\n")
	svg.WriteString("  <g class=\"boxz-group-labels\">\n")
	writeGroupLabels(&svg, labels)
	svg.WriteString("  </g>\n")
	if cfg.Debug {
		svg.WriteString("  <g class=\"boxz-debug-ports\">\n")
		writeDebugPorts(&svg, ports)
		svg.WriteString("  </g>\n")
	}
	svg.WriteString("</svg>\n")
	_, err = io.WriteString(w, svg.String())
	return err
}

type groupLabel struct {
	container string
	fullText  string
	text      string
	rect      rect
}

// placeGroupLabels is a post-routing display pass. Labels reserve vertical
// space during layout but never become obstacles or influence route selection.
func placeGroupLabels(root *placement, routes [][]point, cfg Config) []groupLabel {
	var labels []groupLabel
	var visit func(*placement)
	visit = func(p *placement) {
		if p.Element.Kind != KindNode && p.Element.Title != "" {
			strip := p.LabelStrip
			desired := float64(utf8.RuneCountInString(p.Element.Title))*cfg.CharacterWidth + 2*cfg.GroupLabelPaddingX
			width := math.Min(strip.W, desired)
			width = math.Max(0, width)
			x := alignedLabelX(strip, width, p.Element.ContainerAttributes.LabelAlign, routes)
			labels = append(labels, groupLabel{
				container: p.Element.ID,
				fullText:  p.Element.Title,
				text:      truncateGroupTitle(p.Element.Title, width, cfg),
				rect:      rect{X: x, Y: strip.Y, W: width, H: strip.H},
			})
		}
		for _, child := range p.Children {
			visit(child)
		}
	}
	visit(root)
	return labels
}

func alignedLabelX(strip rect, width float64, alignment LabelAlignment, routes [][]point) float64 {
	switch alignment {
	case LabelAlignCenter:
		return strip.X + (strip.W-width)/2
	case LabelAlignRight:
		return strip.X + strip.W - width
	case LabelAlignLeft:
		return strip.X
	default:
		return automaticLabelX(strip, width, routes)
	}
}

// automaticLabelX sweeps the positions where a route starts or stops
// intersecting the backing rectangle, then chooses the least-covered position.
// Candidate and route order are stable, and equal scores prefer the left edge.
func automaticLabelX(strip rect, width float64, routes [][]point) float64 {
	const clearance = 1.0
	left, right := strip.X, strip.X+strip.W-width
	candidates := []float64{left, right}
	for _, route := range routes {
		for index := 0; index+1 < len(route); index++ {
			a, b := route[index], route[index+1]
			minY, maxY := math.Min(a.Y, b.Y), math.Max(a.Y, b.Y)
			if maxY+clearance <= strip.Y || minY-clearance >= strip.Y+strip.H {
				continue
			}
			minX, maxX := math.Min(a.X, b.X), math.Max(a.X, b.X)
			candidates = append(candidates,
				clamp(minX-clearance-width, left, right),
				clamp(maxX+clearance, left, right),
			)
		}
	}
	sort.Float64s(candidates)
	bestX, bestScore := left, math.MaxInt
	for _, candidate := range candidates {
		score := labelCrossingScore(rect{X: candidate, Y: strip.Y, W: width, H: strip.H}, routes, clearance)
		if score < bestScore {
			bestX, bestScore = candidate, score
		}
	}
	return bestX
}

func labelCrossingScore(label rect, routes [][]point, clearance float64) int {
	score := 0
	for _, route := range routes {
		for index := 0; index+1 < len(route); index++ {
			a, b := route[index], route[index+1]
			minX, maxX := math.Min(a.X, b.X), math.Max(a.X, b.X)
			minY, maxY := math.Min(a.Y, b.Y), math.Max(a.Y, b.Y)
			if maxX+clearance > label.X && minX-clearance < label.X+label.W &&
				maxY+clearance > label.Y && minY-clearance < label.Y+label.H {
				score++
			}
		}
	}
	return score
}

func truncateGroupTitle(title string, width float64, cfg Config) string {
	available := math.Max(0, width-2*cfg.GroupLabelPaddingX)
	if float64(utf8.RuneCountInString(title))*cfg.CharacterWidth <= available {
		return title
	}
	capacity := int(available / cfg.CharacterWidth)
	if capacity <= 0 {
		return ""
	}
	if capacity == 1 {
		return "…"
	}
	runes := []rune(title)
	return string(runes[:capacity-1]) + "…"
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}

// solve alternates layout and routing until every outer channel and sibling
// seam has enough room for the tracks assigned to it.
func solve(doc *Document, cfg Config) (*layout, *routeResult, error) {
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		return nil, nil, err
	}
	// Outer-channel and cyclic-seam demand is only known after routing, while
	// routing needs coordinates. Rebuild until every track fits. Allocations only
	// grow, which makes the loop monotonic and prevents layout oscillation.
	allocated := make(map[string]int)
	for iteration := 0; iteration < len(doc.Edges)*2+4; iteration++ {
		l, err := buildLayout(doc, cfg, allocated, plan)
		if err != nil {
			return nil, nil, err
		}
		routes, err := routeDocument(doc, l, plan, cfg)
		if err != nil {
			return nil, nil, err
		}
		grew := false
		for channelName, count := range routes.LaneCounts {
			if count > allocated[channelName] {
				allocated[channelName] = count
				grew = true
			}
		}
		ports := allocatePorts(l, routes)
		seamGrew, err := assignSeamTracks(doc, l, plan, ports, cfg)
		if err != nil {
			return nil, nil, err
		}
		grew = grew || seamGrew
		if !grew {
			if err := rerouteSeams(doc, l, plan, routes, cfg); err != nil {
				return nil, nil, err
			}
			if err := assignCrossbarTracks(doc, l, plan, routes, ports, cfg); err != nil {
				return nil, nil, err
			}
			return l, routes, nil
		}
	}
	return nil, nil, fmt.Errorf("boxz: routing-space sizing did not converge")
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
	// along each side. Measuring reserved enough side length for this count.
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

// displayRoute converts a center-line graph path to its assigned lane and exact
// node ports without introducing lane-width jogs.
func displayRoute(route *routedEdge, routes *routeResult, ports map[portKey]point, cfg Config) []point {
	type straightRun struct {
		a          point
		b          point
		horizontal bool
	}
	if len(route.Channels) == 0 {
		return route.Points
	}
	// Seam routes already contain final track coordinates and therefore have
	// empty channel tags. Named outer-channel runs still need lane offsets. Both
	// route kinds need projection from center-line geometry to exact node ports.

	// A route can cross from a channel into a collinear hierarchy riser. Offset
	// the whole straight run as one unit; offsetting its graph segments
	// independently creates a meaningless lane-width jog at their boundary.
	var runs []straightRun
	for first := 0; first < len(route.Channels); {
		horizontal := route.Points[first].Y == route.Points[first+1].Y
		last := first
		for last+1 < len(route.Channels) {
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
				if (a.X == b.X && b.X == c.X) || (a.Y == b.Y && b.Y == c.Y) {
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

func writeNodes(svg *strings.Builder, p *placement) {
	if p.Element.Kind == KindNode {
		fmt.Fprintf(svg, "    <g class=\"boxz-node-group\" data-node=\"%s\">\n", html.EscapeString(p.Element.ID))
		fmt.Fprintf(svg, "      <rect class=\"boxz-node\" x=\"%s\" y=\"%s\" width=\"%s\" height=\"%s\" rx=\"6\"/>\n",
			number(p.Rect.X), number(p.Rect.Y), number(p.Rect.W), number(p.Rect.H))
		center := p.Rect.center()
		fmt.Fprintf(svg, "      <text class=\"boxz-title\" x=\"%s\" y=\"%s\">%s</text>\n",
			number(center.X), number(center.Y), html.EscapeString(p.Element.Title))
		svg.WriteString("    </g>\n")
		return
	}
	for _, child := range p.Children {
		writeNodes(svg, child)
	}
}

func writeGroups(svg *strings.Builder, p *placement) {
	if p.Element.Kind == KindNode {
		return
	}
	if p.Element.Title != "" {
		fmt.Fprintf(svg, "    <rect class=\"boxz-group-boundary\" data-container=\"%s\" data-kind=\"%s\" x=\"%s\" y=\"%s\" width=\"%s\" height=\"%s\"/>\n",
			html.EscapeString(p.Element.ID), p.Element.Kind,
			number(p.Rect.X), number(p.Rect.Y), number(p.Rect.W), number(p.Rect.H))
	}
	for _, child := range p.Children {
		writeGroups(svg, child)
	}
}

func writeGroupLabels(svg *strings.Builder, labels []groupLabel) {
	for _, label := range labels {
		center := label.rect.center()
		fmt.Fprintf(svg, "    <g class=\"boxz-group-label\" data-container=\"%s\">\n", html.EscapeString(label.container))
		fmt.Fprintf(svg, "      <title>%s</title>\n", html.EscapeString(label.fullText))
		fmt.Fprintf(svg, "      <rect class=\"boxz-group-label-bg\" x=\"%s\" y=\"%s\" width=\"%s\" height=\"%s\"/>\n",
			number(label.rect.X), number(label.rect.Y), number(label.rect.W), number(label.rect.H))
		if label.text != "" {
			fmt.Fprintf(svg, "      <text class=\"boxz-group-title\" x=\"%s\" y=\"%s\">%s</text>\n",
				number(center.X), number(center.Y), html.EscapeString(label.text))
		}
		svg.WriteString("    </g>\n")
	}
}

func writeContainers(svg *strings.Builder, p *placement) {
	if p.Element.Kind == KindNode {
		return
	}
	fmt.Fprintf(svg, "    <rect class=\"boxz-container\" data-container=\"%s\" data-kind=\"%s\" x=\"%s\" y=\"%s\" width=\"%s\" height=\"%s\"/>\n",
		html.EscapeString(p.Element.ID), p.Element.Kind,
		number(p.Rect.X), number(p.Rect.Y), number(p.Rect.W), number(p.Rect.H))
	for _, child := range p.Children {
		writeContainers(svg, child)
	}
}

func writeRoutingGraph(svg *strings.Builder, routes *routeResult) {
	for _, s := range routes.Segments {
		if s.A == s.B {
			continue
		}
		kind := "channel"
		if s.channel == "" {
			kind = "riser"
		} else if _, ok := routes.Crossbars[s.channel]; ok {
			kind = "crossbar"
		}
		class := "boxz-debug-route boxz-debug-" + kind
		if len(routes.ChannelUses[s.channel]) != 0 {
			class += " boxz-debug-used"
		}
		fmt.Fprintf(svg, "    <line class=\"%s\"", class)
		if s.channel != "" {
			fmt.Fprintf(svg, " data-resource=\"%s\"", html.EscapeString(s.channel))
		}
		fmt.Fprintf(svg, " x1=\"%s\" y1=\"%s\" x2=\"%s\" y2=\"%s\"/>\n",
			number(s.A.X), number(s.A.Y), number(s.B.X), number(s.B.Y))
	}
}

func writeDebugPorts(svg *strings.Builder, ports map[portKey]point) {
	keys := make([]portKey, 0, len(ports))
	for key := range ports {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.node != right.node {
			return left.node < right.node
		}
		if left.side != right.side {
			return sideOrder(left.side) < sideOrder(right.side)
		}
		if left.edge != right.edge {
			return left.edge < right.edge
		}
		return !left.to && right.to
	})
	for _, key := range keys {
		p := ports[key]
		end := "from"
		if key.to {
			end = "to"
		}
		fmt.Fprintf(svg, "    <circle class=\"boxz-debug-port\" data-node=\"%s\" data-side=\"%s\" data-edge=\"%d\" data-end=\"%s\" cx=\"%s\" cy=\"%s\" r=\"2.5\"/>\n",
			html.EscapeString(key.node), key.side, key.edge, end, number(p.X), number(p.Y))
	}
}

func sideOrder(side Side) int {
	switch side {
	case North:
		return 0
	case East:
		return 1
	case South:
		return 2
	case West:
		return 3
	default:
		return 4
	}
}

func svgPath(points []point) string {
	if len(points) == 0 {
		return ""
	}
	var result strings.Builder
	fmt.Fprintf(&result, "M %s %s", number(points[0].X), number(points[0].Y))
	for _, p := range points[1:] {
		fmt.Fprintf(&result, " L %s %s", number(p.X), number(p.Y))
	}
	return result.String()
}

func number(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

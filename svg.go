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
	l, routes, ports, err := solve(doc, cfg)
	if err != nil {
		return err
	}

	displayRoutes := make([][]point, len(routes.Edges))
	for index, route := range routes.Edges {
		displayRoutes[index] = route.Display
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
		if s.kind == segmentRiser {
			kind = "riser"
		} else if s.kind == segmentCrossbar {
			kind = "crossbar"
		}
		class := "boxz-debug-route boxz-debug-" + kind
		if len(routes.ResourceUses[s.resource]) != 0 || len(routes.ChannelUses[s.domain]) != 0 || len(routes.ConnectorUses[s.domain]) != 0 {
			class += " boxz-debug-used"
		}
		fmt.Fprintf(svg, "    <line class=\"%s\"", class)
		if s.resource != "" {
			fmt.Fprintf(svg, " data-resource=\"%s\"", html.EscapeString(s.resource))
		}
		if s.domain != "" && s.domain != s.resource {
			fmt.Fprintf(svg, " data-domain=\"%s\"", html.EscapeString(s.domain))
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

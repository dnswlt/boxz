package boxz

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/dnswlt/boxz/internal/avoid"
)

// RouterKind selects which implementation turns placed boxes into edge
// polylines.
type RouterKind string

const (
	// RouterBuiltin is the structural router in route.go and seam.go.
	RouterBuiltin RouterKind = ""
	// RouterAvoid delegates routing to the boxz-avoid sidecar process. It is
	// experimental: see avoidrouter/README.md.
	RouterAvoid RouterKind = "avoid"
)

const defaultAvoidTimeout = 30 * time.Second

// solveWithAvoid replaces every routing phase after placement. Layout runs
// once: the fixed point in solve only grows outer channel bands for boxz's own
// lane model, and libavoid does not route in channels.
func solveWithAvoid(doc *Document, cfg Config, plan *routingPlan) (*layout, *routeResult, map[portKey]point, error) {
	l, err := buildLayout(doc, cfg, map[string]int{}, plan)
	if err != nil {
		return nil, nil, nil, err
	}
	request, portSides, err := buildAvoidRequest(doc, l, plan, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	client, err := avoid.Start(cfg.AvoidBinary, os.Stderr)
	if err != nil {
		return nil, nil, nil, err
	}
	defer client.Close()
	timeout := cfg.AvoidTimeout
	if timeout <= 0 {
		timeout = defaultAvoidTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := client.Route(ctx, request)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, nil, nil, fmt.Errorf("boxz: avoid router did not finish within %s: %w", timeout, err)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("boxz: avoid router: %w", err)
	}
	for _, warning := range response.Warnings {
		fmt.Fprintf(os.Stderr, "boxz: avoid router: %s\n", warning)
	}
	if len(response.Routes) != len(doc.Edges) {
		return nil, nil, nil, fmt.Errorf("boxz: avoid router returned %d routes for %d edges",
			len(response.Routes), len(doc.Edges))
	}

	routes := &routeResult{
		Edges:            make([]*routedEdge, len(doc.Edges)),
		ChannelCapacity:  make(map[string]int),
		LaneByEdge:       make(map[int]map[string]int),
		ChannelUses:      make(map[string][]int),
		ChannelLaneCount: make(map[string]int),
		ResourceUses:     make(map[string][]int),
		ConnectorUses:    make(map[string][]int),
		Connectors:       make(map[string]connectorSpec),
		Domains:          make(map[string]connectorDomain),
	}
	ports := make(map[portKey]point)
	for index, edge := range doc.Edges {
		chosen := response.Routes[index]
		if len(chosen.Points) < 2 {
			return nil, nil, nil, fmt.Errorf("boxz: avoid router returned no path for %s -> %s",
				edge.From, edge.To)
		}
		points := avoidPoints(chosen.Points)
		last := len(points) - 1
		fromSide := resolveSide(chosen.From.Side, portSides[chosen.From.Port], points[0], points[1])
		toSide := resolveSide(chosen.To.Side, portSides[chosen.To.Port], points[last], points[last-1])
		routes.Edges[index] = &routedEdge{
			EdgeIndex: index,
			From:      edge.From,
			To:        edge.To,
			FromSide:  fromSide,
			ToSide:    toSide,
			Points:    points,
			Display:   points,
		}
		// Nudging slides the segment attached to a shape, so record where the
		// route landed rather than where its candidate port was.
		ports[portKey{node: edge.From, side: fromSide, edge: index}] = points[0]
		ports[portKey{node: edge.To, side: toSide, edge: index, to: true}] = points[last]
	}
	return l, routes, ports, nil
}

func avoidPoints(in []avoid.Point) []point {
	out := make([]point, len(in))
	for index, p := range in {
		out[index] = point{X: p.X, Y: p.Y}
	}
	return out
}

func avoidOptions(cfg Config) *avoid.Options {
	options := &avoid.Options{}
	if cfg.LaneSpacing > 0 {
		lane := cfg.LaneSpacing
		options.IdealNudgingDistance = &lane
	}
	if cfg.ChannelPadding > 0 {
		buffer := cfg.ChannelPadding
		options.ShapeBufferDistance = &buffer
	}
	return options
}

// buildAvoidRequest turns placed geometry into obstacles and candidate ports,
// returning the boxz side of each port so the response maps back. Ports are
// hints that pick a side; libavoid owns the endpoint. See avoidrouter/README.md.
func buildAvoidRequest(doc *Document, l *layout, plan *routingPlan, cfg Config) (*avoid.Request, map[string]Side, error) {
	request := &avoid.Request{ID: "boxz", Options: avoidOptions(cfg)}
	shared := false
	portSides := make(map[string]Side)
	portsBySide := make(map[string]map[Side][]string)

	// Only leaf nodes are obstacles. The experimental sidecar cannot express the
	// built-in router's endpoint-scoped bounded regions, so container boundaries
	// remain legal to cross here; the display pass moves labels clear of routes.
	var visit func(*placement)
	visit = func(p *placement) {
		if p.Element.Kind == KindNode {
			obstacle := avoid.Obstacle{ID: p.Element.ID, Rect: avoidRect(p.Rect), ExclusivePorts: &shared}
			sides := make(map[Side][]string)
			for _, side := range []Side{North, East, South, West} {
				count := plan.PortCount[p.Element.ID][side]
				for _, allowed := range allowedSides(p.Element) {
					if allowed == side {
						count += plan.OuterDegree[p.Element.ID]
					}
				}
				// One pin per anticipated edge. libavoid need not use these
				// positions, but chooses better than with one pin per side.
				for index := 0; index < count; index++ {
					id := string(side) + strconv.Itoa(index)
					obstacle.Ports = append(obstacle.Ports, avoid.Port{
						ID:   id,
						Side: avoidSide(side),
						Pos:  float64(index+1) / float64(count+1),
					})
					portSides[id] = side
					sides[side] = append(sides[side], id)
				}
			}
			portsBySide[p.Element.ID] = sides
			request.Obstacles = append(request.Obstacles, obstacle)
			return
		}
		for _, child := range p.Children {
			visit(child)
		}
	}
	visit(l.Root)

	// Nothing bounds a libavoid route to the diagram. The root sits
	// cfg.CanvasMargin inside the canvas, leaving the frame free for routing.
	request.Obstacles = append(request.Obstacles, canvasFrame(l)...)

	for index, edge := range doc.Edges {
		from, err := avoidEndpoint(edge.From, edge.FromSide, portsBySide)
		if err != nil {
			return nil, nil, err
		}
		to, err := avoidEndpoint(edge.To, edge.ToSide, portsBySide)
		if err != nil {
			return nil, nil, err
		}
		request.Edges = append(request.Edges, avoid.Edge{
			ID:   strconv.Itoa(index),
			From: from,
			To:   to,
		})
	}

	return request, portSides, nil
}

// avoidEndpoint restricts an endpoint to one side when the source constrained
// it. Topology validation has already rejected a side the node cannot offer.
func avoidEndpoint(node string, side *Side, portsBySide map[string]map[Side][]string) (avoid.Endpoint, error) {
	endpoint := avoid.Endpoint{Obstacle: node}
	if side == nil {
		return endpoint, nil
	}
	ids := portsBySide[node][*side]
	if len(ids) == 0 {
		return endpoint, fmt.Errorf("boxz: node %q has no ports on side %s", node, *side)
	}
	endpoint.Ports = ids
	return endpoint, nil
}

// canvasFrame returns four obstacles enclosing the canvas.
func canvasFrame(l *layout) []avoid.Obstacle {
	const thickness = 1000
	return []avoid.Obstacle{
		{ID: "canvas:north", Rect: avoid.Rect{X: -thickness, Y: -thickness, W: l.Width + 2*thickness, H: thickness}},
		{ID: "canvas:south", Rect: avoid.Rect{X: -thickness, Y: l.Height, W: l.Width + 2*thickness, H: thickness}},
		{ID: "canvas:west", Rect: avoid.Rect{X: -thickness, Y: 0, W: thickness, H: l.Height}},
		{ID: "canvas:east", Rect: avoid.Rect{X: l.Width, Y: 0, W: thickness, H: l.Height}},
	}
}

func avoidRect(r rect) avoid.Rect {
	return avoid.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
}

func avoidSide(side Side) avoid.Side {
	switch side {
	case North:
		return avoid.North
	case East:
		return avoid.East
	case South:
		return avoid.South
	default:
		return avoid.West
	}
}

// resolveSide prefers the port the router reported, falls back to the side it
// named, and finally infers one from the terminal segment's direction.
func resolveSide(reported avoid.Side, known Side, terminal, inward point) Side {
	if known != "" {
		return known
	}
	switch reported {
	case avoid.North:
		return North
	case avoid.East:
		return East
	case avoid.South:
		return South
	case avoid.West:
		return West
	}
	if terminal.X == inward.X {
		if inward.Y < terminal.Y {
			return North
		}
		return South
	}
	if inward.X < terminal.X {
		return West
	}
	return East
}

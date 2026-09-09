package boxz

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// Config contains the deliberately simple geometry constants used by the MVP.
type Config struct {
	CharacterWidth float64
	LineHeight     float64
	NodePaddingX   float64
	NodePaddingY   float64
	MinNodeWidth   float64
	MinNodeHeight  float64
	ChildGap       float64
	AlongPadding   float64
	ChannelSize    float64
	ChannelPadding float64
	LaneSpacing    float64
	CanvasMargin   float64
}

// DefaultConfig returns conservative dimensions suitable for the bundled SVG style.
func DefaultConfig() Config {
	return Config{
		CharacterWidth: 8,
		LineHeight:     18,
		NodePaddingX:   16,
		NodePaddingY:   11,
		MinNodeWidth:   64,
		MinNodeHeight:  40,
		ChildGap:       48,
		AlongPadding:   24,
		ChannelSize:    28,
		ChannelPadding: 7,
		LaneSpacing:    8,
		CanvasMargin:   20,
	}
}

type point struct {
	X float64
	Y float64
}

type rect struct {
	X float64
	Y float64
	W float64
	H float64
}

func (r rect) center() point { return point{X: r.X + r.W/2, Y: r.Y + r.H/2} }

type channel struct {
	ID string
	A  point
	B  point
}

type placement struct {
	Element  *Element
	Rect     rect
	Children []*placement
	Channels map[Side]*channel
}

type layout struct {
	Root     *placement
	ByID     map[string]*placement
	Channels map[string]*channel
	Width    float64
	Height   float64
}

type measured struct {
	element  *Element
	w        float64
	h        float64
	children []*measured
	gaps     []float64
	top      float64
	right    float64
	bottom   float64
	left     float64
}

func buildLayout(doc *Document, cfg Config, laneCounts map[string]int, plan *routingPlan) (*layout, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	m := measureElement(doc.Root, cfg, laneCounts, plan)
	result := &layout{ByID: make(map[string]*placement), Channels: make(map[string]*channel)}
	result.Root = placeElement(m, cfg.CanvasMargin, cfg.CanvasMargin, cfg, result)
	result.Width = m.w + 2*cfg.CanvasMargin
	result.Height = m.h + 2*cfg.CanvasMargin
	return result, nil
}

func validateConfig(cfg Config) error {
	values := []struct {
		name  string
		value float64
	}{
		{"CharacterWidth", cfg.CharacterWidth}, {"LineHeight", cfg.LineHeight},
		{"NodePaddingX", cfg.NodePaddingX}, {"NodePaddingY", cfg.NodePaddingY},
		{"MinNodeWidth", cfg.MinNodeWidth}, {"MinNodeHeight", cfg.MinNodeHeight},
		{"ChildGap", cfg.ChildGap}, {"AlongPadding", cfg.AlongPadding},
		{"ChannelSize", cfg.ChannelSize}, {"ChannelPadding", cfg.ChannelPadding},
		{"LaneSpacing", cfg.LaneSpacing}, {"CanvasMargin", cfg.CanvasMargin},
	}
	for _, item := range values {
		if item.value < 0 {
			return fmt.Errorf("boxz: Config.%s must not be negative", item.name)
		}
	}
	if cfg.CharacterWidth == 0 || cfg.LineHeight == 0 || cfg.LaneSpacing == 0 {
		return fmt.Errorf("boxz: character width, line height, and lane spacing must be positive")
	}
	return nil
}

func measureElement(element *Element, cfg Config, lanes map[string]int, plan *routingPlan) *measured {
	m := &measured{element: element}
	if element.Kind == KindNode {
		m.w = math.Max(cfg.MinNodeWidth, float64(utf8.RuneCountInString(element.Title))*cfg.CharacterWidth+2*cfg.NodePaddingX)
		m.h = math.Max(cfg.MinNodeHeight, cfg.LineHeight+2*cfg.NodePaddingY)
		ports := plan.PortCount[element.ID]
		north, east := ports[North], ports[East]
		south, west := ports[South], ports[West]
		outer := plan.OuterDegree[element.ID]
		sides := allowedSides(element)
		if sides[0] == North {
			north += outer
			south += outer
		} else {
			west += outer
			east += outer
		}
		if capacity := maxInt(north, south); capacity > 0 {
			m.w = math.Max(m.w, float64(capacity+1)*cfg.LaneSpacing)
		}
		if capacity := maxInt(west, east); capacity > 0 {
			m.h = math.Max(m.h, float64(capacity+1)*cfg.LaneSpacing)
		}
		return m
	}

	for _, child := range element.Children {
		m.children = append(m.children, measureElement(child, cfg, lanes, plan))
	}
	for index := 0; index+1 < len(element.Children); index++ {
		m.gaps = append(m.gaps, seamBand(cfg, plan.SeamLaneCount[seamID(element.ID, index)]))
	}
	if element.Kind == KindHBox {
		m.top = channelBand(cfg, lanes[channelID(element.ID, North)])
		m.bottom = channelBand(cfg, lanes[channelID(element.ID, South)])
		maxHeight := 0.0
		for index, child := range m.children {
			m.w += child.w
			if index < len(m.gaps) {
				m.w += m.gaps[index]
			}
			maxHeight = math.Max(maxHeight, child.h)
		}
		m.w += 2 * cfg.AlongPadding
		m.h = m.top + maxHeight + m.bottom
	} else {
		m.left = channelBand(cfg, lanes[channelID(element.ID, West)])
		m.right = channelBand(cfg, lanes[channelID(element.ID, East)])
		maxWidth := 0.0
		for index, child := range m.children {
			m.h += child.h
			if index < len(m.gaps) {
				m.h += m.gaps[index]
			}
			maxWidth = math.Max(maxWidth, child.w)
		}
		m.h += 2 * cfg.AlongPadding
		m.w = m.left + maxWidth + m.right
	}
	return m
}

func channelBand(cfg Config, lanes int) float64 {
	needed := float64(lanes)*cfg.LaneSpacing + 2*cfg.ChannelPadding
	return math.Max(cfg.ChannelSize, needed)
}

func seamBand(cfg Config, lanes int) float64 {
	needed := float64(lanes)*cfg.LaneSpacing + 2*cfg.ChannelPadding
	return math.Max(cfg.ChildGap, needed)
}

func placeElement(m *measured, x, y float64, cfg Config, result *layout) *placement {
	p := &placement{
		Element:  m.element,
		Rect:     rect{X: x, Y: y, W: m.w, H: m.h},
		Channels: make(map[Side]*channel),
	}
	result.ByID[m.element.ID] = p
	if m.element.Kind == KindNode {
		return p
	}

	if m.element.Kind == KindHBox {
		cursor := x + cfg.AlongPadding
		contentHeight := m.h - m.top - m.bottom
		for index, child := range m.children {
			childY := y + m.top + (contentHeight-child.h)/2
			p.Children = append(p.Children, placeElement(child, cursor, childY, cfg, result))
			cursor += child.w
			if index < len(m.gaps) {
				cursor += m.gaps[index]
			}
		}
		addChannel(result, p, North,
			point{X: x + cfg.AlongPadding/2, Y: y + m.top/2},
			point{X: x + m.w - cfg.AlongPadding/2, Y: y + m.top/2})
		addChannel(result, p, South,
			point{X: x + cfg.AlongPadding/2, Y: y + m.h - m.bottom/2},
			point{X: x + m.w - cfg.AlongPadding/2, Y: y + m.h - m.bottom/2})
	} else {
		cursor := y + cfg.AlongPadding
		contentWidth := m.w - m.left - m.right
		for index, child := range m.children {
			childX := x + m.left + (contentWidth-child.w)/2
			p.Children = append(p.Children, placeElement(child, childX, cursor, cfg, result))
			cursor += child.h
			if index < len(m.gaps) {
				cursor += m.gaps[index]
			}
		}
		addChannel(result, p, West,
			point{X: x + m.left/2, Y: y + cfg.AlongPadding/2},
			point{X: x + m.left/2, Y: y + m.h - cfg.AlongPadding/2})
		addChannel(result, p, East,
			point{X: x + m.w - m.right/2, Y: y + cfg.AlongPadding/2},
			point{X: x + m.w - m.right/2, Y: y + m.h - cfg.AlongPadding/2})
	}
	return p
}

func addChannel(result *layout, owner *placement, side Side, a, b point) {
	c := &channel{ID: channelID(owner.Element.ID, side), A: a, B: b}
	owner.Channels[side] = c
	result.Channels[c.ID] = c
}

func channelID(owner string, side Side) string { return owner + ":" + string(side) }

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

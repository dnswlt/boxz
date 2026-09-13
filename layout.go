package boxz

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Config contains rendering options and the deliberately simple geometry
// constants used by the MVP.
type Config struct {
	// Debug adds diagnostic SVG layers without changing layout or routing.
	Debug              bool
	CharacterWidth     float64
	LineHeight         float64
	NodePaddingX       float64
	NodePaddingY       float64
	GroupLabelPaddingX float64
	GroupLabelPaddingY float64
	MinNodeWidth       float64
	MinNodeHeight      float64
	ChildGap           float64
	AlongPadding       float64
	ChannelSize        float64
	ChannelPadding     float64
	LaneSpacing        float64
	CanvasMargin       float64
	// EdgeRouter selects the routing implementation. The zero value is the
	// built-in structural router.
	EdgeRouter RouterKind
	// AvoidBinary overrides discovery of the boxz-avoid executable. Only
	// consulted when EdgeRouter is RouterAvoid.
	AvoidBinary string
	// AvoidTimeout bounds routing with boxz-avoid; the router is killed when it
	// expires. Zero means 30 seconds.
	AvoidTimeout time.Duration
}

// DefaultConfig returns conservative dimensions suitable for the bundled SVG style.
func DefaultConfig() Config {
	return Config{
		CharacterWidth:     8,
		LineHeight:         18,
		NodePaddingX:       16,
		NodePaddingY:       11,
		GroupLabelPaddingX: 8,
		GroupLabelPaddingY: 5,
		MinNodeWidth:       64,
		MinNodeHeight:      40,
		ChildGap:           48,
		AlongPadding:       24,
		ChannelSize:        28,
		ChannelPadding:     7,
		LaneSpacing:        8,
		CanvasMargin:       20,
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
	// A and B are the channel center line. Lanes are parallel display offsets.
	ID string
	A  point
	B  point
}

type placement struct {
	Element    *Element
	Rect       rect
	LabelStrip rect
	Children   []*placement
	Channels   map[Side]*channel
}

type layout struct {
	Root     *placement
	ByID     map[string]*placement
	Channels map[string]*channel
	Width    float64
	Height   float64
}

// measured is the bottom-up size of an element. Container side fields are the
// channel bands reserved inside its rectangle; gaps reserve sibling seams.
type measured struct {
	element *Element
	w       float64
	h       float64
	// growX/growY say whether the subtree can absorb surplus on each axis.
	growX    bool
	growY    bool
	children []*measured
	gaps     []float64
	top      float64
	right    float64
	bottom   float64
	left     float64
	label    float64
}

// buildLayout separates bottom-up measurement from top-down placement so child
// order and alignment never depend on traversal side effects.
func buildLayout(doc *Document, cfg Config, capacity map[string]int, plan *routingPlan) (*layout, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if err := validateSpringSlots(doc.Root); err != nil {
		return nil, err
	}
	m := measureElement(doc.Root, cfg, capacity, plan)
	result := &layout{ByID: make(map[string]*placement), Channels: make(map[string]*channel)}
	rootSlot := rect{X: cfg.CanvasMargin, Y: cfg.CanvasMargin, W: m.w, H: m.h}
	result.Root = placeElement(m, rootSlot, cfg, result)
	result.Width = m.w + 2*cfg.CanvasMargin
	result.Height = m.h + 2*cfg.CanvasMargin
	return result, nil
}

// validateSpringSlots protects the positional spring representation used during
// placement. A nil slice is the convenient programmatic form for no springs.
func validateSpringSlots(element *Element) error {
	if element == nil {
		return fmt.Errorf("boxz: document has no root element")
	}
	if element.Kind == KindNode {
		if len(element.Springs) != 0 {
			return fmt.Errorf("boxz: node %q cannot contain spring slots", element.ID)
		}
		return nil
	}
	want := len(element.Children) + 1
	if len(element.Springs) != 0 && len(element.Springs) != want {
		return fmt.Errorf("boxz: %s %q has %d spring slots; want %d", element.Kind, element.ID, len(element.Springs), want)
	}
	for _, count := range element.Springs {
		if count < 0 {
			return fmt.Errorf("boxz: %s %q has a negative spring count", element.Kind, element.ID)
		}
	}
	for _, child := range element.Children {
		if err := validateSpringSlots(child); err != nil {
			return err
		}
	}
	return nil
}

func validateConfig(cfg Config) error {
	values := []struct {
		name  string
		value float64
	}{
		{"CharacterWidth", cfg.CharacterWidth}, {"LineHeight", cfg.LineHeight},
		{"NodePaddingX", cfg.NodePaddingX}, {"NodePaddingY", cfg.NodePaddingY},
		{"GroupLabelPaddingX", cfg.GroupLabelPaddingX}, {"GroupLabelPaddingY", cfg.GroupLabelPaddingY},
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

// measureElement reserves node ports, sibling seams, and outer channel bands in
// a bottom-up pass.
func measureElement(element *Element, cfg Config, capacity map[string]int, plan *routingPlan) *measured {
	m := &measured{element: element}
	if element.Kind == KindNode {
		m.w = math.Max(cfg.MinNodeWidth, float64(utf8.RuneCountInString(element.Title))*cfg.CharacterWidth+2*cfg.NodePaddingX)
		m.h = math.Max(cfg.MinNodeHeight, cfg.LineHeight+2*cfg.NodePaddingY)
		ports := plan.PortCount[element.ID]
		north, east := ports[North], ports[East]
		south, west := ports[South], ports[West]
		// Seam sides are known exactly. An outer route chooses one of the two
		// parent-facing sides later, so reserve its degree on both. This may
		// overestimate node size, but can never leave too little port space.
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
		if element.NodeAttributes.Spring && element.Parent != nil {
			m.growX = element.Parent.Kind == KindHBox
			m.growY = element.Parent.Kind == KindVBox
		}
		return m
	}

	for _, child := range element.Children {
		m.children = append(m.children, measureElement(child, cfg, capacity, plan))
	}
	if element.Title != "" {
		m.label = cfg.LineHeight + 2*cfg.GroupLabelPaddingY
	}
	for index := 0; index+1 < len(element.Children); index++ {
		m.gaps = append(m.gaps, seamBand(cfg, plan.SeamTrackCount[seamID(element.ID, index)]))
	}
	if element.Kind == KindHBox {
		m.top = channelBand(cfg, capacity[channelID(element.ID, North)])
		m.bottom = channelBand(cfg, capacity[channelID(element.ID, South)])
		maxHeight := 0.0
		for index, child := range m.children {
			m.w += child.w
			if index < len(m.gaps) {
				m.w += m.gaps[index]
			}
			maxHeight = math.Max(maxHeight, child.h)
			m.growX = m.growX || child.growX
			m.growY = m.growY || child.growY
		}
		m.growX = m.growX || springCount(element) != 0
		m.w += 2 * cfg.AlongPadding
		m.h = m.top + m.label + maxHeight + m.bottom
	} else {
		m.left = channelBand(cfg, capacity[channelID(element.ID, West)])
		m.right = channelBand(cfg, capacity[channelID(element.ID, East)])
		maxWidth := 0.0
		for index, child := range m.children {
			m.h += child.h
			if index < len(m.gaps) {
				m.h += m.gaps[index]
			}
			maxWidth = math.Max(maxWidth, child.w)
			m.growX = m.growX || child.growX
			m.growY = m.growY || child.growY
		}
		m.growY = m.growY || springCount(element) != 0
		m.h += 2*cfg.AlongPadding + m.label
		m.w = m.left + maxWidth + m.right
	}
	// A hierarchy connector allocates tracks across the child container. Reserve
	// enough cross-axis extent that those tracks fit even in a narrow subtree.
	horizontalConnectors := maxInt(hierarchyConnectorCapacity(capacity, element.ID, North), hierarchyConnectorCapacity(capacity, element.ID, South))
	verticalConnectors := maxInt(hierarchyConnectorCapacity(capacity, element.ID, West), hierarchyConnectorCapacity(capacity, element.ID, East))
	margin := math.Max(cfg.AlongPadding, 2*cfg.ChannelPadding)
	if horizontalConnectors > 0 {
		m.w = math.Max(m.w, float64(horizontalConnectors-1)*cfg.LaneSpacing+margin)
	}
	if verticalConnectors > 0 {
		m.h = math.Max(m.h, float64(verticalConnectors-1)*cfg.LaneSpacing+margin)
	}
	return m
}

func hierarchyConnectorCapacity(capacity map[string]int, elementID string, side Side) int {
	prefix := elementID + ":" + string(side) + ":riser:"
	total := 0
	for resource, count := range capacity {
		if strings.HasPrefix(resource, prefix) {
			total += count
		}
	}
	return total
}

func springCount(element *Element) int {
	total := 0
	for _, count := range element.Springs {
		total += count
	}
	return total
}

func springSlot(element *Element, index int) int {
	if element.Springs == nil {
		return 0
	}
	return element.Springs[index]
}

// gapSpringCount maps measured gap i (between children i and i+1) to
// spring slot i+1, since slot 0 is the leading edge of the container.
func gapSpringCount(element *Element, gapIndex int) int {
	return springSlot(element, gapIndex+1)
}

// growthWeight counts equal-weight consumers on a container's stacking axis.
// A run of adjacent springs retains its count, while a growable child is one
// spring-like box regardless of how it distributes that space internally.
func growthWeight(m *measured) int {
	weight := springCount(m.element)
	for _, child := range m.children {
		if (m.element.Kind == KindHBox && child.growX) || (m.element.Kind == KindVBox && child.growY) {
			weight++
		}
	}
	return weight
}

func channelBand(cfg Config, lanes int) float64 {
	needed := float64(lanes)*cfg.LaneSpacing + 2*cfg.ChannelPadding
	return math.Max(cfg.ChannelSize, needed)
}

func seamBand(cfg Config, lanes int) float64 {
	needed := float64(lanes)*cfg.LaneSpacing + 2*cfg.ChannelPadding
	return math.Max(cfg.ChildGap, needed)
}

// placeElement arranges an element within its assigned slot. Edge springs may
// make the element's compact routing rectangle smaller than that slot.
func placeElement(m *measured, slot rect, cfg Config, result *layout) *placement {
	p := &placement{
		Element:  m.element,
		Rect:     slot,
		Channels: make(map[Side]*channel),
	}
	result.ByID[m.element.ID] = p
	if m.element.Kind == KindNode {
		return p
	}

	if m.element.Kind == KindHBox {
		unit, leading, trailing := springAllocation(m, slot.W-m.w)
		p.Rect.X += leading
		p.Rect.W -= leading + trailing
		cursor := p.Rect.X + cfg.AlongPadding
		contentHeight := slot.H - m.top - m.label - m.bottom
		if m.label != 0 {
			p.LabelStrip = rect{
				X: p.Rect.X + cfg.AlongPadding,
				Y: slot.Y,
				W: p.Rect.W - 2*cfg.AlongPadding,
				H: m.label,
			}
		}
		for index, child := range m.children {
			childW, childH := child.w, child.h
			if child.growX {
				childW += unit
			}
			if child.growY {
				childH = contentHeight
			}
			childY := slot.Y + m.label + m.top + (contentHeight-childH)/2
			childSlot := rect{X: cursor, Y: childY, W: childW, H: childH}
			p.Children = append(p.Children, placeElement(child, childSlot, cfg, result))
			cursor += childW
			if index < len(m.gaps) {
				cursor += m.gaps[index] + float64(gapSpringCount(m.element, index))*unit
			}
		}
		addChannel(result, p, North,
			point{X: p.Rect.X + cfg.AlongPadding/2, Y: slot.Y + m.label + m.top/2},
			point{X: p.Rect.X + p.Rect.W - cfg.AlongPadding/2, Y: slot.Y + m.label + m.top/2})
		addChannel(result, p, South,
			point{X: p.Rect.X + cfg.AlongPadding/2, Y: slot.Y + slot.H - m.bottom/2},
			point{X: p.Rect.X + p.Rect.W - cfg.AlongPadding/2, Y: slot.Y + slot.H - m.bottom/2})
	} else {
		unit, leading, trailing := springAllocation(m, slot.H-m.h)
		p.Rect.Y += leading
		p.Rect.H -= leading + trailing
		cursor := p.Rect.Y + cfg.AlongPadding + m.label
		contentWidth := slot.W - m.left - m.right
		if m.label != 0 {
			p.LabelStrip = rect{
				X: slot.X + m.left,
				Y: p.Rect.Y,
				W: contentWidth,
				H: m.label,
			}
		}
		for index, child := range m.children {
			childW, childH := child.w, child.h
			if child.growX {
				childW = contentWidth
			}
			if child.growY {
				childH += unit
			}
			childX := slot.X + m.left + (contentWidth-childW)/2
			childSlot := rect{X: childX, Y: cursor, W: childW, H: childH}
			p.Children = append(p.Children, placeElement(child, childSlot, cfg, result))
			cursor += childH
			if index < len(m.gaps) {
				cursor += m.gaps[index] + float64(gapSpringCount(m.element, index))*unit
			}
		}
		addChannel(result, p, West,
			point{X: slot.X + m.left/2, Y: p.Rect.Y + cfg.AlongPadding/2},
			point{X: slot.X + m.left/2, Y: p.Rect.Y + p.Rect.H - cfg.AlongPadding/2})
		addChannel(result, p, East,
			point{X: slot.X + slot.W - m.right/2, Y: p.Rect.Y + cfg.AlongPadding/2},
			point{X: slot.X + slot.W - m.right/2, Y: p.Rect.Y + p.Rect.H - cfg.AlongPadding/2})
	}
	return p
}

func springAllocation(m *measured, extra float64) (unit, leading, trailing float64) {
	unit = growthUnit(extra, growthWeight(m))
	leading = float64(springSlot(m.element, 0)) * unit
	trailing = float64(springSlot(m.element, len(m.children))) * unit
	return unit, leading, trailing
}

func growthUnit(extra float64, weight int) float64 {
	if extra <= 0 || weight == 0 {
		return 0
	}
	return extra / float64(weight)
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

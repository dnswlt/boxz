// Package renderconfig contains rendering controls used by the boxz command
// and engine. It is internal so the public library API has one stable render
// behavior while diagnostics and experimental routers remain implementation
// tools.
package renderconfig

import "time"

// RouterKind selects the implementation that turns placed boxes into edge
// polylines.
type RouterKind string

const (
	RouterBuiltin RouterKind = ""
	RouterAvoid   RouterKind = "avoid"
)

// Config contains internal rendering and geometry controls.
type Config struct {
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
	EdgeRouter         RouterKind
	AvoidBinary        string
	AvoidTimeout       time.Duration
}

// Default returns the geometry used by the public renderer and CLI.
func Default() Config {
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

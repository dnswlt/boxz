package boxz

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestContainerLabelReservesOnlyHeight(t *testing.T) {
	untitled, err := ParseString("untitled.boxz", `hbox root { node a }`)
	if err != nil {
		t.Fatal(err)
	}
	titled, err := ParseString("titled.boxz", `hbox root "A title far wider than its child" { node a }`)
	if err != nil {
		t.Fatal(err)
	}
	planUntitled, err := buildRoutingPlan(untitled)
	if err != nil {
		t.Fatal(err)
	}
	planTitled, err := buildRoutingPlan(titled)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	plain, err := buildLayout(untitled, cfg, nil, planUntitled)
	if err != nil {
		t.Fatal(err)
	}
	withLabel, err := buildLayout(titled, cfg, nil, planTitled)
	if err != nil {
		t.Fatal(err)
	}
	if withLabel.Root.Rect.W != plain.Root.Rect.W {
		t.Fatalf("titled width = %g, want unchanged width %g", withLabel.Root.Rect.W, plain.Root.Rect.W)
	}
	wantGrowth := cfg.LineHeight + 2*cfg.GroupLabelPaddingY
	if got := withLabel.Root.Rect.H - plain.Root.Rect.H; got != wantGrowth {
		t.Fatalf("titled height growth = %g, want label strip %g", got, wantGrowth)
	}
	north := withLabel.Root.Channels[North]
	if north == nil || withLabel.Root.LabelStrip.Y+withLabel.Root.LabelStrip.H > north.A.Y {
		t.Fatalf("label strip = %#v, want it above north channel at %v", withLabel.Root.LabelStrip, north)
	}
}

func TestBuildLayoutRejectsMalformedSpringSlots(t *testing.T) {
	child := &Element{Kind: KindNode, ID: "child", Title: "child"}
	root := &Element{
		Kind:     KindHBox,
		ID:       "root",
		Children: []*Element{child},
		Springs:  []int{0},
	}
	child.Parent = root
	doc := &Document{Root: root}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildLayout(doc, DefaultConfig(), nil, plan)
	if err == nil || !strings.Contains(err.Error(), "has 1 spring slots; want 2") {
		t.Fatalf("error = %v, want malformed spring-slot diagnostic", err)
	}
}

func TestHorizontalSpringsShareSurplusByWeight(t *testing.T) {
	doc, err := ParseString("horizontal-springs.boxz", `
vbox root {
  hbox flexible {
    node a "A"
    spring
    spring
    node b "A" [spring]
  }
  hbox widthSource {
    node wide "A title wide enough to establish the row width"
  }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	l, err := buildLayout(doc, DefaultConfig(), nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	a, b := l.ByID["a"].Rect, l.ByID["b"].Rect
	baseGap := seamBand(DefaultConfig(), 0)
	actualGap := b.X - (a.X + a.W)
	nodeGrowth := b.W - a.W
	gapGrowth := actualGap - baseGap
	if nodeGrowth <= 0 || abs(gapGrowth-2*nodeGrowth) > 1e-9 {
		t.Fatalf("node growth = %g, gap growth = %g; want two springs to receive twice one spring-like node", nodeGrowth, gapGrowth)
	}
}

func TestVerticalSpringsShareSurplusByWeight(t *testing.T) {
	doc, err := ParseString("vertical-springs.boxz", `
hbox root {
  vbox flexible {
    node a "A"
    spring
    spring
    node b "A" [spring]
  }
  vbox heightSource {
    node t1
    node t2
    node t3
    node t4
  }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	l, err := buildLayout(doc, DefaultConfig(), nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	a, b := l.ByID["a"].Rect, l.ByID["b"].Rect
	baseGap := seamBand(DefaultConfig(), 0)
	actualGap := b.Y - (a.Y + a.H)
	nodeGrowth := b.H - a.H
	gapGrowth := actualGap - baseGap
	if nodeGrowth <= 0 || abs(gapGrowth-2*nodeGrowth) > 1e-9 {
		t.Fatalf("node growth = %g, gap growth = %g; want two springs to receive twice one spring-like node", nodeGrowth, gapGrowth)
	}
}

func TestSpringNodeDoesNotGrowAcrossParentAxis(t *testing.T) {
	doc, err := ParseString("spring-axis.boxz", `
vbox root {
  node a "A" [spring]
  node widthSource "A title wide enough to establish the column width"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	l, err := buildLayout(doc, DefaultConfig(), nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	a := l.ByID["a"].Rect
	if abs(a.W-DefaultConfig().MinNodeWidth) > 1e-9 {
		t.Fatalf("spring node width = %g, want unchanged cross-axis width %g", a.W, DefaultConfig().MinNodeWidth)
	}
}

func TestEdgeSpringsAlignCompactRoutingRectangles(t *testing.T) {
	doc, err := ParseString("edge-springs.boxz", `
vbox root {
  hbox start { node a node b spring }
  hbox end { spring node c node d }
  hbox center { spring node e node f spring }
  hbox widthSource { node wide "A title wide enough to establish the row width" }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	l, err := buildLayout(doc, DefaultConfig(), nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	root := l.ByID["root"]
	contentLeft := root.Rect.X + (root.Channels[West].A.X-root.Rect.X)*2
	contentRight := root.Rect.X + root.Rect.W - (root.Rect.X+root.Rect.W-root.Channels[East].A.X)*2
	start, end, center := l.ByID["start"].Rect, l.ByID["end"].Rect, l.ByID["center"].Rect
	if abs(start.X-contentLeft) > 1e-9 {
		t.Fatalf("trailing spring row starts at %g, want content start %g", start.X, contentLeft)
	}
	if abs(end.X+end.W-contentRight) > 1e-9 {
		t.Fatalf("leading spring row ends at %g, want content end %g", end.X+end.W, contentRight)
	}
	leftSpace, rightSpace := center.X-contentLeft, contentRight-(center.X+center.W)
	if abs(leftSpace-rightSpace) > 1e-9 {
		t.Fatalf("two-sided springs leave %g left and %g right, want centered content", leftSpace, rightSpace)
	}
	if abs(start.W-end.W) > 1e-9 || abs(end.W-center.W) > 1e-9 {
		t.Fatalf("edge springs changed compact routing widths: start=%g end=%g center=%g", start.W, end.W, center.W)
	}
}

func TestSpringGrowthPropagatesThroughNestedContainers(t *testing.T) {
	doc, err := ParseString("nested-springs.boxz", `
vbox root {
  vbox wrapper {
    hbox flexible {
      node a
      spring
      node b
    }
  }
  hbox widthSource {
    node wide "A title wide enough to establish the outer width"
  }
}
edges {
  a -> b
  b -> wide
}
`)
	if err != nil {
		t.Fatal(err)
	}
	l, _, _, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	a, b := l.ByID["a"].Rect, l.ByID["b"].Rect
	if gap := b.X - (a.X + a.W); gap <= seamBand(DefaultConfig(), 0) {
		t.Fatalf("nested spring gap = %g, want more than its intrinsic seam width", gap)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
}

func TestHierarchyConnectorCapacityCanGrowNestedContainer(t *testing.T) {
	doc, err := ParseString("riser-demand.boxz", `hbox root { hbox child { node a } node b }`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	const tracks = 30
	l, err := buildLayout(doc, cfg, map[string]int{riserID("child", North, 0): tracks}, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := float64(tracks-1)*cfg.LaneSpacing + math.Max(cfg.AlongPadding, 2*cfg.ChannelPadding)
	if l.ByID["child"].Rect.W < want {
		t.Fatalf("child width = %g, want at least %g for %d connector tracks", l.ByID["child"].Rect.W, want, tracks)
	}
}

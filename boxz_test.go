package boxz

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const exampleSource = `
vbox root {
  hbox services {
    node client "Client"
    node api "API Server"
  }
  node database
}
edges {
  client -> api
  api -> database
}
`

func TestParsePreservesOrderAndDefaultsTitle(t *testing.T) {
	doc, err := ParseString("test.boxz", exampleSource)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Root.ID != "root" || doc.Root.Kind != KindVBox {
		t.Fatalf("unexpected root: %#v", doc.Root)
	}
	if got := doc.Root.Children[0].ID; got != "services" {
		t.Fatalf("first child = %q, want services", got)
	}
	if got := doc.Root.Children[0].Children[1].ID; got != "api" {
		t.Fatalf("second hbox child = %q, want api", got)
	}
	if got := doc.Root.Children[1].Title; got != "database" {
		t.Fatalf("default title = %q, want database", got)
	}
	if len(doc.Edges) != 2 || doc.Edges[0].From != "client" || doc.Edges[1].From != "api" {
		t.Fatalf("edges = %#v, want source order preserved", doc.Edges)
	}
}

func TestParseEndpointSides(t *testing.T) {
	source := `// constrained ports
hbox root { node a node b }
edges { a:N -> b:S }`
	doc, err := ParseString("sides.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Edges[0].FromSide == nil || *doc.Edges[0].FromSide != North {
		t.Fatalf("source side = %#v, want N", doc.Edges[0].FromSide)
	}
	if doc.Edges[0].ToSide == nil || *doc.Edges[0].ToSide != South {
		t.Fatalf("target side = %#v, want S", doc.Edges[0].ToSide)
	}
}

func TestParseAllowsEmptyEdgesBlock(t *testing.T) {
	doc, err := ParseString("empty-edges.boxz", `node root
edges {}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Edges) != 0 {
		t.Fatalf("edges = %#v, want none", doc.Edges)
	}
}

func TestParseSpringsAndNodeAttributes(t *testing.T) {
	doc, err := ParseString("springs.boxz", `
hbox root {
  spring
  spring
  node a "A" [spring,]
  spring
  node b [spring = false]
  spring
  spring
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc.Root.Springs, []int{2, 1, 2}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("springs = %v, want %v", got, want)
	}
	if !doc.Root.Children[0].NodeAttributes.Spring {
		t.Fatal("bare spring attribute did not enable node growth")
	}
	if doc.Root.Children[1].NodeAttributes.Spring {
		t.Fatal("spring = false enabled node growth")
	}
}

func TestParseContainerTitlesAndLabelAlignment(t *testing.T) {
	doc, err := ParseString("groups.boxz", `
vbox root "System" [labelAlign = center] {
  hbox services "Services" [labelAlign = RIGHT] {
    node api
  }
  hbox automatic "Automatic" [labelAlign = auto, bounded = false] { node worker }
  hbox boundary [bounded] { node database }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Root.Title != "System" || doc.Root.ContainerAttributes.LabelAlign != LabelAlignCenter {
		t.Fatalf("root title/attributes = %q/%q, want System/center", doc.Root.Title, doc.Root.ContainerAttributes.LabelAlign)
	}
	if !doc.Root.ContainerAttributes.Bounded {
		t.Fatal("titled root did not default to bounded")
	}
	services := doc.Root.Children[0]
	if services.Title != "Services" || services.ContainerAttributes.LabelAlign != LabelAlignRight {
		t.Fatalf("services title/attributes = %q/%q, want Services/right", services.Title, services.ContainerAttributes.LabelAlign)
	}
	if got := doc.Root.Children[1].ContainerAttributes.LabelAlign; got != LabelAlignAuto {
		t.Fatalf("explicit automatic alignment = %q, want auto", got)
	}
	if doc.Root.Children[1].ContainerAttributes.Bounded {
		t.Fatal("bounded = false did not override the titled default")
	}
	if !doc.Root.Children[2].ContainerAttributes.Bounded {
		t.Fatal("bare bounded attribute did not bound an untitled container")
	}
}

func TestAttributeGrammarAcceptsScalarValuesAndTrailingComma(t *testing.T) {
	parsed, err := documentParser.ParseString("attributes.boxz", `node root [text = "foo", integer = 17, decimal = -2.5, enabled = true,]`)
	if err != nil {
		t.Fatal(err)
	}
	attributes := parsed.Root.Attributes.Items
	if len(attributes) != 4 || *attributes[0].Value.String != "foo" || attributes[1].Value.Number.Value != "17" ||
		!attributes[2].Value.Number.Negative || attributes[2].Value.Number.Value != "2.5" || *attributes[3].Value.Ident != "true" {
		t.Fatalf("attributes = %#v, want string, integer, decimal, and identifier values", attributes)
	}
}

func TestParseRejectsInvalidAttributesAndEmptySpringContainer(t *testing.T) {
	tests := map[string]struct {
		source string
		want   string
	}{
		"duplicate":          {`hbox root { node a [spring, spring] }`, "duplicate attribute"},
		"unknown":            {`hbox root { node a [sprung] }`, "attribute \"sprung\" is not supported"},
		"invalid spring":     {`hbox root { node a [spring = 1] }`, "must be a flag or boolean"},
		"container attr":     {`hbox root [spring] { node a }`, "is not supported on hbox"},
		"invalid alignment":  {`hbox root "Root" [labelAlign = north] { node a }`, "must be auto, left, center, or right"},
		"bare alignment":     {`hbox root "Root" [labelAlign] { node a }`, "must be auto, left, center, or right"},
		"invalid bounded":    {`hbox root [bounded = 1] { node a }`, "must be a flag or boolean"},
		"align no title":     {`hbox root [labelAlign = left] { node a }`, "has labelAlign but no title"},
		"old attribute name": {`hbox root "Root" [labelAnchor = left] { node a }`, "attribute \"labelAnchor\" is not supported"},
		"only springs":       {`hbox root { spring spring }`, "must contain at least one child"},
		"spring root node":   {`node root [spring]`, "root node \"root\" cannot be spring-enabled"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseString("attributes.boxz", test.source)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want diagnostic containing %q", err, test.want)
			}
		})
	}
}

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

func TestAutomaticGroupLabelPlacementAvoidsRoutes(t *testing.T) {
	cfg := DefaultConfig()
	root := &placement{
		Element:    &Element{Kind: KindHBox, ID: "root", Title: "AAAA"},
		LabelStrip: rect{X: 0, Y: 10, W: 100, H: cfg.LineHeight + 2*cfg.GroupLabelPaddingY},
	}
	routes := [][]point{{{X: 20, Y: 0}, {X: 20, Y: 100}}}
	labels := placeGroupLabels(root, routes, cfg)
	if len(labels) != 1 {
		t.Fatalf("labels = %#v, want one", labels)
	}
	if labels[0].rect.X <= 20 {
		t.Fatalf("automatic label x = %g, want it moved right of crossing at 20", labels[0].rect.X)
	}
	if score := labelCrossingScore(labels[0].rect, routes, 1); score != 0 {
		t.Fatalf("automatic label crossing score = %d, want zero", score)
	}

	root.Element.ContainerAttributes.LabelAlign = LabelAlignCenter
	labels = placeGroupLabels(root, routes, cfg)
	wantCenter := (root.LabelStrip.W - labels[0].rect.W) / 2
	if labels[0].rect.X != wantCenter {
		t.Fatalf("centered label x = %g, want %g", labels[0].rect.X, wantCenter)
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

func TestGalleryAvoidsNodeInteriors(t *testing.T) {
	filenames, err := filepath.Glob("examples/gallery/*.boxz")
	if err != nil {
		t.Fatal(err)
	}
	if len(filenames) == 0 {
		t.Fatal("visual gallery contains no boxz examples")
	}
	for _, filename := range filenames {
		t.Run(filename, func(t *testing.T) {
			source, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := ParseString(filename, string(source))
			if err != nil {
				t.Fatal(err)
			}
			l, routes, ports, err := solve(doc, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			assertSolvedRouteMetadata(t, routes)
			assertNoNodeRectOverlaps(t, l)
			assertRoutesAvoidOtherNodes(t, l, routes, ports, DefaultConfig())
			assertRoutesMeetPortsCleanly(t, l, routes, ports, DefaultConfig())
			assertNoDisplayRouteOverlaps(t, routes, ports, DefaultConfig(), 1)
		})
	}
}

func assertSolvedRouteMetadata(t *testing.T, routes *routeResult) {
	t.Helper()
	for _, route := range routes.Edges {
		segments := len(route.Points) - 1
		if len(route.Resources) != segments || len(route.Domains) != segments {
			t.Fatalf("route %s -> %s has %d segments, %d resources, and %d domains",
				route.From, route.To, segments, len(route.Resources), len(route.Domains))
		}
		if len(route.Display) < 2 {
			t.Fatalf("route %s -> %s has no materialized display path", route.From, route.To)
		}
		for index, domainID := range route.Domains {
			domain, connector := routes.Domains[domainID]
			if !connector {
				continue
			}
			a, b := route.Points[index], route.Points[index+1]
			if domain.Horizontal != (a.Y == b.Y) {
				t.Fatalf("route %s -> %s uses connector %q in the wrong orientation: %v -> %v",
					route.From, route.To, domainID, a, b)
			}
		}
	}
}

func TestMultipleEdgesAllocateChannelLanesAndPorts(t *testing.T) {
	source := `
hbox root {
  node a
  node b
  node c
  node d
}
edges {
  a -> d
  b -> c
  b -> c
}
`
	doc, err := ParseString("lanes.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.SeamTrackCount[seamID("root", 1)]; got != 2 {
		t.Fatalf("seam lane count = %d, want 2", got)
	}
	_, _, ports, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var bPorts []point
	for key, p := range ports {
		if key.node == "b" {
			bPorts = append(bPorts, p)
		}
	}
	if len(bPorts) != 2 || bPorts[0] == bPorts[1] {
		t.Fatalf("ports for b = %#v, want two distinct ports", bPorts)
	}
}

func TestSeamRoutesUseFinalPortsWithoutBorderSlides(t *testing.T) {
	tests := map[string]string{
		"horizontal fan-in": `
hbox root {
  vbox left { node x "X" node a "A" node y "Y" }
  node b "B"
}
edges { a -> b x -> b y -> b }
`,
		"vertical fan-in": `
vbox root {
  hbox top { node x "X" node a "A" node y "Y" }
  node b "B"
}
edges { a -> b x -> b y -> b }
`,
		"reversed nested seam": `
hbox root {
  vbox left { node x "X" node a "A" node y "Y" }
  node b "B"
}
edges { b -> a b -> x b -> y }
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseString("min-border-slide.boxz", source)
			if err != nil {
				t.Fatal(err)
			}
			l, routes, ports, err := solve(doc, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range routes.Edges {
				fromPort := ports[portKey{node: route.From, side: route.FromSide, edge: route.EdgeIndex}]
				toPort := ports[portKey{node: route.To, side: route.ToSide, edge: route.EdgeIndex, to: true}]
				if route.Points[0] != fromPort || route.Points[len(route.Points)-1] != toPort {
					t.Fatalf("raw seam route %s -> %s endpoints = %v/%v, want ports %v/%v",
						route.From, route.To, route.Points[0], route.Points[len(route.Points)-1], fromPort, toPort)
				}
			}
			assertRoutesMeetPortsCleanly(t, l, routes, ports, DefaultConfig())
			assertNoDisplayRouteOverlaps(t, routes, ports, DefaultConfig(), 1)

			var output bytes.Buffer
			if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			assertNoCollinearEdgeOverlaps(t, output.String())
		})
	}
}

func TestHierarchyConnectorAllocatesTracks(t *testing.T) {
	doc, err := ParseString("hierarchy-risers.boxz", `
hbox root {
  node n1 "xxxxx"
  node n2 "xxxxx"
  node n3 "xxxxx"
  hbox c4 { node n5 "xxxxxxxx" node n6 "xxxxxxxxxx" }
}
edges { n5 -> n3 n2 -> n1 n5 -> n2 n3 -> n6 n3 -> n6 }
`)
	if err != nil {
		t.Fatal(err)
	}
	l, routes, ports, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	var sharedConnector string
	for domain, spec := range routes.Domains {
		if spec.Kind == connectorHierarchy && len(routes.ConnectorUses[domain]) > 1 {
			sharedConnector = domain
			break
		}
	}
	if sharedConnector == "" {
		t.Fatalf("connector uses = %#v, want a shared hierarchy domain", routes.ConnectorUses)
	}
	coordinates := make(map[float64]bool)
	for _, edgeIndex := range routes.ConnectorUses[sharedConnector] {
		route := routes.Edges[edgeIndex]
		for segmentIndex, domain := range route.Domains {
			if domain != sharedConnector {
				continue
			}
			a, b := route.Points[segmentIndex], route.Points[segmentIndex+1]
			if a.Y == b.Y {
				coordinates[a.Y] = true
			} else {
				coordinates[a.X] = true
			}
			break
		}
	}
	if len(coordinates) != len(routes.ConnectorUses[sharedConnector]) {
		t.Fatalf("shared connector %q coordinates = %v, want %d distinct tracks", sharedConnector, coordinates, len(routes.ConnectorUses[sharedConnector]))
	}
	assertRoutesMeetPortsCleanly(t, l, routes, ports, DefaultConfig())
	assertNoDisplayRouteOverlaps(t, routes, ports, DefaultConfig(), 1)
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

func TestDirectSeamAndOuterRouteShareConnectorAllocation(t *testing.T) {
	doc, err := ParseString("seam-connector.boxz", `
hbox root {
  vbox c1 {
    vbox c2 {
      spring
      hbox c3 {
        node n4 "xxxxxxxxxxxxxx"
        node n5 "xxxxxxxxxxxxx"
        node n6 "xxxxxxxxxxxxx"
        node n7 "xxxxxxx"
      }
    }
    node n8 "xxxxxxxxx"
  }
  node n9 "xx"
}
edges {
  n7 -> n5
  n4 -> n5
  n7 -> n8
  n7 -> n5
  n8 -> n9
  n7 -> n5
  n4 -> n7
  n7 -> n9
  n8 -> n5
  n9 -> n4
}
`)
	if err != nil {
		t.Fatal(err)
	}
	l, routes, ports, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	domain := riserID("c2", East, 0)
	if got := routes.ConnectorUses[domain]; len(got) != 2 {
		t.Fatalf("domain %q uses = %v, want seam and outer route", domain, got)
	}
	coordinates := make(map[float64]bool)
	for _, edgeIndex := range routes.ConnectorUses[domain] {
		route := routes.Edges[edgeIndex]
		for segmentIndex, candidate := range route.Domains {
			if candidate == domain {
				a, b := route.Points[segmentIndex], route.Points[segmentIndex+1]
				if a.Y == b.Y {
					coordinates[a.Y] = true
				} else {
					coordinates[a.X] = true
				}
				break
			}
		}
	}
	if len(coordinates) != 2 {
		t.Fatalf("domain %q coordinates = %v, want two allocated tracks", domain, coordinates)
	}
	assertRoutesMeetPortsCleanly(t, l, routes, ports, DefaultConfig())
	assertNoDisplayRouteOverlaps(t, routes, ports, DefaultConfig(), 1)
}

func TestRecursiveFrontiers(t *testing.T) {
	doc, err := ParseString("frontiers.boxz", `
hbox root {
  vbox column {
    node a
    node b
  }
  node c
}
`)
	if err != nil {
		t.Fatal(err)
	}
	a := doc.Root.Children[0].Children[0]
	b := doc.Root.Children[0].Children[1]
	c := doc.Root.Children[1]
	checks := []struct {
		node *Element
		side Side
		want bool
	}{
		{a, North, true}, {c, North, true}, {b, North, false},
		{b, South, true}, {c, South, true}, {a, South, false},
		{a, West, true}, {b, West, true}, {c, West, false},
		{c, East, true}, {a, East, false}, {b, East, false},
	}
	for _, check := range checks {
		if got := onFrontier(doc.Root, check.node, check.side); got != check.want {
			t.Errorf("onFrontier(root, %s, %s) = %v, want %v", check.node.ID, check.side, got, check.want)
		}
	}
}

func TestNestedFrontiersRouteThroughParentSeam(t *testing.T) {
	doc, err := ParseString("nested-seam.boxz", `
vbox root {
  hbox upper {
    vbox leftColumn {
      node a
      node b
    }
    node c
  }
  hbox lower {
    node d
    vbox rightColumn {
      node e
      node f
    }
  }
}
edges { b -> e }
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	seam := plan.Seams[0]
	if seam == nil || seam.ParentID != "root" || seam.FromSide != South || seam.ToSide != North {
		t.Fatalf("b -> e seam = %#v, want nested root S/N seam", seam)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
}

func TestParseRejectsDuplicateID(t *testing.T) {
	_, err := ParseString("duplicate.boxz", `hbox root { node same node same }`)
	if err == nil || !strings.Contains(err.Error(), "duplicate element ID") {
		t.Fatalf("error = %v, want duplicate ID diagnostic", err)
	}
}

func TestParseRejectsNodeBodyAndEmptyContainer(t *testing.T) {
	for name, source := range map[string]string{
		"node body":       `node root {}`,
		"empty container": `vbox root {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseString("invalid.boxz", source); err == nil {
				t.Fatal("ParseString succeeded, want a validation error")
			}
		})
	}
}

func TestParseRejectsImpossibleSide(t *testing.T) {
	_, err := ParseString("side.boxz", `hbox root { node a node b node c }
edges { a:E -> c }`)
	if err == nil || !strings.Contains(err.Error(), "side E is not available") {
		t.Fatalf("error = %v, want unavailable side diagnostic", err)
	}
}

func TestParseRejectsLegacyEdgeKeyword(t *testing.T) {
	_, err := ParseString("legacy.boxz", `hbox root { node a node b }
edge a -> b`)
	if err == nil {
		t.Fatal("legacy edge declaration succeeded, want an edges block")
	}
}

func TestRenderSVGIsDeterministicAndOrthogonal(t *testing.T) {
	doc, err := ParseString("test.boxz", exampleSource)
	if err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	if err := RenderSVG(&first, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if err := RenderSVG(&second, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("rendering is not deterministic")
	}
	if !strings.Contains(first.String(), ">database</text>") {
		t.Fatal("rendered SVG does not contain fallback title")
	}
	if got := strings.Count(first.String(), `class="boxz-edge"`); got != 2 {
		t.Fatalf("edge count = %d, want 2", got)
	}
	if strings.Contains(first.String(), "boxz-debug-") || strings.Contains(first.String(), `class="boxz-container"`) {
		t.Fatal("normal rendering contains debug geometry")
	}

	debugConfig := DefaultConfig()
	debugConfig.Debug = true
	var debug bytes.Buffer
	if err := RenderSVG(&debug, doc, debugConfig); err != nil {
		t.Fatal(err)
	}
	normalPaths := pathPattern.FindAllStringSubmatch(first.String(), -1)
	debugPaths := pathPattern.FindAllStringSubmatch(debug.String(), -1)
	if len(normalPaths) != len(debugPaths) {
		t.Fatalf("debug edge count = %d, want %d", len(debugPaths), len(normalPaths))
	}
	for index := range normalPaths {
		if normalPaths[index][1] != debugPaths[index][1] {
			t.Fatalf("debug changed edge %d from %q to %q", index, normalPaths[index][1], debugPaths[index][1])
		}
	}
	if got := strings.Count(debug.String(), `class="boxz-container"`); got != 2 {
		t.Fatalf("debug container count = %d, want 2", got)
	}
	for _, marker := range []string{
		`data-container="root" data-kind="vbox"`,
		`boxz-debug-channel`,
		`boxz-debug-riser`,
		`boxz-debug-port`,
	} {
		if !strings.Contains(debug.String(), marker) {
			t.Fatalf("debug SVG does not contain %q", marker)
		}
	}
	assertOrthogonalPaths(t, first.String())
}

func TestRenderTitledContainerBoundaryAndTruncatedLabel(t *testing.T) {
	doc, err := ParseString("group.boxz", `vbox root "This title is much too long" [labelAlign = right] { node a "A" node b "B" }
edges { a -> b }`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	svg := output.String()
	for _, marker := range []string{
		`class="boxz-group-boundary" data-container="root"`,
		`class="boxz-group-label" data-container="root"`,
		`<title>This title is much too long</title>`,
		`<text class="boxz-group-title"`,
		`…</text>`,
	} {
		if !strings.Contains(svg, marker) {
			t.Fatalf("rendered SVG does not contain %q", marker)
		}
	}
	if strings.Index(svg, `<g class="boxz-edges">`) > strings.Index(svg, `<g class="boxz-group-labels">`) {
		t.Fatal("group label was not painted after edges")
	}
}

func TestContainerLabelAlignmentDoesNotChangeRoutes(t *testing.T) {
	render := func(alignment string) string {
		t.Helper()
		source := `hbox root "Group" [labelAlign = ` + alignment + `] { node a node b node c }
edges { a:N -> c:N b -> c }`
		doc, err := ParseString("alignment.boxz", source)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	leftPaths := pathPattern.FindAllStringSubmatch(render("left"), -1)
	rightPaths := pathPattern.FindAllStringSubmatch(render("right"), -1)
	if len(leftPaths) != len(rightPaths) {
		t.Fatalf("left paths = %d, right paths = %d", len(leftPaths), len(rightPaths))
	}
	for index := range leftPaths {
		if leftPaths[index][1] != rightPaths[index][1] {
			t.Fatalf("label alignment changed route %d from %q to %q", index, leftPaths[index][1], rightPaths[index][1])
		}
	}
}

func TestTransparentContainerSeamProvidesThroughRoute(t *testing.T) {
	doc, err := ParseString("three-layers.boxz", `
vbox root {
  hbox top { node a1 node a2 }
  hbox middle "Layout annotation" [bounded = false] { node b1 node b2 }
  hbox bottom { node c1 node c2 }
}
edges { a1 -> c2 }
`)
	if err != nil {
		t.Fatal(err)
	}
	_, routes, _, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	want := seamChannelID(seamID("middle", 0))
	if !containsString(routes.Edges[0].Resources, want) {
		t.Fatalf("route resources = %v, want transparent middle seam %q", routes.Edges[0].Resources, want)
	}
	if containsString(routes.Edges[0].Resources, channelID("root", West)) ||
		containsString(routes.Edges[0].Resources, channelID("root", East)) {
		t.Fatalf("route resources = %v, want passage through transparent middle row", routes.Edges[0].Resources)
	}
}

func TestTransparentSingleChildContainerProvidesGutterRoute(t *testing.T) {
	doc, err := ParseString("three-layers-single.boxz", `
vbox root {
  hbox top { node a1 node a2 }
  hbox middle { node b1 }
  hbox bottom { node c1 node c2 }
}
edges { a1 -> c2 }
`)
	if err != nil {
		t.Fatal(err)
	}
	_, routes, _, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	resources := routes.Edges[0].Resources
	usesGutter := containsString(resources, gutterChannelID("middle", West)) ||
		containsString(resources, gutterChannelID("middle", East))
	if !usesGutter {
		t.Fatalf("route resources = %v, want a transparent middle gutter", resources)
	}
	if containsString(resources, channelID("root", West)) ||
		containsString(resources, channelID("root", East)) {
		t.Fatalf("route resources = %v, want passage through transparent middle wrapper", resources)
	}
}

func TestSeamChannelLanesDoNotOverlapDirectSeamTracks(t *testing.T) {
	doc, err := ParseString("mixed-seam-use.boxz", `
vbox root {
  hbox top { node a1 node a2 }
  hbox middle [bounded] { node b1 node b2 }
  hbox bottom { node c1 node c2 }
}
edges {
  a2 -> b1
  a1 -> c2
}
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
	assertNoCollinearEdgeOverlaps(t, output.String())
}

func TestBoundedContainerRejectsForeignTransit(t *testing.T) {
	doc, err := ParseString("three-layers-bounded.boxz", `
vbox root {
  hbox top { node a1 node a2 }
  hbox middle [bounded] { node b1 node b2 }
  hbox bottom { node c1 node c2 }
}
edges { a1 -> c2 }
`)
	if err != nil {
		t.Fatal(err)
	}
	_, routes, _, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range routes.Edges[0].Resources {
		if strings.HasPrefix(resource, "middle:") {
			t.Fatalf("foreign route used bounded middle resource %q: %v", resource, routes.Edges[0].Resources)
		}
	}
	want := seamChannelID(seamID("root", 0))
	if !containsString(routes.Edges[0].Resources, want) {
		t.Fatalf("route resources = %v, want parent-owned seam channel %q", routes.Edges[0].Resources, want)
	}

	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `class="boxz-group-boundary" data-container="middle"`) {
		t.Fatal("bounded untitled container was not drawn")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRenderNestedSameKindContainers(t *testing.T) {
	source := `
hbox outer {
  hbox left {
    node a
    node b
  }
  hbox right {
    node c
    node d
  }
}
edges {
  a -> d
  b -> c
}
`
	doc, err := ParseString("nested.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
}

func TestLaneOffsetsDoNotCreateRiserJogs(t *testing.T) {
	source := `
vbox root {
  hbox services {
    node client "Client"
    node api "API Server"
  }
  hbox infra {
    node database
    node cacheServer
  }
  hbox export {
    node exportService
  }
}
edges {
  client -> api
  api -> database
  api -> exportService
}
`
	doc, err := ParseString("double.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	if seam := plan.Seams[0]; seam == nil || seam.ParentID != "services" || seam.FromSide != East || seam.ToSide != West {
		t.Fatalf("client -> api seam = %#v, want services E/W seam", seam)
	}
	if seam := plan.Seams[1]; seam == nil || seam.ParentID != "root" || seam.FromSide != South || seam.ToSide != North {
		t.Fatalf("api -> database seam = %#v, want root S/N seam", seam)
	}
	if seam := plan.Seams[2]; seam != nil {
		t.Fatalf("api -> exportService seam = %#v, want outer-channel route", seam)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())

	for _, path := range pathPattern.FindAllStringSubmatch(output.String(), -1) {
		coordinates := coordinatePattern.FindAllStringSubmatch(path[1], -1)
		for index := 1; index < len(coordinates)-1; index++ {
			previousX, _ := strconv.ParseFloat(coordinates[index-1][1], 64)
			previousY, _ := strconv.ParseFloat(coordinates[index-1][2], 64)
			currentX, _ := strconv.ParseFloat(coordinates[index][1], 64)
			currentY, _ := strconv.ParseFloat(coordinates[index][2], 64)
			length := abs(previousX-currentX) + abs(previousY-currentY)
			if length < DefaultConfig().LaneSpacing {
				t.Fatalf("lane offset created a short interior jog in path %q", path[1])
			}
		}
	}
}

func TestSeamTrackOrderAvoidsAlignedAccessOverlap(t *testing.T) {
	doc, err := ParseString("aligned.boxz", `
vbox root {
  hbox services {
    node client "A"
    node api "A"
  }
  hbox infra {
    node database "A"
    node cacheServer "A"
  }
  hbox export {
    node exportService "A"
  }
}
edges {
  client -> api
  api -> database
  api -> exportService
  client -> cacheServer
}
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
	assertNoCollinearEdgeOverlaps(t, output.String())
}

func TestCyclicSeamConstraintsUseDoglegs(t *testing.T) {
	tests := map[string]string{
		"vertical forward": `
vbox root {
  hbox upper {
    node a "A"
    node b "A"
  }
  hbox lower {
    node c "A"
    node d "A"
  }
}
edges {
  a -> d
  b -> c
}
`,
		"horizontal reverse": `
hbox root {
  vbox left {
    node a "A"
    node b "A"
  }
  vbox right {
    node c "A"
    node d "A"
  }
}
edges {
  d -> a
  c -> b
}
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseString("cyclic.boxz", source)
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
			routes, err := routeDocument(doc, l, plan, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			grew, err := assignSeamTracks(doc, l, plan, allocatePorts(l, routes), DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			if !grew {
				t.Fatal("cyclic seam did not request additional dogleg tracks")
			}
			for edgeIndex := range doc.Edges {
				spec := plan.Seams[edgeIndex]
				if spec.TrackCount != 4 || spec.FirstTrack == spec.SecondTrack {
					t.Fatalf("edge %d seam = %#v, want two-bank dogleg in four tracks", edgeIndex, spec)
				}
			}

			var output bytes.Buffer
			if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			assertOrthogonalPaths(t, output.String())
			assertNoCollinearEdgeOverlaps(t, output.String())
		})
	}
}

func TestAcyclicSeamKeepsRetainedTrackCapacity(t *testing.T) {
	doc, err := ParseString("retained-seam-capacity.boxz", `
hbox root {
  node a "A"
  node b "B"
}
edges { a -> b }
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	const retained = 3
	plan.SeamTrackCount[seamID("root", 0)] = retained
	l, err := buildLayout(doc, DefaultConfig(), nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routeDocument(doc, l, plan, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	grew, err := assignSeamTracks(doc, l, plan, allocatePorts(l, routes), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if grew {
		t.Fatal("acyclic seam unexpectedly requested more capacity")
	}
	spec := plan.Seams[0]
	if spec.TrackCount != retained || spec.FirstTrack != 1 || spec.SecondTrack != 1 {
		t.Fatalf("seam = %#v, want centered track 1 in retained bank of 3", spec)
	}
}

func TestOuterRoutesCrossAdjacentSiblingChannels(t *testing.T) {
	tests := map[string]string{
		"horizontal sibling gap": `
hbox root {
  vbox center {
    hbox upper {
      node u1
      node u2
    }
    hbox lower {
      node l1
      node l2
    }
  }
  vbox right {
    node e1
    node e2
  }
}
edges {
  u1 -> e2
}
`,
		"vertical sibling gap": `
vbox root {
  hbox upper {
    vbox column {
      node u1
      node u2
    }
    node ur
  }
  hbox lower {
    vbox column2 {
      node d1
      node d2
    }
    node dr
  }
}
edges {
  u1 -> d1
}
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseString("crossbar.boxz", source)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := buildRoutingPlan(doc)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Seams[0] != nil {
				t.Fatalf("route unexpectedly classified as a direct seam: %#v", plan.Seams[0])
			}
			_, routes, _, err := solve(doc, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			usesCrossbar := false
			for resource, edgeIndexes := range routes.ResourceUses {
				usedByRoute := false
				for _, edgeIndex := range edgeIndexes {
					usedByRoute = usedByRoute || edgeIndex == routes.Edges[0].EdgeIndex
				}
				if !usedByRoute {
					continue
				}
				if strings.Contains(resource, ":crossbar:") {
					usesCrossbar = true
				}
				if resource == channelID("root", North) || resource == channelID("root", South) ||
					resource == channelID("root", East) || resource == channelID("root", West) {
					t.Fatalf("route used root outer channel %q instead of a local crossbar", resource)
				}
			}
			if !usesCrossbar {
				t.Fatalf("route resources = %#v, want a sibling crossbar", routes.ResourceUses)
			}

			debugConfig := DefaultConfig()
			debugConfig.Debug = true
			var first, second bytes.Buffer
			if err := RenderSVG(&first, doc, debugConfig); err != nil {
				t.Fatal(err)
			}
			if err := RenderSVG(&second, doc, debugConfig); err != nil {
				t.Fatal(err)
			}
			if first.String() != second.String() {
				t.Fatal("crossbar routing is not deterministic")
			}
			if !strings.Contains(first.String(), "boxz-debug-crossbar") {
				t.Fatal("debug SVG does not contain sibling crossbars")
			}
			assertOrthogonalPaths(t, first.String())
		})
	}
}

func TestSiblingCrossbarAllocatesParallelLanes(t *testing.T) {
	doc, err := ParseString("crossbar-lanes.boxz", `
hbox root {
  vbox center {
    hbox upper {
      node u1
      node u2
    }
    node lower
  }
  vbox right {
    node e1
    node e2
  }
}
edges {
  u1 -> e2
  u1 -> e2
}
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
	assertNoCollinearEdgeOverlaps(t, output.String())
}

func TestCrossbarAvoidsDirectSeamAccessLeg(t *testing.T) {
	tests := map[string]string{
		"vertical crossbar": `
hbox root {
  vbox leftExt { node w1 node w2 node w3 node w4 }
  vbox center {
    hbox upperService { node u1 node u2 node u3 }
    hbox lowerService { node l1 node l2 node l3 }
  }
  vbox rightExt { node e1 node e2 }
}
edges {
  u1 -> e2
  l1 -> u3
}
		`,
		"horizontal crossbar": `
vbox root {
  hbox upperExt { node w1 node w2 node w3 node w4 }
  hbox center {
    vbox leftService { node u1 node u2 node u3 }
    vbox rightService { node l1 node l2 node l3 }
  }
  hbox lowerExt { node e1 node e2 }
}
edges {
  u1 -> e2
  l1 -> u3
}
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseString("arrangement.boxz", source)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			assertOrthogonalPaths(t, output.String())
			assertNoCollinearEdgeOverlaps(t, output.String())
		})
	}
}

func TestInterstitialSeamTurnKeepsEndpointLegPerpendicular(t *testing.T) {
	doc, err := ParseString("arrangement.boxz", `
hbox root {
  vbox leftExt { node w1 node w2 node w3 node w4 }
  vbox center {
    hbox upperService { node u1 node u2 node u3 }
    hbox lowerService { node l1 node l2 node l3 }
  }
  vbox rightExt { node e1 node e2 }
}
edges {
  u1 -> e2
  l1 -> u3
  l1 -> l2
  l1:N -> l3:N
  e1 -> w1
  e1 -> w2
  e1 -> w3
}
`)
	if err != nil {
		t.Fatal(err)
	}
	_, routes, ports, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var points []point
	for _, route := range routes.Edges {
		if route.From == "e1" && route.To == "w3" {
			points = displayRoute(route, routes, ports, DefaultConfig())
			break
		}
	}
	if len(points) < 2 {
		t.Fatal("e1 -> w3 route is missing")
	}
	if points[0].Y != points[1].Y || points[1].X >= points[0].X {
		t.Fatalf("route does not leave e1 westward and perpendicular to its port: %v", points)
	}

	var output bytes.Buffer
	if err := RenderSVG(&output, doc, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
	assertNoCollinearEdgeOverlaps(t, output.String())
}

var pathPattern = regexp.MustCompile(`<path class="boxz-edge"[^>]* d="([^"]+)"`)
var coordinatePattern = regexp.MustCompile(`(?:M|L) ([0-9.]+) ([0-9.]+)`)

func assertOrthogonalPaths(t *testing.T, svg string) {
	t.Helper()
	paths := pathPattern.FindAllStringSubmatch(svg, -1)
	if len(paths) == 0 {
		t.Fatal("no edge paths found")
	}
	for _, path := range paths {
		coordinates := coordinatePattern.FindAllStringSubmatch(path[1], -1)
		for index := 1; index < len(coordinates); index++ {
			previousX, _ := strconv.ParseFloat(coordinates[index-1][1], 64)
			previousY, _ := strconv.ParseFloat(coordinates[index-1][2], 64)
			currentX, _ := strconv.ParseFloat(coordinates[index][1], 64)
			currentY, _ := strconv.ParseFloat(coordinates[index][2], 64)
			if previousX != currentX && previousY != currentY {
				t.Fatalf("non-orthogonal segment in path %q: (%g,%g) to (%g,%g)", path[1], previousX, previousY, currentX, currentY)
			}
		}
	}
}

func assertNoCollinearEdgeOverlaps(t *testing.T, svg string) {
	t.Helper()
	paths := pathPattern.FindAllStringSubmatch(svg, -1)
	for left := range paths {
		leftPoints := coordinatePattern.FindAllStringSubmatch(paths[left][1], -1)
		for right := left + 1; right < len(paths); right++ {
			rightPoints := coordinatePattern.FindAllStringSubmatch(paths[right][1], -1)
			for leftSegment := 1; leftSegment < len(leftPoints); leftSegment++ {
				lx1, _ := strconv.ParseFloat(leftPoints[leftSegment-1][1], 64)
				ly1, _ := strconv.ParseFloat(leftPoints[leftSegment-1][2], 64)
				lx2, _ := strconv.ParseFloat(leftPoints[leftSegment][1], 64)
				ly2, _ := strconv.ParseFloat(leftPoints[leftSegment][2], 64)
				for rightSegment := 1; rightSegment < len(rightPoints); rightSegment++ {
					rx1, _ := strconv.ParseFloat(rightPoints[rightSegment-1][1], 64)
					ry1, _ := strconv.ParseFloat(rightPoints[rightSegment-1][2], 64)
					rx2, _ := strconv.ParseFloat(rightPoints[rightSegment][1], 64)
					ry2, _ := strconv.ParseFloat(rightPoints[rightSegment][2], 64)
					verticalOverlap := lx1 == lx2 && rx1 == rx2 && lx1 == rx1 &&
						intervalOverlap(ly1, ly2, ry1, ry2) > 1e-9
					horizontalOverlap := ly1 == ly2 && ry1 == ry2 && ly1 == ry1 &&
						intervalOverlap(lx1, lx2, rx1, rx2) > 1e-9
					if verticalOverlap || horizontalOverlap {
						t.Fatalf("collinear overlap between paths %q and %q", paths[left][1], paths[right][1])
					}
				}
			}
		}
	}
}

func assertNoNodeRectOverlaps(t *testing.T, l *layout) {
	t.Helper()
	var nodes []*placement
	collectNodePlacements(l.Root, &nodes)
	for left := range nodes {
		for right := left + 1; right < len(nodes); right++ {
			if rectInteriorsOverlap(nodes[left].Rect, nodes[right].Rect) {
				t.Fatalf("nodes %q and %q overlap: %#v and %#v",
					nodes[left].Element.ID, nodes[right].Element.ID, nodes[left].Rect, nodes[right].Rect)
			}
		}
	}
}

func assertRoutesAvoidOtherNodes(t *testing.T, l *layout, routes *routeResult, ports map[portKey]point, cfg Config) {
	t.Helper()
	var nodes []*placement
	collectNodePlacements(l.Root, &nodes)
	for _, route := range routes.Edges {
		points := displayRoute(route, routes, ports, cfg)
		for index := 1; index < len(points); index++ {
			for _, node := range nodes {
				if node.Element.ID == route.From || node.Element.ID == route.To {
					continue
				}
				if segmentEntersRectInterior(points[index-1], points[index], node.Rect) {
					t.Fatalf("route %s -> %s segment %#v -> %#v enters node %q at %#v",
						route.From, route.To, points[index-1], points[index], node.Element.ID, node.Rect)
				}
			}
		}
	}
}

func assertRoutesMeetPortsCleanly(t *testing.T, l *layout, routes *routeResult, ports map[portKey]point, cfg Config) {
	t.Helper()
	for _, route := range routes.Edges {
		points := displayRoute(route, routes, ports, cfg)
		if len(points) < 2 {
			t.Fatalf("route %s -> %s has fewer than two display points: %v", route.From, route.To, points)
		}
		fromPort := ports[portKey{node: route.From, side: route.FromSide, edge: route.EdgeIndex}]
		toPort := ports[portKey{node: route.To, side: route.ToSide, edge: route.EdgeIndex, to: true}]
		if points[0] != fromPort || points[len(points)-1] != toPort {
			t.Fatalf("display route %s -> %s endpoints = %v/%v, want ports %v/%v",
				route.From, route.To, points[0], points[len(points)-1], fromPort, toPort)
		}
		if !endpointSegmentIsPerpendicular(fromPort, points[1], l.ByID[route.From].Rect, route.FromSide) {
			t.Fatalf("route %s -> %s leaves side %s along the node border: %v",
				route.From, route.To, route.FromSide, points[:2])
		}
		last := len(points) - 1
		if !endpointSegmentIsPerpendicular(toPort, points[last-1], l.ByID[route.To].Rect, route.ToSide) {
			t.Fatalf("route %s -> %s enters side %s along the node border: %v",
				route.From, route.To, route.ToSide, points[last-1:])
		}
	}
}

func assertNoDisplayRouteOverlaps(t *testing.T, routes *routeResult, ports map[portKey]point, cfg Config, tolerance float64) {
	t.Helper()
	display := make([][]point, len(routes.Edges))
	for index, route := range routes.Edges {
		display[index] = displayRoute(route, routes, ports, cfg)
	}
	for left := range display {
		for right := left + 1; right < len(display); right++ {
			for leftIndex := 1; leftIndex < len(display[left]); leftIndex++ {
				la, lb := display[left][leftIndex-1], display[left][leftIndex]
				for rightIndex := 1; rightIndex < len(display[right]); rightIndex++ {
					ra, rb := display[right][rightIndex-1], display[right][rightIndex]
					vertical := sameCoordinate(la.X, lb.X) && sameCoordinate(ra.X, rb.X) && sameCoordinate(la.X, ra.X) &&
						intervalOverlap(la.Y, lb.Y, ra.Y, rb.Y) > tolerance
					horizontal := sameCoordinate(la.Y, lb.Y) && sameCoordinate(ra.Y, rb.Y) && sameCoordinate(la.Y, ra.Y) &&
						intervalOverlap(la.X, lb.X, ra.X, rb.X) > tolerance
					if vertical || horizontal {
						t.Fatalf("display routes %d and %d overlap: %v and %v", left, right, display[left], display[right])
					}
				}
			}
		}
	}
}

func endpointSegmentIsPerpendicular(port, outside point, node rect, side Side) bool {
	switch side {
	case North:
		return sameCoordinate(port.Y, node.Y) && sameCoordinate(port.X, outside.X) && outside.Y < port.Y
	case East:
		return sameCoordinate(port.X, node.X+node.W) && sameCoordinate(port.Y, outside.Y) && outside.X > port.X
	case South:
		return sameCoordinate(port.Y, node.Y+node.H) && sameCoordinate(port.X, outside.X) && outside.Y > port.Y
	case West:
		return sameCoordinate(port.X, node.X) && sameCoordinate(port.Y, outside.Y) && outside.X < port.X
	default:
		return false
	}
}

func collectNodePlacements(p *placement, nodes *[]*placement) {
	if p.Element.Kind == KindNode {
		*nodes = append(*nodes, p)
		return
	}
	for _, child := range p.Children {
		collectNodePlacements(child, nodes)
	}
}

func rectInteriorsOverlap(left, right rect) bool {
	return intervalOverlap(left.X, left.X+left.W, right.X, right.X+right.W) > 1e-9 &&
		intervalOverlap(left.Y, left.Y+left.H, right.Y, right.Y+right.H) > 1e-9
}

func segmentEntersRectInterior(a, b point, r rect) bool {
	if a.X == b.X {
		return a.X > r.X+1e-9 && a.X < r.X+r.W-1e-9 &&
			intervalOverlap(a.Y, b.Y, r.Y, r.Y+r.H) > 1e-9
	}
	if a.Y == b.Y {
		return a.Y > r.Y+1e-9 && a.Y < r.Y+r.H-1e-9 &&
			intervalOverlap(a.X, b.X, r.X, r.X+r.W) > 1e-9
	}
	return true
}

func intervalOverlap(a1, a2, b1, b2 float64) float64 {
	if a1 > a2 {
		a1, a2 = a2, a1
	}
	if b1 > b2 {
		b1, b2 = b2, b1
	}
	left, right := a1, a2
	if b1 > left {
		left = b1
	}
	if b2 < right {
		right = b2
	}
	return right - left
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

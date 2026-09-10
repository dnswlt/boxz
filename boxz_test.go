package boxz

import (
	"bytes"
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
	l, routes, err := solve(doc, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ports := allocatePorts(l, routes)
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
	if got := strings.Count(first.String(), `class="boxz-container"`); got != 2 {
		t.Fatalf("container count = %d, want 2", got)
	}
	if !strings.Contains(first.String(), `data-container="root" data-kind="vbox"`) {
		t.Fatal("rendered SVG does not identify the root container")
	}
	assertOrthogonalPaths(t, first.String())
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
			_, routes, err := solve(doc, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			usesCrossbar := false
			for resource, edgeIndexes := range routes.ChannelUses {
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
				t.Fatalf("route resources = %#v, want a sibling crossbar", routes.ChannelUses)
			}

			var first, second bytes.Buffer
			if err := RenderSVG(&first, doc, DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			if err := RenderSVG(&second, doc, DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			if first.String() != second.String() {
				t.Fatal("crossbar routing is not deterministic")
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

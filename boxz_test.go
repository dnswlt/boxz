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
edge client -> api
edge api -> database
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
}

func TestParseEndpointSides(t *testing.T) {
	source := `// constrained ports
hbox root { node a node b }
edge a:N -> b:S`
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

func TestMultipleEdgesAllocateChannelLanesAndPorts(t *testing.T) {
	source := `
hbox root {
  node a
  node b
  node c
  node d
}
edge a -> d
edge b -> c
edge b -> c
`
	doc, err := ParseString("lanes.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildRoutingPlan(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.SeamLaneCount[seamID("root", 1)]; got != 2 {
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
edge b -> e
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
edge a:E -> c`)
	if err == nil || !strings.Contains(err.Error(), "side E is not available") {
		t.Fatalf("error = %v, want unavailable side diagnostic", err)
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
edge a -> d
edge b -> c
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
edge client -> api
edge api -> database
edge api -> exportService
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

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

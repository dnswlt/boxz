package boxz

import (
	"bytes"
	"strings"
	"testing"
)

func TestAutomaticGroupLabelPlacementAvoidsRoutes(t *testing.T) {
	cfg := defaultConfig()
	root := &placement{
		element:    &element{kind: kindHBox, ID: "root", Title: "AAAA"},
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

	root.element.containerAttributes.LabelAlign = labelAlignCenter
	labels = placeGroupLabels(root, routes, cfg)
	wantCenter := (root.LabelStrip.W - labels[0].rect.W) / 2
	if labels[0].rect.X != wantCenter {
		t.Fatalf("centered label x = %g, want %g", labels[0].rect.X, wantCenter)
	}
}

func TestRenderTitledContainerBoundaryAndTruncatedLabel(t *testing.T) {
	doc, err := ParseString("group.boxz", `vbox root "This title is much too long" [labelAlign = right] { node a "A" node b "B" }
edges { a -> b }`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
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
		if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
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

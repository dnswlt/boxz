package boxz

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

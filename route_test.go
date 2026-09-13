package boxz

import (
	"bytes"
	"strings"
	"testing"
)

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

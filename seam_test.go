package boxz

import (
	"bytes"
	"testing"
)

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
	l, routes, ports, err := solve(doc, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	var sharedConnector string
	for domain, spec := range routes.Domains {
		if spec.kind == connectorHierarchy && len(routes.ConnectorUses[domain]) > 1 {
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
	assertRoutesMeetPortsCleanly(t, l, routes, ports, defaultConfig())
	assertNoDisplayRouteOverlaps(t, routes, ports, defaultConfig(), 1)
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
	l, routes, ports, err := solve(doc, defaultConfig())
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
	assertRoutesMeetPortsCleanly(t, l, routes, ports, defaultConfig())
	assertNoDisplayRouteOverlaps(t, routes, ports, defaultConfig(), 1)
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
	if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
	assertNoCollinearEdgeOverlaps(t, output.String())
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
	if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
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
			l, err := buildLayout(doc, defaultConfig(), nil, plan)
			if err != nil {
				t.Fatal(err)
			}
			routes, err := routeDocument(doc, l, plan, defaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			grew, err := assignSeamTracks(doc, l, plan, allocatePorts(l, routes), defaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			if !grew {
				t.Fatal("cyclic seam did not request additional dogleg tracks")
			}
			for edgeIndex := range doc.edges {
				spec := plan.Seams[edgeIndex]
				if spec.TrackCount != 4 || spec.FirstTrack == spec.SecondTrack {
					t.Fatalf("edge %d seam = %#v, want two-bank dogleg in four tracks", edgeIndex, spec)
				}
			}

			var output bytes.Buffer
			if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
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
	l, err := buildLayout(doc, defaultConfig(), nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routeDocument(doc, l, plan, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	grew, err := assignSeamTracks(doc, l, plan, allocatePorts(l, routes), defaultConfig())
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
	if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
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
			if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
				t.Fatal(err)
			}
			assertOrthogonalPaths(t, output.String())
			assertNoCollinearEdgeOverlaps(t, output.String())
		})
	}
}

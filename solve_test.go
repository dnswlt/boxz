package boxz

import (
	"bytes"
	"strconv"
	"testing"
)

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

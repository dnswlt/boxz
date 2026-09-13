package boxz

import (
	"testing"
)

func TestRouteRefinementRemovesSafeLaneShoulder(t *testing.T) {
	from := &placement{Element: &Element{Kind: KindNode, ID: "from"}, Rect: rect{X: 140, Y: 40, W: 40, H: 20}}
	to := &placement{Element: &Element{Kind: KindNode, ID: "to"}, Rect: rect{X: 80, Y: 120, W: 40, H: 20}}
	l := &layout{
		Root: &placement{Element: &Element{Kind: KindVBox, ID: "root"}, Children: []*placement{from, to}},
		ByID: map[string]*placement{"from": from, "to": to},
	}
	routes := &routeResult{Edges: []*routedEdge{
		{Display: []point{{X: 100, Y: 0}, {X: 100, Y: 70}}},
		{
			From: "from", To: "to", FromSide: South, ToSide: North,
			Display: []point{
				{X: 160, Y: 60}, {X: 160, Y: 80}, {X: 92, Y: 80},
				{X: 92, Y: 100}, {X: 100, Y: 100}, {X: 100, Y: 120},
			},
		},
	}}

	refineDisplayRoutes(l, routes, DefaultConfig())
	want := []point{{X: 160, Y: 60}, {X: 160, Y: 100}, {X: 100, Y: 100}, {X: 100, Y: 120}}
	if got := routes.Edges[1].Display; !samePolyline(got, want) {
		t.Fatalf("refined route = %v, want %v", got, want)
	}
	refineDisplayRoutes(l, routes, DefaultConfig())
	if got := routes.Edges[1].Display; !samePolyline(got, want) {
		t.Fatalf("second refinement changed route to %v, want idempotent %v", got, want)
	}
}

func TestRouteRefinementTreatsTouchingRunsAsConflict(t *testing.T) {
	upperA, upperB := point{X: 100, Y: 20}, point{X: 100, Y: 80}
	lowerA, lowerB := point{X: 100, Y: 80}, point{X: 100, Y: 140}
	if !displaySegmentsConflict(upperA, upperB, lowerA, lowerB, 4, true) {
		t.Fatal("collinear runs that meet at one endpoint must conflict")
	}

	horizontalA, horizontalB := point{X: 80, Y: 50}, point{X: 120, Y: 50}
	if displaySegmentsConflict(upperA, upperB, horizontalA, horizontalB, 4, true) {
		t.Fatal("proper perpendicular crossing should remain legal")
	}

	nearA, nearB := point{X: 20, Y: 100}, point{X: 140, Y: 100}
	tooCloseA, tooCloseB := point{X: 40, Y: 104}, point{X: 120, Y: 104}
	if !displaySegmentsConflict(nearA, nearB, tooCloseA, tooCloseB, 8, true) {
		t.Fatal("overlapping parallel runs closer than one lane must conflict")
	}
	spacedA, spacedB := point{X: 40, Y: 108}, point{X: 120, Y: 108}
	if displaySegmentsConflict(nearA, nearB, spacedA, spacedB, 8, true) {
		t.Fatal("parallel runs one lane apart should remain legal")
	}
}

func TestRouteRefinementPreservesBoundedRegionCrossings(t *testing.T) {
	boundary := rect{X: 20, Y: 20, W: 60, H: 60}
	original := []point{{X: 0, Y: 50}, {X: 40, Y: 50}}
	movedPortal := []point{
		{X: 0, Y: 50}, {X: 10, Y: 50}, {X: 10, Y: 30},
		{X: 40, Y: 30}, {X: 40, Y: 50},
	}
	if sameBoundaryCrossings(original, movedPortal, boundary) {
		t.Fatal("moving a bounded-region crossing must not be a legal refinement")
	}
	if !sameBoundaryCrossings(original, original, boundary) {
		t.Fatal("an unchanged bounded-region crossing should remain legal")
	}
}

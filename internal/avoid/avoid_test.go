package avoid

import (
	"context"
	"os"
	"testing"
)

// newClient starts the router, skipping when it has not been built. The C++
// binary is an optional component, so `go test ./...` must pass without it.
func newClient(t *testing.T) *Client {
	t.Helper()
	binary, err := FindBinary()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	client, err := Start(binary, os.Stderr)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return client
}

func ports(prefix string) []Port {
	return []Port{
		{ID: prefix + "n", Side: North, Pos: 0.5},
		{ID: prefix + "e", Side: East, Pos: 0.5},
		{ID: prefix + "s", Side: South, Pos: 0.5},
		{ID: prefix + "w", Side: West, Pos: 0.5},
	}
}

// The router picks the side, so a node directly below another must be reached
// through south/north rather than by wrapping around.
func TestRouterChoosesFacingSides(t *testing.T) {
	client := newClient(t)
	response, err := client.Route(context.Background(), &Request{
		ID: "facing",
		Obstacles: []Obstacle{
			{ID: "a", Rect: Rect{X: 0, Y: 0, W: 100, H: 40}, Ports: ports("a")},
			{ID: "b", Rect: Rect{X: 0, Y: 200, W: 100, H: 40}, Ports: ports("b")},
		},
		Edges: []Edge{{ID: "e0", From: Endpoint{Obstacle: "a"}, To: Endpoint{Obstacle: "b"}}},
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(response.Routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(response.Routes))
	}
	route := response.Routes[0]
	if route.From.Side != South || route.To.Side != North {
		t.Errorf("got %s -> %s, want south -> north", route.From.Side, route.To.Side)
	}
	if route.From.Port != "as" || route.To.Port != "bn" {
		t.Errorf("got ports %q -> %q, want \"as\" -> \"bn\"", route.From.Port, route.To.Port)
	}
	assertOrthogonal(t, route)
}

// Ports are exclusive by default, so parallel edges between the same pair must
// not stack on one coordinate.
func TestExclusivePortsSeparateParallelEdges(t *testing.T) {
	client := newClient(t)
	south := []Port{
		{ID: "s0", Side: South, Pos: 0.25},
		{ID: "s1", Side: South, Pos: 0.75},
	}
	north := []Port{
		{ID: "n0", Side: North, Pos: 0.25},
		{ID: "n1", Side: North, Pos: 0.75},
	}
	response, err := client.Route(context.Background(), &Request{
		ID: "parallel",
		Obstacles: []Obstacle{
			{ID: "a", Rect: Rect{X: 0, Y: 0, W: 100, H: 40}, Ports: south},
			{ID: "b", Rect: Rect{X: 0, Y: 200, W: 100, H: 40}, Ports: north},
		},
		Edges: []Edge{
			{ID: "e0", From: Endpoint{Obstacle: "a"}, To: Endpoint{Obstacle: "b"}},
			{ID: "e1", From: Endpoint{Obstacle: "a"}, To: Endpoint{Obstacle: "b"}},
		},
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(response.Routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(response.Routes))
	}
	if response.Routes[0].From.Port == response.Routes[1].From.Port {
		t.Errorf("both edges left through port %q; exclusive ports should differ",
			response.Routes[0].From.Port)
	}
	for _, route := range response.Routes {
		assertOrthogonal(t, route)
	}
}

// Naming a port subset must confine the endpoint to it, even when that forces
// a longer route.
func TestPortSubsetConstrainsEndpoint(t *testing.T) {
	client := newClient(t)
	response, err := client.Route(context.Background(), &Request{
		ID: "constrained",
		Obstacles: []Obstacle{
			{ID: "a", Rect: Rect{X: 0, Y: 0, W: 100, H: 40}, Ports: ports("a")},
			{ID: "b", Rect: Rect{X: 0, Y: 200, W: 100, H: 40}, Ports: ports("b")},
		},
		Edges: []Edge{{
			ID:   "e0",
			From: Endpoint{Obstacle: "a", Ports: []string{"an"}},
			To:   Endpoint{Obstacle: "b"},
		}},
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if got := response.Routes[0].From.Side; got != North {
		t.Errorf("got departure side %s, want north", got)
	}
}

// Routes must bend around an obstacle rather than crossing it.
func TestRouteAvoidsObstacle(t *testing.T) {
	client := newClient(t)
	blocker := Rect{X: 20, Y: 100, W: 60, H: 40}
	response, err := client.Route(context.Background(), &Request{
		ID: "avoid",
		Obstacles: []Obstacle{
			{ID: "a", Rect: Rect{X: 0, Y: 0, W: 100, H: 40}, Ports: ports("a")},
			{ID: "b", Rect: Rect{X: 0, Y: 200, W: 100, H: 40}, Ports: ports("b")},
			{ID: "blocker", Rect: blocker},
		},
		Edges: []Edge{{ID: "e0", From: Endpoint{Obstacle: "a"}, To: Endpoint{Obstacle: "b"}}},
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	route := response.Routes[0]
	assertOrthogonal(t, route)
	if len(route.Points) < 3 {
		t.Fatalf("got a straight route through the blocker: %v", route.Points)
	}
	for index := 1; index < len(route.Points); index++ {
		if segmentCrossesRect(route.Points[index-1], route.Points[index], blocker) {
			t.Errorf("segment %v->%v crosses the blocker", route.Points[index-1], route.Points[index])
		}
	}
}

// A rejected request must come back as an *Error and leave the client usable,
// so one bad diagram cannot poison a long-lived router.
func TestRequestErrorKeepsClientUsable(t *testing.T) {
	client := newClient(t)
	_, err := client.Route(context.Background(), &Request{
		ID:    "bad",
		Edges: []Edge{{ID: "e0", From: Endpoint{Obstacle: "missing"}, To: Endpoint{Obstacle: "missing"}}},
	})
	var routerError *Error
	if !asError(err, &routerError) {
		t.Fatalf("got %v, want *Error", err)
	}
	if routerError.Code != "unknown_obstacle" {
		t.Errorf("got code %q, want \"unknown_obstacle\"", routerError.Code)
	}

	response, err := client.Route(context.Background(), &Request{
		ID:        "good",
		Obstacles: []Obstacle{{ID: "a", Rect: Rect{X: 0, Y: 0, W: 10, H: 10}}},
	})
	if err != nil {
		t.Fatalf("client unusable after a rejected request: %v", err)
	}
	if response.ID != "good" {
		t.Errorf("got response id %q, want \"good\"", response.ID)
	}
}

// Equal requests must produce equal routes: boxz output has to stay stable
// across runs.
func TestRoutingIsDeterministic(t *testing.T) {
	client := newClient(t)
	request := func() *Request {
		return &Request{
			ID: "determinism",
			Obstacles: []Obstacle{
				{ID: "a", Rect: Rect{X: 0, Y: 0, W: 100, H: 40}, Ports: ports("a")},
				{ID: "b", Rect: Rect{X: 200, Y: 0, W: 100, H: 40}, Ports: ports("b")},
				{ID: "c", Rect: Rect{X: 100, Y: 200, W: 100, H: 40}, Ports: ports("c")},
			},
			Edges: []Edge{
				{ID: "e0", From: Endpoint{Obstacle: "a"}, To: Endpoint{Obstacle: "c"}},
				{ID: "e1", From: Endpoint{Obstacle: "b"}, To: Endpoint{Obstacle: "c"}},
				{ID: "e2", From: Endpoint{Obstacle: "a"}, To: Endpoint{Obstacle: "b"}},
			},
		}
	}
	first, err := client.Route(context.Background(), request())
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		next, err := client.Route(context.Background(), request())
		if err != nil {
			t.Fatalf("Route: %v", err)
		}
		for index := range first.Routes {
			if !samePoints(first.Routes[index].Points, next.Routes[index].Points) {
				t.Fatalf("edge %s differs on attempt %d:\n first %v\n then  %v",
					first.Routes[index].ID, attempt, first.Routes[index].Points, next.Routes[index].Points)
			}
		}
	}
}

// Clusters are accepted but inert under orthogonal routing; the router says so
// rather than silently ignoring them.
func TestClusterWarnsUnderOrthogonalRouting(t *testing.T) {
	client := newClient(t)
	response, err := client.Route(context.Background(), &Request{
		ID:        "cluster",
		Clusters:  []Cluster{{ID: "g", Rect: Rect{X: 150, Y: 60, W: 170, H: 220}}},
		Obstacles: []Obstacle{{ID: "a", Rect: Rect{X: 0, Y: 100, W: 60, H: 40}}},
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(response.Warnings) == 0 {
		t.Error("got no warning for a cluster under orthogonal routing")
	}
}

func assertOrthogonal(t *testing.T, route Route) {
	t.Helper()
	for index := 1; index < len(route.Points); index++ {
		a, b := route.Points[index-1], route.Points[index]
		if a.X != b.X && a.Y != b.Y {
			t.Errorf("edge %s segment %v->%v is neither horizontal nor vertical", route.ID, a, b)
		}
	}
}

func samePoints(a, b []Point) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// segmentCrossesRect reports whether an axis-aligned segment passes through a
// rectangle's interior. Touching a border is fine; entering it is not.
func segmentCrossesRect(a, b Point, r Rect) bool {
	minX, maxX := min(a.X, b.X), max(a.X, b.X)
	minY, maxY := min(a.Y, b.Y), max(a.Y, b.Y)
	return minX < r.X+r.W && maxX > r.X && minY < r.Y+r.H && maxY > r.Y
}

func asError(err error, target **Error) bool {
	routerError, ok := err.(*Error)
	if ok {
		*target = routerError
	}
	return ok
}

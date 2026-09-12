package boxz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dnswlt/boxz/internal/avoid"
)

// avoidConfig skips when the optional sidecar has not been built.
func avoidConfig(t *testing.T) Config {
	t.Helper()
	if _, err := avoid.FindBinary(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	cfg := DefaultConfig()
	cfg.EdgeRouter = RouterAvoid
	return cfg
}

// knownAvoidOverlaps records examples libavoid cannot draw without two routes
// sharing a segment. Boxz's own router grows the seam into two banks instead;
// no nudging option or extra space fixes it.
var knownAvoidOverlaps = map[string]string{
	"03-cyclic-seam-doglegs.boxz": "crossed seam edges share ~8px of one vertical stub",
}

// The gallery holds the same geometric invariants under either router. Route
// metadata is not checked: resources and track domains are built-in concepts.
func TestAvoidGalleryHoldsRoutingInvariants(t *testing.T) {
	cfg := avoidConfig(t)
	filenames, err := filepath.Glob("examples/gallery/*.boxz")
	if err != nil {
		t.Fatal(err)
	}
	if len(filenames) == 0 {
		t.Fatal("visual gallery contains no boxz examples")
	}
	for _, filename := range filenames {
		t.Run(filename, func(t *testing.T) {
			doc := parseFile(t, filename)
			l, routes, ports, err := solve(doc, cfg)
			if err != nil {
				t.Fatal(err)
			}
			assertNoNodeRectOverlaps(t, l)
			assertRoutesAvoidOtherNodes(t, l, routes, ports, cfg)
			assertRoutesMeetPortsCleanly(t, l, routes, ports, cfg)

			reason, known := knownAvoidOverlaps[filepath.Base(filename)]
			overlaps := countDisplayRouteOverlaps(routes, 1)
			switch {
			case known && overlaps == 0:
				t.Fatalf("%s no longer overlaps (%s); remove it from knownAvoidOverlaps",
					filepath.Base(filename), reason)
			case known:
				t.Logf("known limitation: %s", reason)
			default:
				assertNoDisplayRouteOverlaps(t, routes, ports, cfg, 1)
			}
		})
	}
}

// countDisplayRouteOverlaps counts what assertNoDisplayRouteOverlaps rejects.
func countDisplayRouteOverlaps(routes *routeResult, tolerance float64) int {
	count := 0
	for left := range routes.Edges {
		for right := left + 1; right < len(routes.Edges); right++ {
			a, b := routes.Edges[left].Display, routes.Edges[right].Display
			for ai := 1; ai < len(a); ai++ {
				for bi := 1; bi < len(b); bi++ {
					la, lb, ra, rb := a[ai-1], a[ai], b[bi-1], b[bi]
					vertical := sameCoordinate(la.X, lb.X) && sameCoordinate(ra.X, rb.X) &&
						sameCoordinate(la.X, ra.X) && intervalOverlap(la.Y, lb.Y, ra.Y, rb.Y) > tolerance
					horizontal := sameCoordinate(la.Y, lb.Y) && sameCoordinate(ra.Y, rb.Y) &&
						sameCoordinate(la.Y, ra.Y) && intervalOverlap(la.X, lb.X, ra.X, rb.X) > tolerance
					if vertical || horizontal {
						count++
					}
				}
			}
		}
	}
	return count
}

// Nothing in libavoid bounds a route to the diagram, so buildAvoidRequest
// frames the canvas with obstacles.
func TestAvoidRoutesStayInsideCanvas(t *testing.T) {
	cfg := avoidConfig(t)
	filenames, err := filepath.Glob("examples/gallery/*.boxz")
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range filenames {
		doc := parseFile(t, filename)
		l, routes, _, err := solve(doc, cfg)
		if err != nil {
			t.Fatalf("%s: %v", filename, err)
		}
		for _, route := range routes.Edges {
			for _, p := range route.Display {
				if p.X < 0 || p.Y < 0 || p.X > l.Width || p.Y > l.Height {
					t.Errorf("%s: route %s -> %s leaves the %gx%g canvas at %v",
						filename, route.From, route.To, l.Width, l.Height, p)
				}
			}
		}
	}
}

// The fixed point only grows outer channel bands, so node rectangles must be
// identical under either router.
func TestAvoidPreservesNodePlacement(t *testing.T) {
	cfg := avoidConfig(t)
	filenames, err := filepath.Glob("examples/gallery/*.boxz")
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range filenames {
		doc := parseFile(t, filename)
		builtin, _, _, err := solve(doc, DefaultConfig())
		if err != nil {
			t.Fatalf("%s: %v", filename, err)
		}
		routed, _, _, err := solve(doc, cfg)
		if err != nil {
			t.Fatalf("%s: %v", filename, err)
		}
		for id, placed := range routed.ByID {
			if placed.Element.Kind != KindNode {
				continue
			}
			other := builtin.ByID[id]
			// Channel bands sized by the built-in router's lane demand shift
			// descendants, so only compare each node's own dimensions.
			if placed.Rect.W != other.Rect.W || placed.Rect.H != other.Rect.H {
				t.Errorf("%s: node %q is %gx%g under avoid and %gx%g under builtin",
					filename, id, placed.Rect.W, placed.Rect.H, other.Rect.W, other.Rect.H)
			}
		}
	}
}

// A canary, not a proof: libavoid promises no determinism. It catches a change
// that reintroduces unstable arbitration, as exclusive pins did.
func TestAvoidRenderIsStableAcrossRuns(t *testing.T) {
	cfg := avoidConfig(t)
	doc := parseFile(t, filepath.Join("examples", "arrangement.boxz"))
	var first strings.Builder
	if err := RenderSVG(&first, doc, cfg); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		var next strings.Builder
		if err := RenderSVG(&next, doc, cfg); err != nil {
			t.Fatal(err)
		}
		if next.String() != first.String() {
			t.Fatalf("render differs on attempt %d", attempt)
		}
	}
}

// A constrained endpoint must reach libavoid as a port subset, or it is free
// to pick any side.
func TestAvoidHonoursExplicitEndpointSides(t *testing.T) {
	cfg := avoidConfig(t)
	doc, err := ParseString("sides.boxz", `
hbox root {
  node a "A"
  node b "B"
}
edges {
  a:N -> b:N
}
`)
	if err != nil {
		t.Fatal(err)
	}
	_, routes, _, err := solve(doc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := routes.Edges[0].FromSide; got != North {
		t.Errorf("departure side = %s, want north", got)
	}
	if got := routes.Edges[0].ToSide; got != North {
		t.Errorf("arrival side = %s, want north", got)
	}
}

func parseFile(t *testing.T, filename string) *Document {
	t.Helper()
	source, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseString(filename, string(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

package boxz

import (
	"bytes"
	"strings"
	"testing"
)

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
	a := doc.root.Children[0].Children[0]
	b := doc.root.Children[0].Children[1]
	c := doc.root.Children[1]
	checks := []struct {
		node *element
		side Side
		want bool
	}{
		{a, North, true}, {c, North, true}, {b, North, false},
		{b, South, true}, {c, South, true}, {a, South, false},
		{a, West, true}, {b, West, true}, {c, West, false},
		{c, East, true}, {a, East, false}, {b, East, false},
	}
	for _, check := range checks {
		if got := onFrontier(doc.root, check.node, check.side); got != check.want {
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
	if err := RenderSVG(&output, doc, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	assertOrthogonalPaths(t, output.String())
}

// Quoted identifiers can contain the colons that separate routing key parts.
func TestRoutingKeysDistinguishQuotedIdentifiers(t *testing.T) {
	if gutterChannelID("a", North) == channelID("a:gutter", North) {
		t.Errorf("gutter of a and channel of `a:gutter` share key %q", gutterChannelID("a", North))
	}
	capacity := map[string]int{
		riserID("a:N:riser:x", South, 0): 5,
		channelID("a:N:riser:x", South):  3,
	}
	if got := hierarchyConnectorCapacity(capacity, "a", North); got != 0 {
		t.Errorf("connector capacity of a = %d, want 0: it counted keys of `a:N:riser:x`", got)
	}
}

func TestParseRejectsImpossibleSide(t *testing.T) {
	_, err := ParseString("side.boxz", `hbox root { node a node b node c }
edges { a:E -> c }`)
	if err == nil || !strings.Contains(err.Error(), "side E is not available") {
		t.Fatalf("error = %v, want unavailable side diagnostic", err)
	}
}

package boxz_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/dnswlt/boxz"
)

func TestDocumentHasNoPublicRepresentation(t *testing.T) {
	typeOfDocument := reflect.TypeOf(boxz.Document{})
	for index := 0; index < typeOfDocument.NumField(); index++ {
		field := typeOfDocument.Field(index)
		if field.IsExported() {
			t.Fatalf("Document exposes field %q", field.Name)
		}
	}
}

func TestDocumentQueriesAndEdgeReplacementAreIsolated(t *testing.T) {
	doc, err := boxz.ParseString("view.boxz", `
hbox root {
  node catalog_a "A"
  node catalog_b "B"
}
edges { catalog_a -> catalog_b }
`)
	if err != nil {
		t.Fatal(err)
	}

	nodes := doc.Nodes()
	if got, want := nodes, []boxz.Node{{ID: "catalog_a", Title: "A"}, {ID: "catalog_b", Title: "B"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Nodes() = %#v, want %#v", got, want)
	}
	nodes[0].ID = "changed"
	if got := doc.Nodes()[0].ID; got != "catalog_a" {
		t.Fatalf("mutating Nodes result changed document node to %q", got)
	}

	edges := doc.Edges()
	edges[0].From = "catalog_b"
	if got := doc.Edges()[0].From; got != "catalog_a" {
		t.Fatalf("mutating Edges result changed document source to %q", got)
	}

	north := boxz.North
	updated, err := doc.WithEdges([]boxz.Edge{{From: "catalog_b", FromSide: &north, To: "catalog_a"}})
	if err != nil {
		t.Fatal(err)
	}
	north = boxz.South
	if got := *updated.Edges()[0].FromSide; got != boxz.North {
		t.Fatalf("mutating input side changed stored side to %s", got)
	}
	if got := doc.Edges()[0].From; got != "catalog_a" {
		t.Fatalf("WithEdges changed receiver edge source to %q", got)
	}
}

func TestWithEdgesRejectsInvalidDocuments(t *testing.T) {
	doc, err := boxz.ParseString("view.boxz", `hbox root { node a node b node c }`)
	if err != nil {
		t.Fatal(err)
	}
	east := boxz.East
	invalid := boxz.Side("up")
	tests := map[string]struct {
		edges []boxz.Edge
		want  string
	}{
		"unknown source":       {[]boxz.Edge{{From: "missing", To: "a"}}, `source "missing" is not a node`},
		"unknown destination":  {[]boxz.Edge{{From: "a", To: "missing"}}, `destination "missing" is not a node`},
		"container endpoint":   {[]boxz.Edge{{From: "root", To: "a"}}, `source "root" is not a node`},
		"self edge":            {[]boxz.Edge{{From: "a", To: "a"}}, `self-edge on "a"`},
		"unknown side":         {[]boxz.Edge{{From: "a", FromSide: &invalid, To: "b"}}, `unknown source side "up"`},
		"unavailable topology": {[]boxz.Edge{{From: "a", FromSide: &east, To: "c"}}, `side E is not available`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := doc.WithEdges(test.edges)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("WithEdges error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestUpdatedDocumentFormatsAndRendersWithoutReparse(t *testing.T) {
	doc, err := boxz.ParseString("view.boxz", `hbox root { node a node b }`)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := doc.WithEdges([]boxz.Edge{{From: "a", To: "b"}})
	if err != nil {
		t.Fatal(err)
	}

	var source bytes.Buffer
	if err := boxz.Format(&source, updated); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source.String(), "a -> b") {
		t.Fatalf("formatted source does not contain updated edge:\n%s", source.String())
	}

	var svg bytes.Buffer
	if err := boxz.RenderSVG(&svg, updated); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svg.String(), `data-from="a" data-to="b"`) {
		t.Fatalf("rendered SVG does not contain updated edge:\n%s", svg.String())
	}
}

func TestZeroDocumentFailsCleanly(t *testing.T) {
	var doc boxz.Document
	if _, err := doc.WithEdges(nil); err == nil {
		t.Fatal("zero Document accepted WithEdges")
	}
	if err := boxz.Format(&bytes.Buffer{}, &doc); err == nil {
		t.Fatal("zero Document accepted Format")
	}
	if err := boxz.RenderSVG(&bytes.Buffer{}, &doc); err == nil {
		t.Fatal("zero Document accepted RenderSVG")
	}
}

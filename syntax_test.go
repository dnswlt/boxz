package boxz

import (
	"strings"
	"testing"
)

func TestParsePreservesOrderAndDefaultsTitle(t *testing.T) {
	doc, err := ParseString("test.boxz", exampleSource)
	if err != nil {
		t.Fatal(err)
	}
	if doc.root.ID != "root" || doc.root.kind != kindVBox {
		t.Fatalf("unexpected root: %#v", doc.root)
	}
	if got := doc.root.Children[0].ID; got != "services" {
		t.Fatalf("first child = %q, want services", got)
	}
	if got := doc.root.Children[0].Children[1].ID; got != "api" {
		t.Fatalf("second hbox child = %q, want api", got)
	}
	if got := doc.root.Children[1].Title; got != "database" {
		t.Fatalf("default title = %q, want database", got)
	}
	if len(doc.edges) != 2 || doc.edges[0].From != "client" || doc.edges[1].From != "api" {
		t.Fatalf("edges = %#v, want source order preserved", doc.edges)
	}
}

func TestParseEndpointSides(t *testing.T) {
	source := `// constrained ports
hbox root { node a node b }
edges { a:N -> b:S }`
	doc, err := ParseString("sides.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	if doc.edges[0].FromSide == nil || *doc.edges[0].FromSide != North {
		t.Fatalf("source side = %#v, want N", doc.edges[0].FromSide)
	}
	if doc.edges[0].ToSide == nil || *doc.edges[0].ToSide != South {
		t.Fatalf("target side = %#v, want S", doc.edges[0].ToSide)
	}
}

func TestParseAllowsEmptyEdgesBlock(t *testing.T) {
	doc, err := ParseString("empty-edges.boxz", `node root
edges {}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.edges) != 0 {
		t.Fatalf("edges = %#v, want none", doc.edges)
	}
}

func TestParseSpringsAndNodeAttributes(t *testing.T) {
	doc, err := ParseString("springs.boxz", `
hbox root {
  spring
  spring
  node a "A" [spring,]
  spring
  node b [spring = false]
  spring
  spring
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc.root.Springs, []int{2, 1, 2}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("springs = %v, want %v", got, want)
	}
	if !doc.root.Children[0].nodeAttributes.Spring {
		t.Fatal("bare spring attribute did not enable node growth")
	}
	if doc.root.Children[1].nodeAttributes.Spring {
		t.Fatal("spring = false enabled node growth")
	}
}

func TestParseContainerTitlesAndLabelAlignment(t *testing.T) {
	doc, err := ParseString("groups.boxz", `
vbox root "System" [labelAlign = center] {
  hbox services "Services" [labelAlign = RIGHT] {
    node api
  }
  hbox automatic "Automatic" [labelAlign = auto, bounded = false] { node worker }
  hbox boundary [bounded] { node database }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if doc.root.Title != "System" || doc.root.containerAttributes.LabelAlign != labelAlignCenter {
		t.Fatalf("root title/attributes = %q/%q, want System/center", doc.root.Title, doc.root.containerAttributes.LabelAlign)
	}
	if !doc.root.containerAttributes.Bounded {
		t.Fatal("titled root did not default to bounded")
	}
	services := doc.root.Children[0]
	if services.Title != "Services" || services.containerAttributes.LabelAlign != labelAlignRight {
		t.Fatalf("services title/attributes = %q/%q, want Services/right", services.Title, services.containerAttributes.LabelAlign)
	}
	if got := doc.root.Children[1].containerAttributes.LabelAlign; got != labelAlignAuto {
		t.Fatalf("explicit automatic alignment = %q, want auto", got)
	}
	if doc.root.Children[1].containerAttributes.Bounded {
		t.Fatal("bounded = false did not override the titled default")
	}
	if !doc.root.Children[2].containerAttributes.Bounded {
		t.Fatal("bare bounded attribute did not bound an untitled container")
	}
}

func TestAttributeGrammarAcceptsScalarValuesAndTrailingComma(t *testing.T) {
	parsed, err := documentParser.ParseString("attributes.boxz", `node root [text = "foo", integer = 17, decimal = -2.5, enabled = true,]`)
	if err != nil {
		t.Fatal(err)
	}
	attributes := parsed.Root.Attributes.Items
	if len(attributes) != 4 || *attributes[0].Value.String != "foo" || attributes[1].Value.Number.Value != "17" ||
		!attributes[2].Value.Number.Negative || attributes[2].Value.Number.Value != "2.5" || *attributes[3].Value.Ident != "true" {
		t.Fatalf("attributes = %#v, want string, integer, decimal, and identifier values", attributes)
	}
}

func TestParseRejectsInvalidAttributesAndEmptySpringContainer(t *testing.T) {
	tests := map[string]struct {
		source string
		want   string
	}{
		"duplicate":          {`hbox root { node a [spring, spring] }`, "duplicate attribute"},
		"unknown":            {`hbox root { node a [sprung] }`, "attribute \"sprung\" is not supported"},
		"invalid spring":     {`hbox root { node a [spring = 1] }`, "must be a flag or boolean"},
		"container attr":     {`hbox root [spring] { node a }`, "is not supported on hbox"},
		"invalid alignment":  {`hbox root "Root" [labelAlign = north] { node a }`, "must be auto, left, center, or right"},
		"bare alignment":     {`hbox root "Root" [labelAlign] { node a }`, "must be auto, left, center, or right"},
		"invalid bounded":    {`hbox root [bounded = 1] { node a }`, "must be a flag or boolean"},
		"align no title":     {`hbox root [labelAlign = left] { node a }`, "has labelAlign but no title"},
		"old attribute name": {`hbox root "Root" [labelAnchor = left] { node a }`, "attribute \"labelAnchor\" is not supported"},
		"only springs":       {`hbox root { spring spring }`, "must contain at least one child"},
		"spring root node":   {`node root [spring]`, "root node \"root\" cannot be spring-enabled"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseString("attributes.boxz", test.source)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want diagnostic containing %q", err, test.want)
			}
		})
	}
}

func TestParseRejectsDuplicateID(t *testing.T) {
	_, err := ParseString("duplicate.boxz", `hbox root { node same node same }`)
	if err == nil || !strings.Contains(err.Error(), "duplicate element ID") {
		t.Fatalf("error = %v, want duplicate ID diagnostic", err)
	}
}

func TestParseRejectsNodeBodyAndEmptyContainer(t *testing.T) {
	for name, source := range map[string]string{
		"node body":       `node root {}`,
		"empty container": `vbox root {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseString("invalid.boxz", source); err == nil {
				t.Fatal("ParseString succeeded, want a validation error")
			}
		})
	}
}

func TestParseQuotedIdentifiers(t *testing.T) {
	source := "vbox `system:core` {\n" +
		"  node `api-server`\n" +
		"  node `C:\\tmp` \"Temp\"\n" +
		"  node db\n" +
		"  node `stra\u00dfe`\n" +
		"}\n" +
		"edges {\n" +
		"  `api-server`:S -> `C:\\tmp`\n" +
		"  `C:\\tmp` -> `db`\n" +
		"}\n"
	doc, err := ParseString("quoted.boxz", source)
	if err != nil {
		t.Fatal(err)
	}
	if doc.root.ID != "system:core" {
		t.Errorf("root ID = %q, want system:core", doc.root.ID)
	}
	api := doc.root.Children[0]
	if api.ID != "api-server" || api.Title != "api-server" {
		t.Errorf("node = %q titled %q, want api-server for both", api.ID, api.Title)
	}
	if got := doc.root.Children[1].ID; got != `C:\tmp` {
		t.Errorf("node ID = %q, want the backslash taken literally", got)
	}
	first, second := doc.edges[0], doc.edges[1]
	if first.From != "api-server" || first.FromSide == nil || *first.FromSide != South || first.To != `C:\tmp` {
		t.Errorf("first edge = %q:%v -> %q", first.From, first.FromSide, first.To)
	}
	if got := doc.root.Children[3].ID; got != "stra\u00dfe" {
		t.Errorf("node ID = %q, want non-ASCII letters accepted", got)
	}
	if second.To != "db" {
		t.Errorf("`db` resolved to %q, want the same node as db", second.To)
	}
}

func TestParseRejectsInvalidQuotedIdentifiers(t *testing.T) {
	for name, tc := range map[string]struct{ source, want string }{
		"empty":      {"hbox root { node `` }", "empty quoted identifier"},
		"line break": {"hbox root { node `a\nb` }", "U+000A"},
		"space":      {"hbox root { node `a b` }", "U+0020"},
		"tab":        {"hbox root { node `a\tb` }", "U+0009"},
		"control":    {"hbox root { node `a\x01b` }", "U+0001"},
		"emoji":      {"hbox root { node `a\U0001F600` }", "U+1F600"},
		"symbol":     {"hbox root { node `a\u2192b` }", "U+2192"},
		"duplicate":  {"hbox root { node a node `a` }", "duplicate element ID"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseString("invalid.boxz", tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsLegacyEdgeKeyword(t *testing.T) {
	_, err := ParseString("legacy.boxz", `hbox root { node a node b }
edge a -> b`)
	if err == nil {
		t.Fatal("legacy edge declaration succeeded, want an edges block")
	}
}

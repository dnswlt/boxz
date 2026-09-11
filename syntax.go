package boxz

import (
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// Kind identifies the role an element plays in the layout tree.
type Kind string

const (
	KindNode Kind = "node"
	KindHBox Kind = "hbox"
	KindVBox Kind = "vbox"
)

// NodeAttributes contains the validated attributes available to visible nodes.
type NodeAttributes struct {
	Spring bool
}

// Side identifies one side of a node or layout box.
type Side string

const (
	North Side = "N"
	East  Side = "E"
	South Side = "S"
	West  Side = "W"
)

// Document is a parsed and validated boxz document.
type Document struct {
	Root  *Element
	Edges []*Edge
}

// Element is either a visible node or an ordered layout container.
type Element struct {
	Kind           Kind
	ID             string
	Title          string
	NodeAttributes NodeAttributes
	Children       []*Element
	// Springs has one entry before each child and one after the last child.
	// Its value is the number of equal-weight springs in that gap; nil means none.
	Springs  []int
	Parent   *Element
	position lexer.Position
}

// Edge connects two nodes. A nil side means that the router may choose it.
type Edge struct {
	From     string
	FromSide *Side
	To       string
	ToSide   *Side
	position lexer.Position
}

type syntaxDocument struct {
	Root      *syntaxElement   `parser:"@@"`
	EdgeBlock *syntaxEdgeBlock `parser:"@@?"`
}

type syntaxElement struct {
	Pos        lexer.Position
	Kind       string            `parser:"@Ident"`
	ID         string            `parser:"@Ident"`
	Title      *string           `parser:"(@String)?"`
	Attributes *syntaxAttributes `parser:"@@?"`
	Body       *syntaxBody       `parser:"@@?"`
}

type syntaxBody struct {
	Items []*syntaxBodyItem `parser:"'{' @@* '}'"`
}

type syntaxBodyItem struct {
	Pos     lexer.Position
	Spring  bool           `parser:"  @'spring'"`
	Element *syntaxElement `parser:"| @@"`
}

type syntaxAttributes struct {
	Items []*syntaxAttribute `parser:"'[' ( @@ ( ',' @@ )* ','? )? ']'"`
}

type syntaxAttribute struct {
	Pos   lexer.Position
	Key   string                `parser:"@Ident"`
	Value *syntaxAttributeValue `parser:"( '=' @@ )?"`
}

type syntaxAttributeValue struct {
	String *string       `parser:"  @String"`
	Number *syntaxNumber `parser:"| @@"`
	Ident  *string       `parser:"| @Ident"`
}

type syntaxNumber struct {
	Negative bool   `parser:"@'-'?"`
	Value    string `parser:"@(Float | Int)"`
}

type syntaxEdgeBlock struct {
	Edges []*syntaxEdge `parser:"'edges' '{' @@* '}'"`
}

type syntaxEdge struct {
	Pos  lexer.Position
	From *syntaxEndpoint `parser:"@@"`
	To   *syntaxEndpoint `parser:"'-' '>' @@"`
}

type syntaxEndpoint struct {
	ID   string  `parser:"@Ident"`
	Side *string `parser:"( ':' @Ident )?"`
}

var documentParser = participle.MustBuild[syntaxDocument](
	participle.UseLookahead(2),
	participle.Unquote("String"),
)

// Parse reads a boxz document. The filename is used in diagnostics only.
func Parse(filename string, r io.Reader) (*Document, error) {
	parsed, err := documentParser.Parse(filename, r)
	if err != nil {
		return nil, err
	}

	doc := &Document{}
	seen := make(map[string]lexer.Position)
	doc.Root, err = convertElement(filename, parsed.Root, nil, seen)
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]*Element)
	collectNodes(doc.Root, nodes)
	var parsedEdges []*syntaxEdge
	if parsed.EdgeBlock != nil {
		parsedEdges = parsed.EdgeBlock.Edges
	}
	for _, raw := range parsedEdges {
		edge, edgeErr := convertEdge(filename, raw, nodes)
		if edgeErr != nil {
			return nil, edgeErr
		}
		doc.Edges = append(doc.Edges, edge)
	}
	// Planning here makes topology and side errors parse-time errors rather than
	// failures that depend on a later choice of rendering dimensions.
	if _, err := buildRoutingPlan(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ParseString parses source from a string.
func ParseString(filename, source string) (*Document, error) {
	return Parse(filename, strings.NewReader(source))
}

func convertElement(filename string, raw *syntaxElement, parent *Element, seen map[string]lexer.Position) (*Element, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s: document must contain a root element", filename)
	}
	if previous, ok := seen[raw.ID]; ok {
		return nil, diagnostic(filename, raw.Pos, "duplicate element ID %q (first declared at %d:%d)", raw.ID, previous.Line, previous.Column)
	}
	seen[raw.ID] = raw.Pos

	element := &Element{
		Kind:     Kind(raw.Kind),
		ID:       raw.ID,
		Parent:   parent,
		position: raw.Pos,
	}
	switch element.Kind {
	case KindNode:
		if raw.Body != nil {
			return nil, diagnostic(filename, raw.Pos, "node %q cannot contain children", raw.ID)
		}
		if raw.Title == nil || *raw.Title == "" {
			element.Title = raw.ID
		} else {
			element.Title = *raw.Title
		}
	case KindHBox, KindVBox:
		if raw.Title != nil {
			return nil, diagnostic(filename, raw.Pos, "%s %q cannot have a title", raw.Kind, raw.ID)
		}
	default:
		return nil, diagnostic(filename, raw.Pos, "unknown element kind %q", raw.Kind)
	}
	if err := convertAttributes(filename, element, raw.Attributes); err != nil {
		return nil, err
	}

	var items []*syntaxBodyItem
	if raw.Body != nil {
		items = raw.Body.Items
	}
	if element.Kind != KindNode {
		element.Springs = []int{0}
	}
	for _, item := range items {
		if item.Spring {
			element.Springs[len(element.Springs)-1]++
			continue
		}
		converted, err := convertElement(filename, item.Element, element, seen)
		if err != nil {
			return nil, err
		}
		element.Children = append(element.Children, converted)
		element.Springs = append(element.Springs, 0)
	}
	if element.Kind != KindNode && len(element.Children) == 0 {
		return nil, diagnostic(filename, raw.Pos, "%s %q must contain at least one child", raw.Kind, raw.ID)
	}
	return element, nil
}

// convertAttributes compiles the generic surface syntax into the typed model.
// Attribute names and scalar representations should not escape this boundary.
func convertAttributes(filename string, element *Element, raw *syntaxAttributes) error {
	if raw == nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, attribute := range raw.Items {
		if seen[attribute.Key] {
			return diagnostic(filename, attribute.Pos, "duplicate attribute %q on %q", attribute.Key, element.ID)
		}
		seen[attribute.Key] = true
		if element.Kind != KindNode || attribute.Key != "spring" {
			return diagnostic(filename, attribute.Pos, "attribute %q is not supported on %s %q", attribute.Key, element.Kind, element.ID)
		}
		enabled, ok := booleanAttribute(attribute.Value)
		if !ok {
			return diagnostic(filename, attribute.Pos, "attribute %q on node %q must be a flag or boolean", attribute.Key, element.ID)
		}
		element.NodeAttributes.Spring = enabled
	}
	if element.Parent == nil && element.NodeAttributes.Spring {
		return diagnostic(filename, element.position, "root node %q cannot be spring-enabled", element.ID)
	}
	return nil
}

func booleanAttribute(value *syntaxAttributeValue) (bool, bool) {
	if value == nil {
		return true, true
	}
	if value.Ident == nil {
		return false, false
	}
	switch *value.Ident {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func convertEdge(filename string, raw *syntaxEdge, nodes map[string]*Element) (*Edge, error) {
	if nodes[raw.From.ID] == nil {
		return nil, diagnostic(filename, raw.Pos, "edge source %q is not a node", raw.From.ID)
	}
	if nodes[raw.To.ID] == nil {
		return nil, diagnostic(filename, raw.Pos, "edge destination %q is not a node", raw.To.ID)
	}
	if raw.From.ID == raw.To.ID {
		return nil, diagnostic(filename, raw.Pos, "self-edge on %q is not supported yet", raw.From.ID)
	}
	fromSide, err := parseSide(filename, raw.Pos, raw.From.Side)
	if err != nil {
		return nil, err
	}
	toSide, err := parseSide(filename, raw.Pos, raw.To.Side)
	if err != nil {
		return nil, err
	}
	edge := &Edge{From: raw.From.ID, FromSide: fromSide, To: raw.To.ID, ToSide: toSide, position: raw.Pos}
	return edge, nil
}

func parseSide(filename string, pos lexer.Position, raw *string) (*Side, error) {
	if raw == nil {
		return nil, nil
	}
	side := Side(strings.ToUpper(*raw))
	switch side {
	case North, East, South, West:
		return &side, nil
	default:
		return nil, diagnostic(filename, pos, "unknown side %q (want N, E, S, or W)", *raw)
	}
}

func allowedSides(node *Element) [2]Side {
	// A node enters the channel system owned by its immediate parent. Hboxes
	// expose their children vertically; vboxes expose them horizontally.
	if node.Parent == nil || node.Parent.Kind == KindHBox {
		return [2]Side{North, South}
	}
	return [2]Side{West, East}
}

func collectNodes(element *Element, nodes map[string]*Element) {
	if element.Kind == KindNode {
		nodes[element.ID] = element
		return
	}
	for _, child := range element.Children {
		collectNodes(child, nodes)
	}
}

func diagnostic(filename string, pos lexer.Position, format string, args ...any) error {
	return fmt.Errorf("%s:%d:%d: %s", filename, pos.Line, pos.Column, fmt.Sprintf(format, args...))
}

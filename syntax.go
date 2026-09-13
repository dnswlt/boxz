package boxz

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// kind identifies the role an element plays in the layout tree.
type kind string

const (
	kindNode kind = "node"
	kindHBox kind = "hbox"
	kindVBox kind = "vbox"
)

// nodeAttributes contains the validated attributes available to visible nodes.
type nodeAttributes struct {
	Spring bool
}

// labelAlignment controls the horizontal placement of a container label.
// The zero value, like labelAlignAuto, selects automatic placement.
type labelAlignment string

const (
	labelAlignAuto   labelAlignment = "auto"
	labelAlignLeft   labelAlignment = "left"
	labelAlignCenter labelAlignment = "center"
	labelAlignRight  labelAlignment = "right"
)

// containerAttributes contains the validated attributes available to layout
// containers. Containers remain structural and cannot be edge endpoints.
type containerAttributes struct {
	LabelAlign labelAlignment
	// Bounded makes the container a visible routing boundary. Titles enable it
	// by default, while an explicit attribute may override that default.
	Bounded bool
}

// Side identifies one side of a node or layout box.
type Side string

const (
	North Side = "N"
	East  Side = "E"
	South Side = "S"
	West  Side = "W"
)

// Document is a parsed or programmatically derived boxz document. Its
// representation is private so a valid document cannot be mutated behind the
// package's back; use Nodes, Edges, and WithEdges to inspect or transform it.
type Document struct {
	root     *element
	edges    []*Edge
	filename string
}

// Node is an immutable snapshot of a visible node in a document.
type Node struct {
	ID    string
	Title string
}

// element is either a visible node or an ordered layout container.
type element struct {
	kind                kind
	ID                  string
	Title               string
	nodeAttributes      nodeAttributes
	containerAttributes containerAttributes
	Children            []*element
	// Springs has one entry before each child and one after the last child.
	// Its value is the number of equal-weight springs in that gap; nil means none.
	Springs  []int
	Parent   *element
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
	ID         string            `parser:"@(Ident | RawString)"`
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
	ID   string  `parser:"@(Ident | RawString)"`
	Side *string `parser:"( ':' @Ident )?"`
}

var documentParser = participle.MustBuild[syntaxDocument](
	participle.UseLookahead(2),
	participle.Unquote("String"),
	participle.Map(unquoteIdentifier, "RawString"),
)

// unquoteIdentifier strips the backticks from a quoted identifier. The content
// is literal, with no escapes.
func unquoteIdentifier(token lexer.Token) (lexer.Token, error) {
	id := token.Value[1 : len(token.Value)-1]
	if id == "" {
		return token, participle.Errorf(token.Pos, "empty quoted identifier")
	}
	for _, r := range id {
		if !quotedIdentifierRune(r) {
			return token, participle.Errorf(token.Pos,
				"quoted identifier %q contains %q (%U); use letters, digits, and ASCII punctuation", id, r, r)
		}
	}
	token.Value = id
	return token, nil
}

// quotedIdentifierRune accepts the letters and digits of a plain identifier
// plus printable ASCII punctuation. Whitespace, control characters, and other
// symbols are rejected; control characters would also make the SVG invalid.
func quotedIdentifierRune(r rune) bool {
	if r < utf8.RuneSelf {
		return r > ' ' && r < 0x7f
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isPlainIdentifier is shared by source formatting and routing keys so the
// two representations agree about which IDs need quoting.
func isPlainIdentifier(id string) bool {
	for index, r := range id {
		if r == '_' || unicode.IsLetter(r) || index > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return id != ""
}

// Parse reads a boxz document. The filename is used in diagnostics only.
func Parse(filename string, r io.Reader) (*Document, error) {
	parsed, err := documentParser.Parse(filename, r)
	if err != nil {
		return nil, err
	}

	doc := &Document{filename: filename}
	seen := make(map[string]lexer.Position)
	doc.root, err = convertElement(filename, parsed.Root, nil, seen)
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]*element)
	collectNodes(doc.root, nodes)
	var parsedEdges []*syntaxEdge
	if parsed.EdgeBlock != nil {
		parsedEdges = parsed.EdgeBlock.Edges
	}
	for _, raw := range parsedEdges {
		edge, edgeErr := convertEdge(filename, raw, nodes)
		if edgeErr != nil {
			return nil, edgeErr
		}
		doc.edges = append(doc.edges, edge)
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

func convertElement(filename string, raw *syntaxElement, parent *element, seen map[string]lexer.Position) (*element, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s: document must contain a root element", filename)
	}
	if previous, ok := seen[raw.ID]; ok {
		return nil, diagnostic(filename, raw.Pos, "duplicate element ID %q (first declared at %d:%d)", raw.ID, previous.Line, previous.Column)
	}
	seen[raw.ID] = raw.Pos

	element := &element{
		kind:     kind(raw.Kind),
		ID:       raw.ID,
		Parent:   parent,
		position: raw.Pos,
	}
	switch element.kind {
	case kindNode:
		if raw.Body != nil {
			return nil, diagnostic(filename, raw.Pos, "node %q cannot contain children", raw.ID)
		}
		if raw.Title == nil || *raw.Title == "" {
			element.Title = raw.ID
		} else {
			element.Title = *raw.Title
		}
	case kindHBox, kindVBox:
		if raw.Title != nil {
			element.Title = *raw.Title
		}
		element.containerAttributes.Bounded = element.Title != ""
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
	if element.kind != kindNode {
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
	if element.kind != kindNode && len(element.Children) == 0 {
		return nil, diagnostic(filename, raw.Pos, "%s %q must contain at least one child", raw.Kind, raw.ID)
	}
	return element, nil
}

// convertAttributes compiles the generic surface syntax into the typed model.
// Attribute names and scalar representations should not escape this boundary.
func convertAttributes(filename string, element *element, raw *syntaxAttributes) error {
	if raw == nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, attribute := range raw.Items {
		if seen[attribute.Key] {
			return diagnostic(filename, attribute.Pos, "duplicate attribute %q on %q", attribute.Key, element.ID)
		}
		seen[attribute.Key] = true
		switch {
		case element.kind == kindNode && attribute.Key == "spring":
			enabled, ok := booleanAttribute(attribute.Value)
			if !ok {
				return diagnostic(filename, attribute.Pos, "attribute %q on node %q must be a flag or boolean", attribute.Key, element.ID)
			}
			element.nodeAttributes.Spring = enabled
		case element.kind != kindNode && attribute.Key == "labelAlign":
			alignment, ok := labelAlignAttribute(attribute.Value)
			if !ok {
				return diagnostic(filename, attribute.Pos, "attribute %q on %s %q must be auto, left, center, or right", attribute.Key, element.kind, element.ID)
			}
			element.containerAttributes.LabelAlign = alignment
		case element.kind != kindNode && attribute.Key == "bounded":
			enabled, ok := booleanAttribute(attribute.Value)
			if !ok {
				return diagnostic(filename, attribute.Pos, "attribute %q on %s %q must be a flag or boolean", attribute.Key, element.kind, element.ID)
			}
			element.containerAttributes.Bounded = enabled
		default:
			return diagnostic(filename, attribute.Pos, "attribute %q is not supported on %s %q", attribute.Key, element.kind, element.ID)
		}
	}
	if element.Parent == nil && element.nodeAttributes.Spring {
		return diagnostic(filename, element.position, "root node %q cannot be spring-enabled", element.ID)
	}
	if element.kind != kindNode && element.Title == "" && element.containerAttributes.LabelAlign != "" {
		return diagnostic(filename, element.position, "%s %q has labelAlign but no title", element.kind, element.ID)
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

func labelAlignAttribute(value *syntaxAttributeValue) (labelAlignment, bool) {
	if value == nil || value.Ident == nil {
		return "", false
	}
	switch strings.ToLower(*value.Ident) {
	case string(labelAlignAuto):
		return labelAlignAuto, true
	case string(labelAlignLeft):
		return labelAlignLeft, true
	case string(labelAlignCenter):
		return labelAlignCenter, true
	case string(labelAlignRight):
		return labelAlignRight, true
	default:
		return "", false
	}
}

func convertEdge(filename string, raw *syntaxEdge, nodes map[string]*element) (*Edge, error) {
	fromSide, err := parseSide(filename, raw.Pos, raw.From.Side)
	if err != nil {
		return nil, err
	}
	toSide, err := parseSide(filename, raw.Pos, raw.To.Side)
	if err != nil {
		return nil, err
	}
	edge := &Edge{From: raw.From.ID, FromSide: fromSide, To: raw.To.ID, ToSide: toSide, position: raw.Pos}
	if err := validateEdge(nodes, edge, -1); err != nil {
		return nil, err
	}
	return edge, nil
}

func parseSide(filename string, pos lexer.Position, raw *string) (*Side, error) {
	if raw == nil {
		return nil, nil
	}
	side := Side(strings.ToUpper(*raw))
	if !validSide(side) {
		return nil, diagnostic(filename, pos, "unknown side %q (want N, E, S, or W)", *raw)
	}
	return &side, nil
}

func allowedSides(node *element) [2]Side {
	// A node enters the channel system owned by its immediate parent. Hboxes
	// expose their children vertically; vboxes expose them horizontally.
	if node.Parent == nil || node.Parent.kind == kindHBox {
		return [2]Side{North, South}
	}
	return [2]Side{West, East}
}

func collectNodes(element *element, nodes map[string]*element) {
	if element.kind == kindNode {
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

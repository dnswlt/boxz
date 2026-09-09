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
	Kind     Kind
	ID       string
	Title    string
	Children []*Element
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
	Root  *syntaxElement `parser:"@@"`
	Edges []*syntaxEdge  `parser:"@@*"`
}

type syntaxElement struct {
	Pos   lexer.Position
	Kind  string      `parser:"@Ident"`
	ID    string      `parser:"@Ident"`
	Title *string     `parser:"(@String)?"`
	Body  *syntaxBody `parser:"@@?"`
}

type syntaxBody struct {
	Children []*syntaxElement `parser:"'{' @@* '}'"`
}

type syntaxEdge struct {
	Pos  lexer.Position
	From *syntaxEndpoint `parser:"'edge' @@"`
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
	for _, raw := range parsed.Edges {
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

	element := &Element{Kind: Kind(raw.Kind), ID: raw.ID, Parent: parent, position: raw.Pos}
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
		if raw.Body == nil || len(raw.Body.Children) == 0 {
			return nil, diagnostic(filename, raw.Pos, "%s %q must contain at least one child", raw.Kind, raw.ID)
		}
	default:
		return nil, diagnostic(filename, raw.Pos, "unknown element kind %q", raw.Kind)
	}

	var children []*syntaxElement
	if raw.Body != nil {
		children = raw.Body.Children
	}
	for _, child := range children {
		converted, err := convertElement(filename, child, element, seen)
		if err != nil {
			return nil, err
		}
		element.Children = append(element.Children, converted)
	}
	return element, nil
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

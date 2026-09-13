package boxz

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"
)

// Nodes returns snapshots of the document's nodes in layout-tree order.
// Mutating the returned slice or its values does not change the document.
func (d *Document) Nodes() []Node {
	if d == nil || d.root == nil {
		return nil
	}
	var result []Node
	var visit func(*element)
	visit = func(element *element) {
		if element.kind == kindNode {
			result = append(result, Node{ID: element.ID, Title: element.Title})
			return
		}
		for _, child := range element.Children {
			visit(child)
		}
	}
	visit(d.root)
	return result
}

// Edges returns copies of the document's edges in declaration order.
// Endpoint-side pointers are copied as well, so the result may be freely
// edited before being passed to WithEdges.
func (d *Document) Edges() []Edge {
	if d == nil {
		return nil
	}
	result := make([]Edge, len(d.edges))
	for index, edge := range d.edges {
		result[index] = cloneEdgeValue(*edge)
	}
	return result
}

// WithEdges returns a new document with edges replaced in the supplied order.
// The receiver is unchanged. The update is atomic: an invalid edge leaves no
// partially modified document behind.
func (d *Document) WithEdges(edges []Edge) (*Document, error) {
	if err := d.checkInitialized(); err != nil {
		return nil, err
	}
	next := &Document{
		root:     d.root,
		filename: d.filename,
		edges:    make([]*Edge, len(edges)),
	}
	nodes := make(map[string]*element)
	collectNodes(next.root, nodes)
	for index, edge := range edges {
		cloned := cloneEdgeValue(edge)
		cloned.position = lexer.Position{Filename: next.filename}
		if err := validateEdge(nodes, &cloned, index); err != nil {
			return nil, err
		}
		next.edges[index] = &cloned
	}
	if _, err := buildRoutingPlan(next); err != nil {
		return nil, err
	}
	return next, nil
}

func (d *Document) checkInitialized() error {
	if d == nil || d.root == nil {
		return fmt.Errorf("boxz: uninitialized document")
	}
	return nil
}

func cloneEdgeValue(edge Edge) Edge {
	cloned := edge
	cloned.position = lexer.Position{}
	cloned.FromSide = cloneSide(edge.FromSide)
	cloned.ToSide = cloneSide(edge.ToSide)
	return cloned
}

func cloneSide(side *Side) *Side {
	if side == nil {
		return nil
	}
	cloned := *side
	return &cloned
}

// validateEdge is the single semantic gate shared by parsed and API-provided
// edges. Source edges retain line diagnostics; API edges use their list index.
func validateEdge(nodes map[string]*element, edge *Edge, index int) error {
	if nodes[edge.From] == nil {
		return edgeValidationError(edge, index, "source %q is not a node", edge.From)
	}
	if nodes[edge.To] == nil {
		return edgeValidationError(edge, index, "destination %q is not a node", edge.To)
	}
	if edge.From == edge.To {
		return edgeValidationError(edge, index, "is a self-edge on %q, which is not supported yet", edge.From)
	}
	if !validOptionalSide(edge.FromSide) {
		return edgeValidationError(edge, index, "has unknown source side %q (want N, E, S, or W)", *edge.FromSide)
	}
	if !validOptionalSide(edge.ToSide) {
		return edgeValidationError(edge, index, "has unknown destination side %q (want N, E, S, or W)", *edge.ToSide)
	}
	return nil
}

func edgeValidationError(edge *Edge, index int, format string, args ...any) error {
	if edge.position.Line > 0 {
		return diagnostic(edge.position.Filename, edge.position, "edge "+format, args...)
	}
	filename := edge.position.Filename
	if filename == "" {
		filename = "boxz"
	}
	return fmt.Errorf("%s: edge %d %s", filename, index+1, fmt.Sprintf(format, args...))
}

func validOptionalSide(side *Side) bool {
	return side == nil || validSide(*side)
}

func validSide(side Side) bool {
	return side == North || side == East || side == South || side == West
}

package boxz

import (
	"fmt"
	"strconv"
)

// seamSpec identifies the boundary between two adjacent children of a
// container. Routes through a seam have fixed endpoint sides, independent of
// the eventual pixel geometry.
type seamSpec struct {
	ID         string
	ParentID   string
	FirstChild int
	FromSide   Side
	ToSide     Side
	// Tracks are normalized to the parent's first and second child, independent
	// of edge direction. Different tracks imply a dogleg at DoglegCoordinate.
	FirstTrack       int
	SecondTrack      int
	TrackCount       int
	DoglegCoordinate float64
}

type routingPlan struct {
	// Seams maps source edge indexes to routes whose topology is already fixed.
	Seams map[int]*seamSpec
	// SeamTrackCount sizes each sibling gap before placement.
	SeamTrackCount map[string]int
	// PortCount is exact for seam endpoints; outer endpoints are counted below.
	PortCount   map[string]map[Side]int
	OuterDegree map[string]int
}

// buildRoutingPlan makes every decision that depends only on tree structure.
// In particular, seam selection must not depend on dimensions that routing may
// later enlarge.
func buildRoutingPlan(doc *Document) (*routingPlan, error) {
	plan := &routingPlan{
		Seams:          make(map[int]*seamSpec),
		SeamTrackCount: make(map[string]int),
		PortCount:      make(map[string]map[Side]int),
		OuterDegree:    make(map[string]int),
	}
	nodes := make(map[string]*element)
	collectNodes(doc.root, nodes)

	for edgeIndex, edge := range doc.edges {
		spec := classifySeam(nodes[edge.From], nodes[edge.To])
		if spec != nil && constraintsAllowSeam(edge, spec) {
			spec.FirstTrack = plan.SeamTrackCount[spec.ID]
			spec.SecondTrack = spec.FirstTrack
			plan.SeamTrackCount[spec.ID]++
			plan.Seams[edgeIndex] = spec
			incrementPort(plan.PortCount, edge.From, spec.FromSide)
			incrementPort(plan.PortCount, edge.To, spec.ToSide)
			continue
		}

		if err := validateChannelConstraint(edgeIndex, edge, nodes[edge.From], edge.FromSide); err != nil {
			return nil, err
		}
		if err := validateChannelConstraint(edgeIndex, edge, nodes[edge.To], edge.ToSide); err != nil {
			return nil, err
		}
		plan.OuterDegree[edge.From]++
		plan.OuterDegree[edge.To]++
	}
	for _, spec := range plan.Seams {
		spec.TrackCount = plan.SeamTrackCount[spec.ID]
	}
	return plan, nil
}

// classifySeam returns a direct sibling-seam route when both endpoints are on
// the required recursive frontiers.
func classifySeam(from, to *element) *seamSpec {
	if from == nil || to == nil || from == to {
		return nil
	}
	lca := lowestCommonAncestor(from, to)
	if lca == nil || lca.kind == kindNode {
		return nil
	}
	fromChild := childBelow(lca, from)
	toChild := childBelow(lca, to)
	fromIndex := childIndex(lca, fromChild)
	toIndex := childIndex(lca, toChild)
	// The endpoints need not be immediate siblings, but their subtrees must be
	// adjacent at the lowest container that contains both.
	if fromIndex < 0 || toIndex < 0 || absInt(fromIndex-toIndex) != 1 {
		return nil
	}

	firstIndex := fromIndex
	if toIndex < firstIndex {
		firstIndex = toIndex
	}
	spec := &seamSpec{ID: seamID(lca.ID, firstIndex), ParentID: lca.ID, FirstChild: firstIndex}
	if lca.kind == kindVBox {
		if fromIndex < toIndex {
			spec.FromSide, spec.ToSide = South, North
		} else {
			spec.FromSide, spec.ToSide = North, South
		}
	} else {
		if fromIndex < toIndex {
			spec.FromSide, spec.ToSide = East, West
		} else {
			spec.FromSide, spec.ToSide = West, East
		}
	}
	// Adjacency alone is insufficient: each node must be visible on the facing
	// recursive frontier of its subtree.
	if !onFrontier(fromChild, from, spec.FromSide) || !onFrontier(toChild, to, spec.ToSide) {
		return nil
	}
	return spec
}

// onFrontier reports whether node is exposed on one side of root according to
// container order alone; it deliberately performs no geometric visibility test.
func onFrontier(root, node *element, side Side) bool {
	if root == node {
		return root.kind == kindNode
	}
	if root.kind == kindNode || !containsElement(root, node) {
		return false
	}

	// Along a container's stacking axis only the first or last child is exposed.
	// Perpendicular to that axis, every child's matching frontier is exposed.
	if root.kind == kindHBox {
		switch side {
		case West:
			return onFrontier(root.Children[0], node, side)
		case East:
			return onFrontier(root.Children[len(root.Children)-1], node, side)
		case North, South:
			for _, child := range root.Children {
				if containsElement(child, node) {
					return onFrontier(child, node, side)
				}
			}
		}
	} else {
		switch side {
		case North:
			return onFrontier(root.Children[0], node, side)
		case South:
			return onFrontier(root.Children[len(root.Children)-1], node, side)
		case West, East:
			for _, child := range root.Children {
				if containsElement(child, node) {
					return onFrontier(child, node, side)
				}
			}
		}
	}
	return false
}

func lowestCommonAncestor(left, right *element) *element {
	ancestors := make(map[*element]bool)
	for current := left; current != nil; current = current.Parent {
		ancestors[current] = true
	}
	for current := right; current != nil; current = current.Parent {
		if ancestors[current] {
			return current
		}
	}
	return nil
}

func childBelow(ancestor, descendant *element) *element {
	current := descendant
	for current != nil && current.Parent != ancestor {
		current = current.Parent
	}
	return current
}

func childIndex(parent, child *element) int {
	for index, candidate := range parent.Children {
		if candidate == child {
			return index
		}
	}
	return -1
}

func containsElement(root, target *element) bool {
	for current := target; current != nil; current = current.Parent {
		if current == root {
			return true
		}
	}
	return false
}

func constraintsAllowSeam(edge *Edge, spec *seamSpec) bool {
	return (edge.FromSide == nil || *edge.FromSide == spec.FromSide) &&
		(edge.ToSide == nil || *edge.ToSide == spec.ToSide)
}

func validateChannelConstraint(edgeIndex int, edge *Edge, node *element, side *Side) error {
	if side == nil || node.Parent == nil {
		return nil
	}
	allowed := allowedSides(node)
	if *side == allowed[0] || *side == allowed[1] {
		return nil
	}
	if edge.position.Line > 0 {
		return diagnostic(edge.position.Filename, edge.position,
			"side %s is not available for edge %s -> %s", *side, edge.From, edge.To)
	}
	filename := edge.position.Filename
	if filename == "" {
		filename = "boxz"
	}
	return fmt.Errorf("%s: edge %d side %s is not available for %s -> %s",
		filename, edgeIndex+1, *side, edge.From, edge.To)
}

func incrementPort(counts map[string]map[Side]int, nodeID string, side Side) {
	if counts[nodeID] == nil {
		counts[nodeID] = make(map[Side]int)
	}
	counts[nodeID][side]++
}

func seamID(parentID string, firstChild int) string {
	return fmt.Sprintf("%s:seam:%d", keyPart(parentID), firstChild)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// keyPart embeds an element ID in a colon-separated routing key. Plain
// identifiers pass through; any other ID is quoted, so it cannot equal a plain
// one and ends unambiguously at its closing quote.
func keyPart(id string) string {
	if !isPlainIdentifier(id) {
		return strconv.Quote(id)
	}
	return id
}

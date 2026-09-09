package boxz

import "fmt"

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
	nodes := make(map[string]*Element)
	collectNodes(doc.Root, nodes)

	for edgeIndex, edge := range doc.Edges {
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

		if err := validateChannelConstraint(edge, nodes[edge.From], edge.FromSide); err != nil {
			return nil, err
		}
		if err := validateChannelConstraint(edge, nodes[edge.To], edge.ToSide); err != nil {
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
func classifySeam(from, to *Element) *seamSpec {
	if from == nil || to == nil || from == to {
		return nil
	}
	lca := lowestCommonAncestor(from, to)
	if lca == nil || lca.Kind == KindNode {
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
	if lca.Kind == KindVBox {
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
func onFrontier(root, node *Element, side Side) bool {
	if root == node {
		return root.Kind == KindNode
	}
	if root.Kind == KindNode || !containsElement(root, node) {
		return false
	}

	// Along a container's stacking axis only the first or last child is exposed.
	// Perpendicular to that axis, every child's matching frontier is exposed.
	if root.Kind == KindHBox {
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

func lowestCommonAncestor(left, right *Element) *Element {
	ancestors := make(map[*Element]bool)
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

func childBelow(ancestor, descendant *Element) *Element {
	current := descendant
	for current != nil && current.Parent != ancestor {
		current = current.Parent
	}
	return current
}

func childIndex(parent, child *Element) int {
	for index, candidate := range parent.Children {
		if candidate == child {
			return index
		}
	}
	return -1
}

func containsElement(root, target *Element) bool {
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

func validateChannelConstraint(edge *Edge, node *Element, side *Side) error {
	if side == nil || node.Parent == nil {
		return nil
	}
	allowed := allowedSides(node)
	if *side == allowed[0] || *side == allowed[1] {
		return nil
	}
	return diagnostic(edge.position.Filename, edge.position,
		"side %s is not available for edge %s -> %s", *side, edge.From, edge.To)
}

func incrementPort(counts map[string]map[Side]int, nodeID string, side Side) {
	if counts[nodeID] == nil {
		counts[nodeID] = make(map[Side]int)
	}
	counts[nodeID][side]++
}

func seamID(parentID string, firstChild int) string {
	return fmt.Sprintf("%s:seam:%d", parentID, firstChild)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

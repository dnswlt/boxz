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
	Lane       int
	LaneCount  int
}

type routingPlan struct {
	Seams         map[int]*seamSpec
	SeamLaneCount map[string]int
	PortCount     map[string]map[Side]int
	OuterDegree   map[string]int
}

func buildRoutingPlan(doc *Document) (*routingPlan, error) {
	plan := &routingPlan{
		Seams:         make(map[int]*seamSpec),
		SeamLaneCount: make(map[string]int),
		PortCount:     make(map[string]map[Side]int),
		OuterDegree:   make(map[string]int),
	}
	nodes := make(map[string]*Element)
	collectNodes(doc.Root, nodes)

	for edgeIndex, edge := range doc.Edges {
		spec := classifySeam(nodes[edge.From], nodes[edge.To])
		if spec != nil && constraintsAllowSeam(edge, spec) {
			spec.Lane = plan.SeamLaneCount[spec.ID]
			plan.SeamLaneCount[spec.ID]++
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
		spec.LaneCount = plan.SeamLaneCount[spec.ID]
	}
	return plan, nil
}

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
	if !onFrontier(fromChild, from, spec.FromSide) || !onFrontier(toChild, to, spec.ToSide) {
		return nil
	}
	return spec
}

func onFrontier(root, node *Element, side Side) bool {
	if root == node {
		return root.Kind == KindNode
	}
	if root.Kind == KindNode || !containsElement(root, node) {
		return false
	}

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

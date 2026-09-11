# Springs

Springs add relational spacing without introducing absolute pixel sizes. They
consume surplus established elsewhere in the layout; they do not create space
on their own.

In an `hbox`, springs expand horizontally. In a `vbox`, they expand vertically.
The examples below use rows, but every rule has an exact column-wise transpose.

## Internal springs

A standalone spring has zero minimum size and receives one share of the
available surplus:

```boxz
vbox root {
  hbox spread {
    node left
    spring
    node right
  }

  node widthSource "This wider sibling establishes the available width"
}
```

The wider sibling determines the `vbox` content width. The nested `hbox` can
accept that width because it contains a horizontal spring, which places the
surplus between `left` and `right`.

Springs remain separate even when adjacent. With three total spring shares,
the pair below receives two thirds of the surplus:

```boxz
hbox row {
  node left
  spring
  spring
  node right [spring]
}
```

## Expandable nodes

The `spring` attribute makes a node spring-like while preserving its normal
minimum size:

```boxz
node worker "Worker" [spring]
```

As a child of an `hbox`, the node may grow in width. As a child of a `vbox`, it
may grow in height. `[spring = true]` is equivalent to the bare flag;
`[spring = false]` disables growth.

The containing box deliberately defines the growth axis. A spring-enabled node
does not also stretch across that box's other axis. Wrapping it in a
perpendicular container is therefore a real layout instruction, not a redundant
wrapper. A root node cannot be spring-enabled because it has no containing box
to define an axis.

Every standalone spring and every growable direct child has equal weight. If a
growable child is itself a nested container, its parent gives it one share and
the child redistributes that share among its own springs. This preserves local,
hierarchical layout decisions rather than flattening all descendant springs
into one global pool.

## Edge springs and alignment

Springs before the first child or after the last child act as flexible margins:

```boxz
hbox leftAligned  { node a node b spring }
hbox rightAligned { spring node a node b }
hbox centered     { spring node a node b spring }
```

An edge spring consumes part of the container's available placement slot, but
does not enlarge its compact routing rectangle. Consequently, owned channels do
not extend across empty alignment space merely because the row or column was
given a larger slot.

Multiple edge springs are valid and retain their individual weights. Combining
edge springs with internal springs or expandable nodes divides the same surplus
between all of them.

## Nesting and routing

Growth capability propagates through nested containers. A deeply nested row can
therefore consume width established by a distant sibling in an ancestor
`vbox`; vertical growth propagates symmetrically through an ancestor `hbox`.

Springs are layout metadata, not elements in the routing topology. In a
sequence containing `node a`, then `spring`, then `node b`, the two nodes remain
adjacent and eligible for a direct seam route. An internal spring enlarges that
seam, while an edge spring only aligns the compact routing rectangle within its
available slot.

## Where surplus comes from

Common sources are:

- a wider row in the same `vbox`;
- a taller column in the same `hbox`;
- a growable ancestor that received surplus higher in the tree;
- routing space added for ports, lanes, or seam tracks.

The root has only its intrinsic size today. Standalone springs inside the root
container do have an axis, but have no surplus to consume unless another layout
relationship enlarges that axis. Boxz intentionally has no absolute `space 200`
construct.

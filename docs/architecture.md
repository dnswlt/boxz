# Architecture

Boxz treats the source tree as the layout. It does not search for a globally
better arrangement, because preserving source order is what makes diagrams
stable across edits.

The rendering pipeline is:

```text
source -> syntax tree -> routing plan -> measure/place <-> route -> SVG
```

The double arrow is a bounded fixed-point calculation: routing discovers how
many lanes each outer channel and cyclic seam needs, and layout grows that
routing space before trying again. Sizes only grow, so a stable result does not
oscillate.

## Model

An `hbox` lays out children from west to east and owns outer channels on its
north and south sides. A `vbox` lays out children from north to south and owns
west and east channels. Nodes are the only visible, connectable elements.
Container rectangles are also rendered for now as a debugging aid.

There are two kinds of routes:

- A **seam route** crosses the gap between adjacent children of one container.
  Its endpoint sides follow the container axis: S/N in a `vbox`, E/W in an
  `hbox`.
- An **outer-channel route** uses channels on container boundaries and risers
  connecting nested containers to their parents.

Seam eligibility is topological, not geometric. This avoids making a routing
decision from coordinates that may change when routing itself requires more
space.

## Recursive frontiers

For a node, every frontier is the node itself. Container frontiers recurse as
follows:

```text
hbox: N/S = all child N/S frontiers
      W   = first child's W frontier
      E   = last child's E frontier

vbox: W/E = all child W/E frontiers
      N   = first child's N frontier
      S   = last child's S frontier
```

Two nodes may use a seam when their lowest common ancestor places them in
adjacent child subtrees and both nodes occur on the facing recursive frontiers.
This is why a deeply nested node can connect directly to a node in the next row
or column without a visibility calculation.

`topology.go` performs this classification before geometry exists. The result
also records exact seam port counts and conservative outer-route degrees so
node measurement can reserve enough perimeter for ports.

### Seam-local track assignment

The eligible seam and its endpoint sides are topological, but track order is
assigned after placement because collisions depend on exact port alignment.
Each seam is solved independently as a small channel-routing problem.

For a vertical seam, an upper access leg and a lower access leg at the same X
coordinate impose an ordering constraint: the upper edge's track must be above
the lower edge's track. Horizontal seams use the transposed rule. A stable
topological sort provides the track order when these constraints are acyclic.

A constraint cycle cannot use one track per edge without an overlapping access
leg. In that case, the seam uses two banks: all first-child tracks, then all
second-child tracks. Each edge changes banks at a private dogleg coordinate.
Only this seam grows to fit the additional tracks; the surrounding layout and
node order remain unchanged.

## Measurement and placement

`layout.go` first measures the tree bottom-up, then assigns rectangles top-down.
Node titles use a conservative character-width estimate. Nodes may grow further
to fit their anticipated ports.

Containers reserve three distinct kinds of space:

- child rectangles;
- seams between consecutive children;
- outer channel bands on the two sides allowed by the container kind.

Each outer channel is represented by one center line during routing. Individual
edge lanes are offsets from that line and are applied only when producing the
display path.

## Outer-channel graph

`route.go` turns channel center lines and hierarchy risers into a rectilinear
graph. Every intersection splits both participating segments. Node-side portals
become graph vertices, so a graph path always starts and ends on legal sides.

Adjacent container children also receive local **crossbars** between their
facing boundary networks. A crossbar is created only at a structural coordinate
already exposed by a channel endpoint, hierarchy riser, or channel
intersection, and only when both boundaries are routable there. Its segment
lies entirely in the empty sibling gap. Node boundaries are excluded so an
unrelated route cannot use a node as a transit junction.

Crossbars do not weaken recursive-frontier rules. Frontier-to-frontier edges
still receive direct seam routes first; an outer route can cross a sibling gap
only after it has reached a boundary channel. Crossbars are named routing
resources and contribute to the congestion tie-breaker.

After ports and direct seam tracks are final, movable crossbars are allocated in
the same sibling-gap domain. The direct routes' endpoint coordinates are
reserved first. A crossbar prefers the resolved coordinate of its straight run:
the exact node port when endpoint-adjacent, otherwise the assigned channel lane.
It then chooses a distinct nearby coordinate within the overlap of its facing
channels. Short shoulders connect a shifted crossbar back to those channels.
This prevents both false conflicts with already-separated ports and real
overlaps with a direct route's access leg without changing route topology.

Outer routes use Dijkstra's algorithm with a lexicographic cost:

1. Manhattan distance;
2. bend count;
3. previous use of the traversed channels.

The incoming direction is part of the search state because bend cost cannot be
derived from position alone. Within one search, fixed endpoint-side order,
sorted adjacency, and queue insertion order provide deterministic tie breaking.
Edges are routed in declaration order, so earlier edges influence congestion
choices for later ones, but no route can reposition a node.

## Display paths

The routing graph uses channel center lines. After all routes are known,
`svg.go` assigns stable node ports and a lane number to every channel use.
Collinear graph segments are merged into straight runs before lane offsets are
applied. This is important: offsetting a channel segment separately from an
adjacent collinear riser would create a small, meaningless jog.

The final path reconnects each offset run to its exact node port with one
orthogonal projection and removes duplicate or redundant collinear points.

## Invariants worth preserving

- Source order determines node order.
- Routing may grow geometry, never reorder it.
- Seam classification depends only on the element tree and explicit side
  constraints.
- Seam track ordering depends only on final port alignment within that seam.
- Sibling crossbars connect routing networks, never arbitrary visible points or
  node interiors.
- Direct seam access coordinates and movable crossbar tracks cannot coincide.
- All route segments are horizontal or vertical.
- Channel allocation grows monotonically until stable.
- Iteration over maps must not affect rendered output.
- Equal-cost paths use fixed side order, sorted adjacency, and search insertion
  order as deterministic tie-breakers.
- Edge declaration order governs congestion history and lane numbering.

Tests in `boxz_test.go` exercise these invariants, especially recursive
frontiers, deterministic output, orthogonality, and the absence of lane-offset
jogs.

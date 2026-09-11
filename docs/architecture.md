# Architecture

Boxz treats the source tree as the layout. It does not search for a globally
better arrangement, because preserving source order is what makes diagrams
stable across edits.

The rendering pipeline is:

```text
source -> syntax tree -> routing plan -> measure/place <-> route -> SVG
```

The routing plan contains decisions that depend only on source topology. The
double arrow is a bounded fixed-point calculation over geometry: routing
discovers how many lanes each outer channel needs, and physical port alignment
may reveal that a cyclic seam needs additional tracks. Layout grows that space
before trying again. Allocated track counts only grow, so the calculation cannot
oscillate between smaller and larger allocations.

More precisely, one iteration:

1. measures and places the tree using the currently allocated routing space;
2. builds the center-line routing graph and routes every edge;
3. chooses exact node ports and solves seam-local track ordering;
4. repeats if any outer channel or seam needs more tracks.

Once allocations are stable, seam paths are rebuilt against the final geometry
and movable sibling crossbars receive their physical coordinates. SVG rendering
then converts the abstract routes into display paths.

The implementation follows those phase boundaries:

- `syntax.go` parses the ordered tree and performs topology-dependent
  validation;
- `topology.go` classifies direct seams and records conservative port demand;
- `layout.go` measures and places elements and their owned channels;
- `route.go` constructs the structural graph and finds outer routes;
- `seam.go` assigns physical seam and crossbar tracks;
- `svg.go` runs the fixed point and turns solved routes into SVG paths.

## Model

An `hbox` lays out children from west to east and owns outer channels on its
north and south sides. A `vbox` lays out children from north to south and owns
west and east channels. Nodes are the only visible, connectable elements.
Containers are structural: their rectangles and routing machinery are rendered
only when debug output is enabled.

There are two kinds of routes:

- A **seam route** crosses the gap between adjacent children of one container.
  Its endpoint sides follow the container axis: S/N in a `vbox`, E/W in an
  `hbox`.
- An **outer-channel route** uses channels on container boundaries and risers
  connecting nested containers to their parents.

Seam eligibility is topological, not geometric. This avoids making a routing
decision from coordinates that may change when routing itself requires more
space.

### Endpoint sides and ports

A seam route receives its endpoint sides from the orientation of that seam. An
outer route instead enters the channel system owned by each node's immediate
parent: a child of an `hbox` can use N/S, while a child of a `vbox` can use W/E.
An explicit endpoint side constrains these choices and is rejected when the
topology cannot provide it.

Topology planning knows exact side counts for seam endpoints. The side of an
unconstrained outer endpoint is not known until graph search, so measurement
conservatively reserves its outer degree on both parent-facing sides. This can
make a node larger than ultimately necessary, but never too small for its
ports.

After routing has selected sides, ports are distributed evenly and
deterministically along each node side. Declaration order determines their
order. These exact coordinates drive seam-track conflict detection and final
endpoint projection; center points used while building the routing graph are
only provisional portals.

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

The current layout is entirely intrinsic: containers take the size required by
their children and routing bands, and children are centered on the parent's
cross axis. There is no separate allocated slot, stretch, or alignment model
yet.

## Outer-channel graph

`route.go` turns channel center lines and hierarchy risers into a rectilinear
graph. Every intersection splits both participating segments. Node-side portals
become graph vertices, so a graph path always starts and ends on legal sides.

This is a structural routing graph, not a geometric visibility graph. Routes
can travel only over channels, hierarchy risers, and generated crossbars; the
router does not search arbitrary empty coordinates around rectangles.

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

Distance and bend costs are evaluated on center-line geometry. Lane offsets and
exact endpoint-port projections are display refinements and do not feed back
into shortest-path selection.

## Display paths

The routing graph uses channel center lines. After all routes are known,
`svg.go` assigns stable node ports and a lane number to every channel use.
Collinear graph segments are merged into straight runs before lane offsets are
applied. This is important: offsetting a channel segment separately from an
adjacent collinear riser would create a small, meaningless jog.

At each endpoint, the final path reconnects the first or last offset run to its
exact node port with an orthogonal projection. Duplicate points and redundant
collinear points are then removed. Arrowheads and styling are SVG concerns and
do not participate in layout or routing.

## Debug rendering

Debug rendering is an SVG-only view of the same solved geometry; it never
changes measurement or routing. It draws structural container rectangles, all
center-line graph segments, and exact allocated endpoint ports behind or above
the normal diagram as appropriate. Channels, unnamed hierarchy risers, and
sibling crossbars use distinct classes and colors. Named resources also carry
`data-resource` attributes for inspection.

## Invariants worth preserving

- Source order determines node order.
- Routing may grow geometry, never reorder it.
- Structural containers are routing topology, not connectable obstacles.
- Seam classification depends only on the element tree and explicit side
  constraints.
- Outer routes use only the finite structural routing graph; they do not gain
  geometric shortcuts from accidental alignment.
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

# Architecture

Boxz treats the source tree as the layout. It does not search for a globally
better arrangement, because preserving source order is what makes diagrams
stable across edits.

The rendering pipeline is:

```text
source -> syntax tree -> topology plan
       -> [measure/place -> route intents -> track allocation]*
       -> exact polylines -> SVG
```

The routing plan contains decisions that depend only on source topology. The
repeated section is a bounded fixed-point calculation over geometry: routing
discovers how many lanes each channel and hierarchy connector needs, while
physical port alignment may reveal that a cyclic seam needs additional tracks.
Layout grows only that local space before trying again. Allocated capacities
only grow, so the calculation cannot oscillate between smaller and larger
allocations.

More precisely, one iteration:

1. measures and places the tree using the currently allocated routing space;
2. builds the center-line routing graph and chooses route intents;
3. chooses exact node ports and rebuilds prescribed seam routes from them;
4. allocates channel lanes, seam tracks, and movable connector tracks;
5. repeats if any local routing domain needs more capacity.

Once allocations are stable, the solver materializes exact orthogonal
polylines. It returns those paths together with the same port map used to build
them. SVG rendering only paints solved geometry; it does not allocate ports,
move tracks, or repair endpoints.

The implementation follows those phase boundaries:

- `syntax.go` parses the ordered tree and performs topology-dependent
  validation;
- `topology.go` classifies direct seams and records conservative port demand;
- `layout.go` measures and places elements and their owned channels;
- `route.go` constructs the structural graph and finds outer routes;
- `seam.go` assigns physical seam and movable-connector tracks;
- `solve.go` owns the fixed point, port allocation, and path materialization;
- `svg.go` places labels and renders the solved layout.

## Model

An `hbox` lays out children from west to east and owns outer channels on its
north and south sides. A `vbox` lays out children from north to south and owns
west and east channels. Nodes are the only connectable elements. Every
container is structural, while `ContainerAttributes.Bounded` independently
controls whether it creates a visible boundary and routing region. Parsing
initializes that property from the presence of a title, then applies an
explicit `bounded` override. Routing never infers region semantics from title
text.

Attribute syntax is deliberately generic, but the validated model is not.
`syntax.go` converts parsed key/value pairs into explicit subject-specific
fields such as `NodeAttributes.Spring` and
`ContainerAttributes.LabelAlign` and `ContainerAttributes.Bounded`. Unknown
names and incompatible value types are rejected at that boundary; routing and
layout never interpret raw attribute strings or generic maps.

There are two ways to choose a route intent:

- A **seam route** crosses the gap between adjacent children of one container.
  Its endpoint sides follow the container axis: S/N in a `vbox`, E/W in an
  `hbox`.
- An **outer-channel route** uses channels on container boundaries and risers
  connecting nested containers to their parents.

The distinction ends after path selection. Both kinds declare their physical
occupancy and participate in the same local track allocation. A prescribed
seam route must not become invisible to outer-route allocation merely because
it did not pass through graph search.

Seam eligibility is topological, not geometric. This avoids making a routing
decision from coordinates that may change when routing itself requires more
space.

### Graph resources and track domains

The router separates two identities that often, but not always, have the same
name:

- A **graph resource** is an edge available to pathfinding and a key for the
  congestion tie-breaker.
- A **track domain** is shared physical space whose collinear users must receive
  distinct coordinates.

Ordinary channel segments use the channel ID for both. Several sibling
crossbar graph edges instead share one seam connector domain. A prescribed seam
path can claim a hierarchy or sibling connector domain without having a graph
resource at all.

This separation is important for correctness: path selection decides where an
edge may travel; domain allocation decides how multiple selected paths coexist
there. Perpendicular crossings are allowed and therefore do not contend for a
track.

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

Provisional seam paths start at node-side centers because exact ports do not
exist until all endpoint sides are known. Final seam reconstruction calls the
same recursive exposure operation with allocated ports instead. Consequently,
access legs change direction on their seam tracks rather than being repaired
along a node border during display.

## Measurement and placement

`layout.go` first measures minimum sizes and growth capabilities bottom-up, then
assigns available rectangles top-down. Node titles use a conservative
character-width estimate. Nodes may grow further to fit their anticipated ports.

Containers reserve three distinct kinds of space:

- child rectangles;
- seams between consecutive children, including lanes used by their
  parent-owned seam channels;
- outer channel bands on the two sides allowed by the container kind.

A titled container additionally reserves a fixed-height label strip at its top.
For an `hbox`, the north channel band lies immediately below that strip; for a
`vbox`, the west and east bands flank it. The title width is deliberately absent
from bottom-up measurement; final-width truncation and horizontal placement
belong to the display pass.

Each outer channel is represented by one center line during routing. Individual
edge lanes are offsets from that line and are applied only when producing the
display path.

### Springs and surplus

Standalone springs are stored as weighted gaps around the real children and
therefore do not enter topology or recursive-frontier calculations. Bottom-up
measurement propagates per-axis growth capability; top-down placement
distributes surplus among standalone springs and spring-like children.

The placement slot and routing rectangle differ when a leading or trailing
spring consumes surplus. Internal springs enlarge the sibling seam, while edge
springs align a compact rectangle whose owned channels exclude the unused
margin. See [Springs](springs.md) for the user-facing semantics and examples.

## Outer-channel graph

`route.go` turns channel center lines, sibling-seam channels, and hierarchy
risers into a rectilinear graph. Every intersection splits both participating
segments. Node-side portals become graph vertices, so a graph path always
starts and ends on legal sides.

Graph segments carry an effective routing-region ID. A bounded container owns
its internal segments; segments introduced by an unbounded container belong to
the nearest bounded ancestor, or to the root canvas when there is none. Before
Dijkstra search, the endpoints determine the smallest common bounded scope and
the bounded endpoint branches that may be entered. Edges in other regions are
filtered from that search.

Every sibling gap also contains a channel owned by the parent. For a `vbox` it
is horizontal; for an `hbox` it is vertical. Recursively, those same channels
make unbounded containers permeable through their inter-child gaps. In a
bounded container they remain private internal routing space. Direct seam
tracks and searched seam-channel lanes reserve disjoint slots in the same gap.
Matching channels in the leading and trailing padding provide passage for a
container with only one child and alternative tracks for larger containers.
Their padding grows monotonically when several routes need separate lanes.

Container-to-parent transitions have two forms. A transition that continues an
orthogonal child channel collinearly remains in that channel's track domain, so
the whole straight run receives one lane offset. A transition between parallel
child and parent channels is a movable **hierarchy connector**. Its graph edge
is placed at a representative center coordinate, but physical uses may be
assigned anywhere in the overlap of the two channels.

Hierarchy-connector demand participates in the fixed point. North/south
connectors can grow their child container horizontally, and east/west
connectors can grow it vertically. This keeps an arbitrary number of tracks
inside the nested container instead of relying on its incidental minimum size.
Connector allocation changes only the local coordinate and adds short shoulders
at its ends; it never changes route topology.

Node access legs remain unnamed terminal geometry because exact endpoint ports
already separate them and unrelated routes may not use nodes as transit
junctions.

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
only after it has reached a boundary channel. Crossbars have graph-resource
identities and contribute to the congestion tie-breaker, but all crossbars
through one sibling gap share a physical connector domain.

Hierarchy bridges and sibling crossbars use the same connector allocator.
Every use supplies a preferred coordinate and legal interval. Prescribed seam
uses are allocated first, so a clean direct line normally remains straight;
searched routes choose the nearest free coordinate. Equal-priority choices use
edge declaration order. Shifted uses receive short shoulders at the connector
ends. This prevents both false conflicts with already-separated ports and real
overlaps between route families without changing either route's topology.

Outer routes use Dijkstra's algorithm with a lexicographic cost:

1. Manhattan distance;
2. bend count;
3. previous use of the traversed routing resources.

The incoming direction is part of the search state because bend cost cannot be
derived from position alone. Within one search, fixed endpoint-side order,
sorted adjacency, and queue insertion order provide deterministic tie breaking.
Edges are routed in declaration order, so earlier edges influence congestion
choices for later ones, but no route can reposition a node.

Distance and bend costs are evaluated on center-line geometry. Lane offsets and
exact endpoint-port projections are display refinements and do not feed back
into shortest-path selection.

## Physical track allocation and display paths

The routing graph and prescribed seam recursion initially produce center-line
route intents. A route retains graph-resource and track-domain boundaries even
when adjacent segments are collinear; simplifying those boundaries too early
would hide shared-space occupancy from the allocator.

Channel domains assign stable parallel lane offsets. Movable connector domains
assign absolute coordinates within their legal interval. A connector use that
moves from its preferred coordinate receives local orthogonal shoulders. Only
after those decisions are complete are adjacent geometric runs simplified.

At each outer-route endpoint, materialization reconnects the first or last
offset run to its exact node port with an orthogonal projection. Final seam
paths already start and end at their allocated ports, making this projection a
no-op for them. Arrowheads and styling are SVG concerns and do not participate
in layout or routing.

Container labels are also a display concern after their fixed strip has been
reserved. Once lane-offset display routes are known, an automatically aligned
label runs a one-dimensional sweep over route-intersection events in its strip.
It chooses the position covering the fewest route segments and uses the leftmost
position as a deterministic tie-breaker. Explicit `left`, `center`, and `right`
alignments bypass the sweep. Visible text is heuristically truncated to the
final strip width; the full title remains in the SVG `<title>` element.

## Debug rendering

Debug rendering is an SVG-only view of the same solved geometry; it never
changes measurement or routing. It draws structural container rectangles, all
center-line graph segments, and exact allocated endpoint ports behind or above
the normal diagram as appropriate. Channels, hierarchy risers, and sibling
crossbars use distinct classes and colors. Named resources also carry
`data-resource` attributes; graph segments whose physical domain differs also
carry `data-domain`.

## Invariants worth preserving

- Source order determines node order.
- Routing may grow geometry, never reorder it.
- Springs affect placement and dimensions but are absent from the element tree
  used for topology and recursive frontiers.
- Edge springs align a compact routing rectangle; they do not extend its owned
  channels through empty alignment space.
- Structural containers are never connectable endpoints. Unbounded containers
  are permeable layout topology; bounded containers are scoped routing regions
  and obstacles to unrelated routes.
- Container label height affects layout, but label width and horizontal
  placement never affect measurement or routing.
- Seam classification depends only on the element tree and explicit side
  constraints.
- Outer routes use only the finite structural routing graph; they do not gain
  geometric shortcuts from accidental alignment.
- A graph resource controls pathfinding and congestion; a track domain controls
  physical coexistence. Code must not assume those identities always match.
- Every collinear traversal through shared routing space declares a domain use
  before exact paths are materialized.
- Hierarchy bridges and sibling crossbars are movable connector domains; node
  access legs remain port-projected endpoint geometry.
- Seam track ordering depends only on final port alignment within that seam.
- Final seam paths begin and end at the same allocated ports returned to the
  renderer.
- Every display route leaves and enters its endpoint perpendicular to the node
  side; no endpoint segment runs along a node border.
- Distinct display routes do not share a collinear segment longer than the
  rendering tolerance.
- Sibling crossbars connect routing networks, never arbitrary visible points or
  node interiors.
- Direct seam access and searched routes cannot receive the same connector
  track.
- All route segments are horizontal or vertical.
- Channel, connector, and seam capacity grows monotonically until stable.
- Iteration over maps must not affect rendered output.
- Equal-cost paths use fixed side order, sorted adjacency, and search insertion
  order as deterministic tie-breakers.
- Edge declaration order governs congestion history and lane numbering.
- Graph vertices use exact point identity. Derived intersections must reuse
  coordinates from stored geometry rather than recomputing equivalent floats.

Tests in `boxz_test.go` exercise these invariants, especially recursive
frontiers, deterministic output, orthogonality, node separation, route avoidance
of unrelated node interiors, and the absence of lane-offset jogs.

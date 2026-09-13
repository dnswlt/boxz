# Boxz: Authored Structure, Automatic Routes

_A conceptual model for stable architecture diagrams_

## Abstract

Architecture diagrams have two structures at once. They are graphs: nodes are
connected by edges. They are also compositions: components sit beside, above,
inside, and between other components. Many diagram tools ask a graph-layout
algorithm to infer both structures from connectivity. Direct drawing tools take
the opposite approach and record nearly every coordinate.

Boxz occupies the space between those approaches. The author specifies an
ordered tree of horizontal and vertical boxes, while the engine measures its
contents and routes orthogonal edges through routing space owned by that tree.
The author controls the diagram's coarse spatial argument; the engine handles
the metric detail needed to keep edges traceable.

This paper describes that model. Its main ideas are recursive box layout,
topological rather than geometric adjacency, structural routing channels,
separate path selection and track allocation, and a monotone feedback loop in
which routing may create more space but may never reorder nodes.

## 1. A diagram is both a graph and a composition

Consider a typical system sketch: clients are above services, data stores are
below them, and two services belong to the same subsystem. Connectivity alone
does not fully express those choices. The same graph could plausibly be drawn
left-to-right, top-to-bottom, or with a database near the center to shorten its
incident edges. All might be valid graph drawings, but they tell different
stories.

At the other extreme, an editor can let the author place every rectangle and
bend point. That records the intended story precisely, but also records many
incidental facts. A longer title, one extra edge, or a newly inserted component
can turn coordinate maintenance into drawing work.

Boxz separates the responsibilities:

- The **author owns structure**: containment, sibling order, horizontal versus
  vertical composition, relational spacing, and connections.
- The **layout engine owns geometry**: exact dimensions, channel widths, port
  positions, lane coordinates, and edge polylines.

In this sense, “manual layout” does not mean manual pixels. It means that the
source says which spatial relationships are intentional. The remaining
geometry is derived.

## 2. Two source structures, with different jobs

A Boxz document describes an ordered **layout tree** and an **edge graph** over
the leaves of that tree. The two structures are related but neither is derived
from the other.

```boxz
vbox system "Order processing" {
  hbox ingress "Ingress" {
    node client "Client"
    spring
    node admin "Admin"
  }

  hbox services "Services" {
    node api "API"
    node worker "Worker"
  }
}

edges {
  client -> api
  admin -> worker
  api -> worker
}
```

An `hbox` places its children west to east. A `vbox` places them north to south.
Either kind may contain more boxes, so a small vocabulary produces rows,
columns, grids, and nested system boundaries. Nodes are the visible,
connectable leaves. A container title may make a structural box visible, but
does not turn it into a routing endpoint.

Containers have separate layout and routing roles. Every container composes
its children, but only a **bounded** container creates a routing region. A
title makes its container bounded by default; an explicit attribute may bound
an untitled container or leave a titled layout annotation transparent.

Source order is semantic. It determines sibling order and also provides stable
tie-breaking when routes compete for equivalent choices. Adding an edge may
enlarge some routing space; it cannot cause the engine to exchange the
positions of two nodes.

The edge graph contributes communication structure, not node placement. This
is the core contract behind Boxz's stability.

## 3. Recursive box layout

Layout proceeds recursively in two directions:

1. **Measure bottom-up.** Each element reports the minimum space needed by its
   contents, labels, ports, seams, and boundary channels.
2. **Place top-down.** Each container assigns final rectangles to its children
   and distributes any surplus space.

For an `hbox`, the main-axis minimum is roughly the sum of child widths and
inter-child gaps; its cross-axis minimum is governed by its tallest child. A
`vbox` applies the transposed rule. Nesting composes these local rules without
introducing a global coordinate system into the language.

Nodes have intrinsic dimensions based on their titles. Those dimensions are
lower bounds, not promises. A node can grow when an edge-heavy side needs more
distinct ports. Similarly, a container can grow when its channels or
hierarchy transitions need more parallel tracks.

### Springs express alignment, not distance

A spring is flexible empty space on its containing box's main axis. A node may
also be spring-like, retaining its intrinsic size and absorbing a share of the
surplus. Every spring and spring-like child currently contributes one equal
share.

This makes spacing relational. A trailing spring aligns content to the start;
a leading spring aligns it to the end; a spring between children separates
them. The author does not have to guess that a gap should be 137 pixels wide,
and an enclosing layout can grow without invalidating the intent.

Springs affect placement but do not become obstacles, nodes, or routing
vertices. An internal spring enlarges a seam between siblings. An edge spring
leaves unused alignment space outside the compact routing rectangle, so a
channel does not extend merely because its row or column was aligned within a
larger slot.

## 4. Topological visibility

The tempting rule for a direct edge is geometric: route directly when the
source can “see” the target. Boxz uses a narrower, topological rule instead.
This avoids a circular dependency, because exact geometry depends partly on
how much space routing will require.

Every element exposes a recursive frontier on each side:

```text
             north / south frontier     west / east frontier

hbox         all child frontiers         first / last child only
vbox         first / last child only     all child frontiers
node         the node                    the node
```

For example, suppose two consecutive children of a `vbox` are themselves
complex rows. Every node on the south frontier of the first row can approach
every node on the north frontier of the second row through their shared seam.
The rule continues through arbitrarily deep nesting. The transposed rule
applies between consecutive children of an `hbox`.

Two endpoints receive a direct **seam route** when:

- their lowest common container places them in consecutive child subtrees;
- they appear on the two facing recursive frontiers; and
- any explicit endpoint-side constraints agree with that seam.

This is a form of structural visibility. It deliberately does not grant a
shortcut because two unrelated nodes happen to align after measurement.
Accidental geometry can change as titles and routing demand change; authored
adjacency does not.

## 5. Routing space belongs to the layout tree

Edges that are not eligible for a direct seam route travel through a finite
network of structural routing space. It helps to think of the box tree as also
constructing a road system:

- **Ports** are the legal points where an edge enters or leaves a node.
- **Seams** are the gaps between consecutive children.
- **Seam channels** run through those gaps and belong to the parent container.
- **Edge gutters** provide the corresponding passage before the first child
  and after the last.
- **Channels** run along selected sides of a container. An `hbox` owns north
  and south channels; a `vbox` owns west and east channels.
- **Hierarchy connectors** link a nested container's channels to its parent's
  network.
- **Sibling crossbars** connect compatible boundary networks across the empty
  gap between adjacent subtrees.

Every channel also belongs to a **routing region**. Structural ownership and
region ownership are deliberately different: channels introduced by an
unbounded box remain in its nearest bounded ancestor's region. Its child seam
channels and edge gutters consequently provide topological passage through
transparent layout structure. A bounded box instead starts a new region, so
unrelated routes cannot use the same internal roads.

An edge is allowed in the smallest bounded region containing both endpoints
and in the nested bounded branches that contain either endpoint. It may
therefore enter or leave a group it connects to, but cannot cross an unrelated
group as a shortcut. Parent-owned seam channels remain available immediately
outside such a group.

These corridors are generated from containment and orientation. The router
does not construct a general visibility graph over every empty horizontal or
vertical line in the picture. Nor may a route use an unrelated node as a
transit junction.

This restriction is useful beyond implementation convenience. A route through
structural space tends to explain itself: it leaves a component, follows a
group boundary, crosses a sibling boundary, and descends toward its target.
The possible paths correspond to the composition the author wrote.

Hierarchy connectors are not necessarily fixed to one midpoint. Where two
channel bands overlap, several edges can receive separate connector tracks
within that interval. This preserves the structural choice—leave this child
through this side—without forcing all hierarchy-changing edges onto one
physical line.

## 6. Route intent before exact geometry

Boxz selects between two kinds of route intent:

### Prescribed seam routes

A seam route is chosen by the topological rule above. Its endpoint sides follow
the containing box's axis: south-to-north in a `vbox`, or east-to-west in an
`hbox`, with direction reversed as needed. It is local by construction and
does not enter the outer routing search.

### Searched outer routes

All other edges enter the structural channel graph, filtered to the routing
regions legal for that edge. Candidate endpoint sides
come from the node's immediate parent: children of an `hbox` normally reach
the outer network through north or south, while children of a `vbox` use west
or east. Explicit side annotations narrow those choices.

The search cost is lexicographic:

1. Manhattan length;
2. number of bends;
3. prior use of routing resources.

Length therefore remains the primary meaning of “shortest.” Bend count decides
among equally short routes, and congestion encourages later edges to use an
equivalent less-busy path. Stable adjacency order and source edge order resolve
remaining ties.

The result is still an intent rather than the final polyline. It identifies the
corridors and transitions an edge will use. Exact parallel coordinates are
assigned in a later phase.

## 7. Path selection and track allocation are different problems

Two routes can make sensible topological choices and still produce an
ambiguous drawing if they are painted on the same line. Conversely, two graph
segments may look related to the pathfinder while occupying different physical
space. Boxz therefore distinguishes:

- A **graph resource**, which is an available transition and a unit of
  congestion during path search.
- A **track domain**, which is a physical corridor whose collinear users must
  receive distinct coordinates.

Often one channel is both one graph resource and one track domain. The concepts
separate at hierarchy connectors, sibling crossbars, and prescribed seam
paths. Several graph transitions may occupy the same physical gap; a seam path
may need to reserve a track even though it never participated in graph search.

After route selection, each route declares its use of physical domains. Local
allocators then assign:

- distinct offsets within shared channels;
- ordered tracks within direct seams;
- separate coordinates for movable hierarchy connectors and crossbars; and
- exact, deterministically ordered ports on node sides.

Perpendicular crossings do not contend for a track: they are visually
crossings, not overlaps. Collinear sharing does contend, because two coincident
edges become impossible to trace. When access requirements in a seam cannot be
satisfied by a single ordered bank, a local dogleg and a second bank resolve
the cycle. Only that seam grows.

This decomposition is central. Route search answers _where may the edge go?_
Track allocation answers _how do several chosen routes coexist there?_

## 8. Routing and layout meet at a monotone fixed point

Layout must know how much routing space to reserve. Routing must know where the
layout placed that space. Boxz resolves this apparent cycle with bounded,
monotone feedback:

```text
source
  -> layout topology
  -> [measure and place
      -> select route intents
      -> allocate ports and tracks]*
  -> exact orthogonal polylines
  -> SVG
```

The first measurement uses known and conservative demand. Routing may then
discover that a channel, seam, connector, or node side needs more capacity.
That local allocation becomes a new lower bound for the next measurement.
Capacities only increase; the system does not alternately shrink and regrow the
same space.

Once those bounds stop changing, exact paths are materialized. Final seam paths
are reconstructed from allocated ports, not provisional node centers, so any
alignment jog occurs in routing space rather than along a node border. Lane
offsets and movable connector positions are applied while preserving the
selected route topology. Only then are redundant collinear points removed.

The SVG renderer is intentionally passive. It paints solved node rectangles,
container boundaries, polylines, arrowheads, and labels. It does not allocate
new ports or repair route geometry. Debug rendering exposes the same solution's
channels, connectors, domains, and ports rather than running a different
layout.

## 9. What stability means

Boxz aims for **structural stability**, not bitwise coordinate immobility.

The stable facts are containment, order, orientation, adjacency, legal routing
corridors, and deterministic tie-breaking. Metric facts may change when the
content changes. A longer title can widen a node. A fourth edge can require a
fourth port or lane. A wider local channel can shift descendants.

Those changes are accepted because they preserve the authored relationships.
The router may make room for the graph, but it does not reinterpret the graph
as a reason to reorder the composition.

Edge declaration order is part of this model. Earlier edges reserve preferred
resources before later edges use congestion as a tie-breaker. This is a small,
visible form of author control rather than an unspecified dependency on map
iteration or solver timing.

## 10. Diagram legibility as local invariants

There is no single objective function for a “good” architecture diagram.
Boxz instead builds around a set of composable invariants:

- sibling order is preserved;
- direct structural neighbors can use their shared seam;
- routes enter and leave node sides perpendicularly through allocated ports;
- unrelated routes do not use nodes as transit space;
- unrelated routes do not use bounded containers as transit space;
- transparent layout containers remain permeable through child seams and edge
  gutters;
- collinear users of shared routing space receive distinct tracks;
- perpendicular channel crossings remain legal;
- extra edge capacity grows the smallest responsible layout region; and
- equal inputs and options produce equal output.

These rules do not eliminate every crossing or guarantee a globally minimal
drawing. They make failures local enough to inspect and concepts explicit
enough to debug. The routing gallery serves the same purpose empirically: each
example exercises a difficult structural pattern that humans can recognize at
a glance.

## 11. Relation to other approaches

Boxz combines familiar ideas in a particular division of responsibility. The
comparisons below locate that choice; they are not claims that one family
subsumes another.

| Approach | Primary placement input | Typical automatic work | Relation to Boxz |
| --- | --- | --- | --- |
| Direct-manipulation drawing | Explicit coordinates and paths | Alignment aids, snapping, sometimes connector repair | Boxz retains author control at the relational level but derives pixels and bend points. |
| Layered graph layout | Graph connectivity, directions, and constraints | Ranking, ordering, crossing reduction, coordinates, edge routes | Boxz takes order and nesting from a separate layout tree; connectivity drives routing rather than placement. |
| Obstacle-avoiding connector routing | Placed shapes, ports, and obstacles | Visibility, path search, route improvement, separation of shared paths | Boxz addresses a similar fixed-placement routing problem, but its legal corridors come from box structure rather than arbitrary free-space visibility. |
| Box and flexible-space layout | Ordered nested content with intrinsic sizes and flexible surplus | Measurement and one-dimensional space distribution | Boxz uses the same compositional vocabulary for nodes, then couples it to routing-space demand. |
| Textual diagram systems | Declarative nodes, relations, and diagram-specific hints | Parsing, layout-engine selection, geometry, rendering | Boxz makes coarse spatial composition a first-class part of the source language and intentionally keeps the notation narrow. |

### Layered graph drawing

Graphviz `dot` and ELK Layered belong to the layered or hierarchical family.
They emphasize a common edge direction, assign nodes to layers, order nodes to
reduce crossings, and compute coordinates and edge geometry. ELK Layered also
supports orthogonal routes, ports, compound graphs, and cross-hierarchy edges.
These are close problem domains with a different source of spatial authority:
the attributed graph is primary for layered layout, while Boxz's ordered box
tree is primary for placement. See the [Graphviz `dot`
documentation](https://graphviz.org/docs/layouts/dot/), Gansner et al.'s [“A
Technique for Drawing Directed
Graphs”](https://graphviz.org/documentation/TSE93.pdf), and the [ELK Layered
reference](https://eclipse.dev/elk/reference/algorithms/org-eclipse-elk-layered.html).

### Orthogonal connector routing

Libavoid starts from placed diagram objects and routes connectors around them.
Its orthogonal-routing work describes a three-stage process: construct an
orthogonal visibility graph, find routes through it, then center and nudge
shared segments apart. Boxz has a recognizably similar separation between path
selection and final visual tracks. Its routing graph is instead structural and
finite by construction: channels, seams, hierarchy connectors, and crossbars
are generated by the layout tree. The comparison is especially useful because
it distinguishes the general obstacle-avoidance problem from Boxz's narrower
question of routing through authored composition. See Wybrow, Marriott, and
Stuckey, [“Orthogonal Connector
Routing”](https://people.eng.unimelb.edu.au/pstuckey/papers/gd09.pdf).

### Box, glue, and flexible layout

Recursive horizontal and vertical composition has a long history in document
and interface layout. TeX constructs horizontal and vertical boxes and uses
stretchable glue to reconcile natural and assigned dimensions. CSS Flexbox
similarly distributes free space among items according to growth factors.
Boxz springs belong to this family of ideas, with two diagram-specific twists:
the composed items expose routing frontiers, and routing demand can feed new
minimum sizes back into composition. See Eijkhout's [_TeX by
Topic_](https://mirrors.ctan.org/info/texbytopic/TeXbyTopic.pdf) and the [CSS
Flexible Box Layout specification](https://www.w3.org/TR/css-flexbox-1/).

### Textual diagram systems

Textual tools such as Mermaid separate authored notation from geometry and
rendering, and can delegate a diagram to different layout engines. That makes a
wide range of diagram types and algorithms available behind a common authoring
experience. Boxz makes a different scope choice: one small spatial language for
architecture sketches, with nesting and arrows as its center. The layout tree
is not merely a hint passed to a general engine; it is the persistent spatial
model. See Mermaid's [layout overview](https://mermaid.js.org/config/layouts)
and [layout-maker's guide](https://mermaid.js.org/community/layout-makers-guide.html).

## 12. Boundaries and open questions

The current model intentionally leaves several subjects outside its center:
rich node shapes, UML-specific semantics, decorative styling, and a large
taxonomy of edge types. Those features can be valuable without changing the
layout argument, but they can also obscure it if introduced as routing
concepts.

More structural extensions require care. Connectable containers would make a
box both routing infrastructure and an endpoint. Edge labels would introduce
objects whose placement may compete with tracks. Multiline node labels would
change intrinsic measurement. Each is feasible, but each should state whether
it participates in topology, measurement, route selection, physical
allocation, or only rendering.

That stage discipline is the broader design principle: visual features stay
visual; physical conflicts enter allocation; only authored spatial relations
enter topology.

## Conclusion

Boxz treats an architecture diagram as an authored composition carrying an
automatically routed graph. Horizontal and vertical boxes preserve the
author's story. Recursive frontiers turn nesting into stable adjacency.
Structural channels define where non-local edges may travel. Separate track
allocation keeps independently selected routes visually distinct. A monotone
feedback loop lets routing claim the space it needs without becoming a global
placement optimizer.

The result is neither a coordinate file nor a graph whose composition is
inferred after the fact. It is a compact relational layout whose metric details
can be recomputed while its spatial intent remains explicit.

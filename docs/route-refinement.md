# Route refinement

Boxz first chooses legal route intents and allocates their ports and physical
tracks. Those operations can introduce small metric artifacts: a lane shifted
away from its preferred coordinate needs a shoulder, and several individually
reasonable shoulders can form a needlessly stepped polyline. Route refinement
removes such artifacts after layout and routing have converged.

Refinement is deliberately not another router. It does not move nodes, change
ports, choose different routing regions, or request less layout space. It only
replaces a short span of an exact display polyline with simpler orthogonal
geometry.

## Phase boundary

The complete pipeline is:

```text
source -> topology and layout
       -> route intents and track allocation
       -> exact display polylines
       -> local route refinement
       -> SVG
```

The route intent, graph resources, and track domains remain unchanged for
debugging and routing-space accounting. Only `routedEdge.Display` is refined.
Unused space is not reclaimed, so the monotone layout fixed point remains
independent of aesthetic cleanup.

The phase is shared by the built-in and experimental libavoid routers because
it operates on their common exact-polyline representation.

## Rules and candidate bounds

Rules are built into Boxz rather than supplied by diagram authors. Each rule
examines a bounded window of an existing route and returns a small, ordered set
of plausible replacements. It does not enumerate combinations of rewrites or
search arbitrary coordinates.

The initial rule examines at most five consecutive segments. Between the
window endpoints it tries only the one or two minimum-length Manhattan paths,
whose coordinates come from existing vertices. This catches local stairs,
reversals, and rectangular excursions with a constant number of candidates per
window.

After the best legal improvement is applied, only that route is rescanned.
Routes are processed in source order. A rewrite must strictly reduce the
lexicographic score `(bend count, length)`, which guarantees termination and
stable output.

## Shared legality check

Rules propose geometry, but one internal predicate owns the scene-dependent
conditions for applying it. A replacement must:

- preserve both endpoint ports and their perpendicular departure directions;
- never shorten the existing first or final endpoint leg;
- preserve the ordered crossing points of every bounded-container boundary;
- remain orthogonal and outside node interiors;
- avoid self-intersection;
- avoid collinear overlap, near-coincident parallel runs, and false junctions
  with other routes; and
- introduce only proper interior-to-interior perpendicular crossings.

Parallel routes must remain at least one configured lane spacing apart while
their longitudinal spans overlap. Collinear intervals are closed for conflict
purposes: if one route occupies the upper portion of a line and another
occupies the lower portion, sharing a coordinate is legal only when a full lane
spacing remains between them. Merely meeting at one endpoint would look like
an electrical junction and is rejected.

This predicate is intentionally smaller than a full diagram validator. It
checks the proposed exact polyline against the solved scene; broader invariants
remain asserted by the test suite and visual routing gallery.

## Adding a rule

A new rule should describe one recognizable local defect and emit a constant
number of deterministic candidates. It should rely on the shared legality
check rather than reproducing collision or boundary semantics. A focused unit
test should demonstrate both the intended rewrite and the nearest case that
must remain unchanged, followed by the full routing-gallery render.

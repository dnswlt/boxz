# boxz-avoid

An experimental orthogonal edge router for boxz, backed by
[libavoid](https://github.com/mjwybrow/adaptagrams) and driven over a pipe.
Boxz places the boxes and hands the rectangles over; only routing leaves Go.

## Why a separate process

- **Licensing.** Adaptagrams is LGPL-2.1. Only boxz-avoid links it, not boxz.
- **Build.** No C++ toolchain, no cgo, no CMake needed for `go build ./...`.
- **Testability.** It is a filter: `echo '{...}' | boxz-avoid`.

## Layout

```text
avoidrouter/
  CMakeLists.txt      pins and builds libavoid, then boxz-avoid
  src/protocol.*      wire types, parsing, validation
  src/route.*         the libavoid driver
  src/main.cpp        the JSON Lines loop
  testdata/           example requests, runnable as-is
  build/              ignored; also where dependencies are fetched
```

Dependencies are neither vendored nor submodules: `CMakeLists.txt` pins
adaptagrams by commit and nlohmann/json by release hash, and CMake fetches both
into `build/_deps`. Third-party source and its licences stay out of the repo,
versions stay pinned, and checkouts that never build this need no submodule.

libavoid alone is self-contained C++, so `CMakeLists.txt` compiles its 23
sources directly instead of driving adaptagrams' autotools build.

## Build

```sh
cmake -S avoidrouter -B avoidrouter/build
cmake --build avoidrouter/build -j
./avoidrouter/build/boxz-avoid < avoidrouter/testdata/example.jsonl
```

Offline, point CMake at an existing checkout with
`-DFETCHCONTENT_SOURCE_DIR_ADAPTAGRAMS=/path/to/adaptagrams`.

## Transport

JSON Lines: one compact request per line on stdin, one response per line on
stdout, in order. EOF exits 0. Requests must contain no literal newline.

Requests are independent — a fresh `Avoid::Router` per request, no state between
them — so a warm process and a cold one give the same answer. A bad request
produces an error response; nothing in a request kills the process. libavoid
writes diagnostics to stderr; stdout carries only protocol.

## Coordinates

SVG convention: x grows east, y grows south. libavoid agrees — its
`ConnDirDown` is `+y` despite the name — so nothing is transformed anywhere.

## Request

```jsonc
{
  "version": 1,
  "id": "optional; echoed back on the response",

  "options": {
    "routingType": "orthogonal",   // or "polyline"
    "shapeBufferDistance": 8,      // clearance kept around obstacles
    "idealNudgingDistance": 8,     // separation between parallel routes
    "segmentPenalty": 50,          // cost per bend; the main straightness knob
    "anglePenalty": 0,
    "crossingPenalty": 0,
    "clusterCrossingPenalty": 4000,
    "fixedSharedPathPenalty": 0,
    "portDirectionPenalty": 100,
    "reverseDirectionPenalty": 0,
    "nudgeOrthogonalSegmentsConnectedToShapes": true,
    "nudgeSharedPathsWithCommonEndPoint": true,
    "nudgeOrthogonalTouchingColinearSegments": false,
    "performUnifyingNudgingPreprocessingStep": true,
    "penaliseOrthogonalSharedPathsAtConnEnds": false,
    "improveHyperedgeRoutesMovingJunctions": false
  },

  "obstacles": [
    {
      "id": "api",
      "rect": {"x": 0, "y": 200, "w": 100, "h": 40},
      "ports": [
        {"id": "n0", "side": "north", "pos": 0.33},
        {"id": "s0", "side": "south", "pos": 0.5}
      ],
      "exclusivePorts": true
    }
  ],

  "clusters": [ {"id": "group", "rect": {"x": 0, "y": 0, "w": 10, "h": 10}} ],

  "edges": [
    {
      "id": "e0",
      "from": {"obstacle": "api"},
      "to": {"obstacle": "db", "ports": ["w0", "w1"]},
      "checkpoints": [{"x": 120, "y": 300}]
    }
  ]
}
```

Only `id`/`rect` on obstacles and `id`/`from`/`to` on edges are required;
omitted options keep libavoid's defaults. Unknown fields are ignored, so
additive changes need no version bump. `version` must be `1`.

Obstacle ids, port ids within an obstacle, and edge ids must each be unique.

### Ports

An obstacle declares the ports it is willing to be entered through; libavoid
picks the side and the port. `pos` is the fraction along that side, west-to-east
on north/south and north-to-south on east/west.

`exclusivePorts` (default `true`) gives each port to at most one edge. Declare
at least as many ports as the side will carry: an edge that finds no free pin
attaches to the shape **centre**, putting the endpoint inside the obstacle. With
`false`, edges share pins and nudging separates them.

Endpoint forms, checked in this order:

| Form | Meaning |
| --- | --- |
| `{"point": {...}, "dirs": ["west"]}` | Fixed coordinate; `dirs` restricts approach, omitted means all four. |
| `{"obstacle": "api", "ports": ["n0","n1"]}` | Any port in that subset — how a side constraint is expressed. |
| `{"obstacle": "api"}` | Any declared port, or the obstacle's centre if it declares none. |

There is no `side` field on an endpoint: naming eligible port ids is more
general and keeps the router free of boxz's port conventions.

## Response

```jsonc
{
  "version": 1,
  "id": "echoed from the request",
  "routes": [
    {
      "id": "e0",
      "points": [{"x": 66, "y": 40}, {"x": 66, "y": 200}],
      "from": {"obstacle": "client", "port": "s1", "side": "south",
               "point": {"x": 66, "y": 40}},
      "to":   {"obstacle": "api", "port": "n1", "side": "north",
               "point": {"x": 66, "y": 200}}
    }
  ],
  "warnings": ["..."],
  "stats": {"elapsedMs": 0.21, "obstacles": 3, "edges": 3}
}
```

`routes` is in request order, one entry per edge. `from`/`to` report the port
libavoid chose, which the caller needs precisely because it did not choose it.
`port` and `side` identify the pin attached to; `point` is the display endpoint,
which nudging may have moved along the border away from that pin.

An error response replaces `routes`:

```json
{"version": 1, "id": "e0", "error": {"code": "unknown_obstacle", "message": "..."}}
```

Codes: `invalid_json`, `bad_request`, `unsupported_version`, `unknown_obstacle`,
`unknown_port`, `router_error`.

## libavoid behaviour worth knowing

- **`ConnDirDown` is `+y`.** No coordinate transformation needed.
- **No determinism guarantee.** Exclusive pins gave two different results for
  identical input at ~1-in-8, in both port choice and coordinates: contended
  pins are arbitrated in pointer order, which moves with ASLR. Non-exclusive
  pins and no pins were stable in every run measured. No nudging option changes
  either result.
- **Clusters do nothing under orthogonal routing.** `clusterCrossingPenalty` had
  no effect at any value, matching the `XXX: Clustered routing doesn't yet work
  with orthogonal connectors` comment in `makepath.cpp`. The protocol accepts
  `clusters` and warns rather than pretending.
- **The centre pin is not implicit.** `ConnEnd(shape, CONNECTIONPIN_CENTRE)`
  needs a centre `ShapeConnectionPin` first, or the endpoint silently loses
  visibility.
- **Routes leave the bounding box,** through negative coordinates if that is
  shortest. Nothing confines them to the diagram.
- **Nudging moves endpoints.** `nudgeOrthogonalSegmentsConnectedToShapes`
  separates shared corridors by sliding the shape-attached segment, dragging the
  endpoint along the border. Off keeps ports exact but reintroduces overlaps.
- **Crossed seam edges can overlap.** Two edges crossing one seam leave ~8px of
  shared stub. No option fixes it; nor does space (seam 48→96px, channels
  28→44px, no change).

## How boxz uses it

`solveWithAvoid` in [`../avoid.go`](../avoid.go) cuts between placement and
routing: the first point where coordinates exist, the last before boxz's own
router would answer.

Layout runs once — the fixed point in `solve` only grows outer channel bands for
boxz's lane model, and libavoid does not route in channels. Node sizes and
sibling seams come from the topological routing plan. Edges then go in one
request, with every measured candidate port offered non-exclusively. Boxz adds
four obstacles framing the canvas, since libavoid will not stay inside it.

The sidecar currently treats only nodes as obstacles. It does not implement the
built-in router's endpoint-scoped bounded regions: making a whole group a
libavoid obstacle would also trap edges whose endpoints are inside it. Router
comparisons involving bounded containers should therefore be read with that
semantic difference in mind.

### What the ports actually buy

The pin positions boxz sends look more authoritative than they are. Measured on
a node spanning x 200–320, three declared south ports, four edges:

| pins | endpoints |
| --- | --- |
| none | all at the shape centre, *inside* the node |
| exclusive | declared positions exactly, until pins run out; surplus edges fall back to the centre |
| **non-exclusive** (boxz) | all four picked one pin (x=260); nudging fanned them to 248 / 256 / 264 / 272 |
| non-exclusive, no nudging | all four stacked at x=260 |

Nudging sets the final coordinate, on a free continuum along the border. The
pins decide only the **set of eligible sides**, plus how many attachments a side
should expect — one pin per anticipated edge still beats one pin per side (12 of
17 examples byte-identical; N pins 0.9% shorter). So:

- Endpoints stay within the node. 8 edges on a 72px side pulled hard left
  spanned 400–456 inside 400–472.
- Explicit `a:N` constraints hold, since a port subset restricts the side.
- **Declaration order does not.** That 8-edge test attached at 400, 408, 416,
  424, **456**, 432, 440, 448.

Exclusive pins would restore the ordering rule but cost determinism, route
quality (non-exclusive measured 1.9% shorter, 13% fewer bends), and require
knowing a side's degree before routing has picked sides.

### Container labels

Containers are not sent; only leaf nodes are obstacles.

Sending the label **strip** was tried and removed: it spans nearly the full
container width, so it walls off every container — `12-container-labels` got 62%
longer. Boxz's display pass already sweeps each label to the quietest position
in its strip, moving the label instead of the routes.

Sending the **label** itself, the text rectangle, is a different and untried
idea. It needs the auto-placement sweep off first: a position chosen after
routing cannot be an obstacle during it, so only explicit `left`, `center`, and
`right` give a rectangle that exists beforehand.

### Comparison with the built-in router

15 gallery examples, identical node placement:

| | total length | total bends |
| --- | --- | --- |
| built-in | 17767 | 134 |
| libavoid | 15958 (**-10.2%**) | 90 (**-32.8%**) |

Nothing is longer or bendier under libavoid. Largest gains:
`14-hierarchy-riser-lanes` (-38%), `15-seam-connector-sharing` (-18%),
`12-container-labels` (-15%). It loses only on the crossed-seam overlap.

```sh
./scripts/compare-routers.sh          # side-by-side page in .gallery-compare
BOXZ_ROUTER=avoid ./scripts/render-gallery.sh
```

## Known limitations

- Output is stable in practice but not guaranteed;
  `TestAvoidRenderIsStableAcrossRuns` is a canary, not a proof. Boxz's "equal
  inputs, equal output" invariant is weaker on this path.
- Crossed seam edges overlap, tracked as a known exception in
  `TestAvoidGalleryHoldsRoutingInvariants`, which fails if the list goes stale
  either way.
- One obstacle used with several *different* eligible-port sets gets a pin class
  per set, so a shared port gets coincident pins and exclusivity holds only
  within a class. Boxz can sidestep it with disjoint sets. No caller hits it.
- Hyperedges, junctions, and non-rectangular obstacles are not exposed.
- The router has no timeout of its own. `Client.Route` takes a context and kills
  the process when it ends; boxz bounds each render with `Config.AvoidTimeout`
  (default 30s). libavoid's `shouldContinueTransactionWithProgress` hook is not
  used: it is polled per connector and per phase, not inside one path search, so
  it cannot bound a stall. The kill reaches only the router process, not
  anything a wrapper script started.

## Go client

[`internal/avoid`](../internal/avoid) mirrors these types and manages the
subprocess. `avoid.FindBinary` checks `$BOXZ_AVOID_BIN`, then
`avoidrouter/build/boxz-avoid`, then `PATH`. Its tests skip when the binary is
absent.

## Licence

boxz-avoid is MIT. It links libavoid (LGPL-2.1-or-later) and nlohmann/json
(MIT), which CMake fetches at build time; neither is in this repository.

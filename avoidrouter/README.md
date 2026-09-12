# boxz-avoid

An experimental orthogonal edge router for boxz, backed by
[libavoid](https://github.com/mjwybrow/adaptagrams) and driven over a pipe.

Boxz places nodes and routes edges itself. This binary is a second opinion on
the routing half only: boxz lays out the boxes, hands the resulting rectangles
to `boxz-avoid`, and draws the polylines that come back. Node placement never
leaves Go.

## Why a separate process

- **Licensing.** Adaptagrams is LGPL-2.1. A separate executable communicating
  over a pipe keeps that obligation off the boxz binary; linking libavoid into
  the Go program would not.
- **Build.** `go build ./...` and `go test ./...` keep working with no C++
  toolchain, no cgo, and no CMake. Everything here is optional.
- **Testability.** The router is a filter. `echo '{...}' | boxz-avoid` is the
  whole debugging story, and the Go tests skip when the binary is absent.

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

Dependencies are **not** vendored and **not** git submodules. `CMakeLists.txt`
pins adaptagrams by commit and nlohmann/json by release hash, and CMake fetches
both into `build/_deps` on first configure. That keeps third-party source and
its licences out of the repository while still pinning exact versions, and it
avoids `git clone --recursive` ceremony for a component most checkouts never
build.

Adaptagrams ships an autotools build for the whole suite, but libavoid alone is
self-contained C++ with no external dependencies, so `CMakeLists.txt` compiles
its 23 source files directly rather than driving `configure` for one
sub-library.

## Build

```sh
cmake -S avoidrouter -B avoidrouter/build
cmake --build avoidrouter/build -j
```

The binary lands at `avoidrouter/build/boxz-avoid`. For an offline build, point
CMake at an existing checkout:

```sh
cmake -S avoidrouter -B avoidrouter/build \
  -DFETCHCONTENT_SOURCE_DIR_ADAPTAGRAMS=/path/to/adaptagrams
```

Try it:

```sh
./avoidrouter/build/boxz-avoid < avoidrouter/testdata/example.jsonl
```

## Transport

JSON Lines: one compact request object per line on stdin, one response object
per line on stdout, in order. EOF on stdin exits 0.

Requests must not contain a literal newline. One-shot use is a single line;
boxz may also keep one warm process and send many requests, since startup is
the only cost that saves. Requests are independent — the router builds a fresh
`Avoid::Router` per request and keeps no state between them — so a warm process
and a cold one produce identical output.

A malformed or invalid request produces an error response and the process
carries on. Nothing a request can contain kills the router.

libavoid writes some diagnostics directly to stderr. stdout carries only
protocol.

## Coordinates

SVG convention: x grows east, y grows south. libavoid uses the same convention
— its `ConnDirDown` is the `+y` direction, despite the screen-flavoured name —
so boxz coordinates pass through unchanged in both directions. There is no axis
flip anywhere in this component.

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

Every field except `edges` entries' `id`/`from`/`to`, and obstacles' `id`/`rect`,
is optional. Omitted options keep libavoid's defaults. Unknown fields are
ignored, so additive protocol changes need no version bump; `version` is
rejected when it is not `1`.

Obstacle ids, port ids within an obstacle, and edge ids must each be unique.

### Ports are candidates, not assignments

This is the part that differs most from boxz's own router, and the reason the
integration is interesting.

An obstacle declares the ports it is *willing* to be entered through. libavoid
then chooses, per edge, which side to use and which port on it. `pos` is the
fraction along that side, measured west-to-east on north/south and
north-to-south on east/west.

`exclusivePorts` (default `true`) gives each port to at most one edge, so
parallel edges between the same pair of nodes land on distinct ports instead of
stacking.

An edge endpoint takes one of three forms, checked in this order:

| Form | Meaning |
| --- | --- |
| `{"point": {...}, "dirs": ["west"]}` | A fixed coordinate. `dirs` restricts approach directions; omitted means all four. |
| `{"obstacle": "api", "ports": ["n0","n1"]}` | Any port in that named subset — this is how a side constraint is expressed. |
| `{"obstacle": "api"}` | Any declared port; or the obstacle's centre if it declares none. |

The protocol has no `side` field on an endpoint. Boxz already knows which of
its ports sit on which side, so naming the eligible port ids is strictly more
general and keeps the router free of boxz's port-numbering conventions.

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

`routes` is in request order and has one entry per edge. `from`/`to` report
which port libavoid picked, which boxz needs precisely because it did not
choose it — for arrowheads, for debug rendering, and for deciding whether the
choice was reasonable.

An error response replaces `routes` entirely:

```json
{"version": 1, "id": "e0", "error": {"code": "unknown_obstacle", "message": "..."}}
```

Codes: `invalid_json`, `bad_request`, `unsupported_version`, `unknown_obstacle`,
`unknown_port`, `router_error`.

## What we learned about libavoid

Findings from probing the pinned version, recorded here so the next person does
not rediscover them:

- **`ConnDirDown` is `+y`.** Verified against a routed obstacle. Boxz's SVG
  coordinates need no transformation.
- **Clusters do not work under orthogonal routing.** libavoid's
  `clusterCrossingPenalty` had no observable effect on orthogonal routes at any
  penalty value, matching the `XXX: Clustered routing doesn't yet work with
  orthogonal connectors` comment in `makepath.cpp`. The protocol accepts
  `clusters` and the router emits a warning rather than pretending. **Titled
  containers therefore cannot be expressed as soft regions** — see below.
- **The centre pin is not implicit.** `ConnEnd(shape, CONNECTIONPIN_CENTRE)`
  needs a `ShapeConnectionPin` at the centre to have been created first, or
  libavoid warns and silently drops the endpoint's visibility.
- **Routes leave the bounding box.** Given a constrained port on the far side
  from the target, libavoid happily routes through negative coordinates.
  Nothing bounds a route to the diagram, so boxz must either add a frame of
  obstacles or grow the viewBox to whatever comes back.
- **Output is deterministic**, given a fixed insertion order, which the router
  guarantees by creating shapes, clusters, and connectors in request order with
  sequential ids.

## Container labels

Boxz's titled containers are structural, not connectable, so they are not
obstacles in the usual sense — but a route through a label strip is
unreadable. With clusters unavailable, the options the protocol supports are:

1. **Ignore containers.** Send only leaf nodes. Simplest, and routes may cross
   labels.
2. **Send the label strip as an obstacle.** A plain entry in `obstacles` with no
   ports. Routes then detour around the text but may still cross the container
   boundary anywhere else — which is arguably what boxz wants, since a boundary
   crossing is legal and a struck-through label is not.
   `testdata/example.jsonl` has a `label-obstacle` case demonstrating this.
3. **Send the whole container as an obstacle.** Only coherent when nothing
   inside it is an endpoint, so not generally useful.

Option 2 looks like the right default. The choice stays in Go: this binary is a
policy-free libavoid driver, and every boxz-specific decision arrives already
resolved.

## Known limitations

- When one obstacle is used with several *different* eligible-port sets, each
  distinct set becomes its own libavoid pin class, and a port appearing in more
  than one set gets a coincident pin per class. Exclusivity is then only
  guaranteed within a class, so two edges could still share a coordinate. Boxz
  can sidestep it by emitting disjoint port sets. Not hit by any current caller.
- Hyperedges, junctions, and non-rectangular obstacles are not exposed.
- There is no timeout. A pathological diagram would hang the router, so the Go
  side should own a deadline.

## Go client

[`internal/avoid`](../internal/avoid) mirrors these types and manages the
subprocess. `avoid.FindBinary` checks `$BOXZ_AVOID_BIN`, then
`avoidrouter/build/boxz-avoid`, then `PATH`. Its tests skip when the binary has
not been built.

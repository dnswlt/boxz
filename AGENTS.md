# Agent guide

Core principles:

- Nodes are laid out manually; edges are routed automatically.
- Boxz targets system-design and architecture sketches.
- Nested boxes and arrows are the scope—not UML-level visual notation.
- Source order and deterministic output matter; routing must not rearrange nodes.

Start with [README.md](README.md), then use:

- [docs/concepts.md](docs/concepts.md) for the design model and related work;
- [docs/language.md](docs/language.md) for source syntax;
- [docs/springs.md](docs/springs.md) for relational sizing;
- [docs/container-labels.md](docs/container-labels.md) for visible groups;
- [docs/route-refinement.md](docs/route-refinement.md) for post-routing cleanup;
- [docs/architecture.md](docs/architecture.md) for implementation concepts;
- [examples/gallery/README.md](examples/gallery/README.md) for visual checks;
- [avoidrouter/README.md](avoidrouter/README.md) for the experimental
  libavoid edge router, an optional C++ sidecar process.

Run `go test ./...` and `./scripts/render-gallery.sh` after layout or routing
changes. `./scripts/compare-routers.sh` renders the gallery under both edge
routers side by side when the optional `boxz-avoid` binary is built.

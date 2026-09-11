# Agent guide

Core principles:

- Nodes are laid out manually; edges are routed automatically.
- Boxz targets system-design and architecture sketches.
- Nested boxes and arrows are the scope—not UML-level visual notation.
- Source order and deterministic output matter; routing must not rearrange nodes.

Start with [README.md](README.md), then use:

- [docs/language.md](docs/language.md) for source syntax;
- [docs/springs.md](docs/springs.md) for relational sizing;
- [docs/architecture.md](docs/architecture.md) for implementation concepts;
- [examples/gallery/README.md](examples/gallery/README.md) for visual checks.

Run `go test ./...` and `./scripts/render-gallery.sh` after layout or routing
changes.

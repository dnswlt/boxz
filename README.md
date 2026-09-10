# boxz

`boxz` renders deterministic, manually structured graph layouts as SVG. Nodes are
placed by nesting ordered horizontal and vertical boxes; edges are routed through
orthogonal seams between neighbors or through channels around those boxes.

```boxz
vbox root {
  hbox services {
    node client "Client"
    node api "API Server"
  }

  node database
}

edges {
  client -> api
  api -> database
}
```

Render it with:

```sh
go run ./cmd/boxz -o diagram.svg examples/basic.boxz
```

Add `-debug` to draw the otherwise invisible layout and routing machinery:

```sh
go run ./cmd/boxz -debug -o diagram.svg examples/basic.boxz
```

Debug SVGs show dashed container bounds, channel centerlines, hierarchy risers,
sibling crossbars, and allocated node ports. Debug mode does not change layout
or edge routing.

For visual review after routing or layout changes, render the focused example
gallery and open the reported HTML file:

```sh
./scripts/render-gallery.sh
```

See [examples/gallery/README.md](examples/gallery/README.md) for the scenarios
and their visual acceptance criteria.

## Language

There must be exactly one root element, followed by an optional `edges` block.
Source order is layout and edge-routing order.

```text
document  = element edges?
element   = node | hbox | vbox
node      = "node" identifier string?
hbox      = "hbox" identifier "{" element+ "}"
vbox      = "vbox" identifier "{" element+ "}"
edges     = "edges" "{" edge* "}"
edge      = endpoint "->" endpoint
endpoint  = identifier (":" side)?
side      = "N" | "E" | "S" | "W"
```

Element IDs are globally unique. A node without a title displays its ID. An
endpoint such as `api:E` constrains the router to that side.

Adjacent children share a routing seam. A `vbox` seam connects the recursively
exposed south nodes of its upper child to the north nodes of its lower child. An
`hbox` seam similarly connects east to west. Every exposed node on one side can
reach every exposed node on the other, even through nested containers. Seam
routing takes precedence over outer channels and fixes the endpoint sides before
geometry is calculated.

Within each seam, exact port alignment determines track order so opposing
access legs do not overlap. Cyclic ordering constraints are resolved locally
with doglegs and additional seam tracks; nodes remain fixed.

For other edges, an `hbox` owns north and south outer channels while a `vbox`
owns west and east outer channels. Nested containers connect to their parents
through orthogonal risers. Facing channel networks of adjacent siblings are
connected by local crossbars inside their empty gap. These routes minimize
geometric distance, then bends, then prior routing-resource use. Fixed side and
adjacency ordering break remaining ties; edge declaration order determines
which routes contribute prior use. Within a sibling gap, crossbars avoid the
exact access coordinates reserved by direct seam routes.

Containers are structural and are only drawn as light dashed boundaries in
debug mode.

Go-style line and block comments are accepted.

See [docs/architecture.md](docs/architecture.md) for the layout and routing
model, phase boundaries, and invariants.

## Current scope

The first version deliberately uses conservative fixed-width title measurement.
It supports directed edges and automatically allocates multiple ports and channel
lanes. Self-edges, edge labels, container labels, and styling syntax are not yet
implemented.

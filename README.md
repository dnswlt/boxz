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
through orthogonal risers. These routes minimize geometric distance, then bends,
then prior channel use. Fixed side and adjacency ordering break remaining ties;
edge declaration order determines which routes contribute prior channel use.

Containers are structural, but are currently drawn with light dashed boundaries
to make the layout and routing topology easier to debug.

Go-style line and block comments are accepted.

See [docs/architecture.md](docs/architecture.md) for the layout and routing
model, phase boundaries, and invariants.

## Current scope

The first version deliberately uses conservative fixed-width title measurement.
It supports directed edges and automatically allocates multiple ports and channel
lanes. Self-edges, edge labels, container labels, and styling syntax are not yet
implemented.

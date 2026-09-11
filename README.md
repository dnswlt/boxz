# boxz

`boxz` renders system-design sketches as SVG. You place nodes manually by
nesting ordered horizontal and vertical boxes; boxz routes the edges
automatically.

Where Mermaid and Graphviz normally derive placement from connectivity, boxz is
also told the intended coarse layout. That makes diagrams easier to direct and
keeps them stable when the graph changes: the router may enlarge nodes and
routing space, but it never rearranges the source tree.

Boxz is deliberately smaller in scope than UML or a general visual modeling
language. It is for architecture sketches made from nested boxes and arrows,
not for accumulating every possible diagram shape and notation.

```boxz
vbox system {
  hbox services {
    node client "Client"
    node api "API Server"
  }

  hbox storage {
    node database "Database"
    node cache "Cache"
  }
}

edges {
  client -> api
  api -> database
  api -> cache
}
```

## Run it

Render a source file with the Go command:

```sh
go run ./cmd/boxz -o diagram.svg examples/basic.boxz
```

Use `-debug` to include the otherwise invisible container boundaries, channels,
hierarchy risers, sibling crossbars, and allocated ports:

```sh
go run ./cmd/boxz -debug -o diagram.svg examples/basic.boxz
```

Debug output changes only the visualization, not layout or routing.

## Documentation

- [Language reference](docs/language.md): nodes, boxes, edges, attributes, and
  the complete grammar.
- [Springs](docs/springs.md): relational spacing, alignment, and expandable
  nodes.
- [Architecture](docs/architecture.md): layout and routing internals, phase
  boundaries, and invariants.
- [Visual routing gallery](examples/gallery/README.md): focused examples for
  inspecting difficult routing patterns.

Render the complete gallery with:

```sh
./scripts/render-gallery.sh
```

## Current scope

Boxz currently has directed edges, conservative single-line title measurement,
automatic ports, orthogonal routing, and relational spacing. Self-edges, edge
labels, container labels, styling syntax, and attributes on edges or containers
are not implemented yet.

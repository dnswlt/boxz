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
vbox system "Order System" {
  hbox services "Services" {
    node client "Client"
    node api "API Server"
  }

  hbox storage "Storage" [labelAlign = right] {
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

Use `-debug` to include all structural container boundaries, channels,
hierarchy risers, sibling crossbars, and allocated ports:

```sh
go run ./cmd/boxz -debug -o diagram.svg examples/basic.boxz
```

Debug output changes only the visualization, not layout or routing.

## Documentation

- [Concepts](docs/concepts.md): the high-level model for authored node layout,
  automatic edge routing, stability, and related work.
- [Language reference](docs/language.md): nodes, boxes, edges, attributes, and
  the complete grammar.
- [Springs](docs/springs.md): relational spacing, alignment, and expandable
  nodes.
- [Container labels](docs/container-labels.md): visible group boundaries,
  label strips, alignment, and automatic placement.
- [Architecture](docs/architecture.md): layout and routing internals, phase
  boundaries, and invariants.
- [Visual routing gallery](examples/gallery/README.md): focused examples for
  inspecting difficult routing patterns.

Render the complete gallery with:

```sh
./scripts/render-gallery.sh
```

## Current scope

Boxz currently has directed edges, conservative single-line node-title
measurement, titled container boundaries, automatic orthogonal routing, and
relational spacing. Self-edges, edge labels, multiline titles, styling syntax,
and connectable containers are not implemented yet.

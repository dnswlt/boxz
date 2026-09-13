# Library API

Boxz can be embedded in programs that parse, inspect, transform, persist, and
render diagrams. The package owns the authored layout, the concrete ordered
edge set, and its rendering; callers decide what node IDs and edges mean in
their own domain.

## One document, two inputs, two outputs

`Parse` turns source text into an opaque `Document`. API transformations return
another valid document. Both `RenderSVG` and `Format` consume that same model:

```text
.boxz source -> Parse -> Document -> WithEdges -> Document'
                                             |-> RenderSVG -> SVG
                                             `-> Format    -> .boxz source
```

Rendering never serializes or reparses the document. `Format` exists only to
produce the persistent, canonical source artifact.

The representation inside `Document` is private. Callers can name and pass the
exported type, but cannot mutate its layout tree or edge storage directly. A
zero `Document` is harmless but uninitialized; operations that consume it
return an error.

## Reading a document

`Nodes` returns node snapshots in layout-tree order. `Edges` returns edge
snapshots in declaration order. The returned slices, values, and optional side
pointers are copies and may be changed without affecting the document.

```go
nodes := doc.Nodes()
edges := doc.Edges()
```

Node IDs are application-defined strings. Applications can use their own domain
identifiers directly, quoting them with backticks in source when they are not
plain identifiers. Boxz only requires IDs to be unique within the document; it
does not interpret what they refer to.

## Replacing selected edges

`WithEdges` atomically replaces the complete edge list and returns a new
document. The receiver remains unchanged. Order is retained because edge
declaration order is a deterministic routing input.

```go
edges := doc.Edges()
edges = append(edges, boxz.Edge{From: "api", To: "database"})

next, err := doc.WithEdges(edges)
```

The operation rejects unknown or container endpoints, self-edges, invalid
sides, and side constraints unavailable in the layout topology. Failed updates
do not produce a partially modified document.

Duplicate edges are retained. Whether several external relationships between
the same endpoint pair should collapse into one drawn edge is an application
decision.

## Derived edge sets

An embedding application may derive edges from another model, a database, or
user interaction. Boxz accepts the concrete result:

1. Read the available node IDs from `doc.Nodes()`.
2. Produce an ordered `[]boxz.Edge` using application-specific rules.
3. Create a validated document with `WithEdges`.
4. Render that document and, when needed, persist it with `Format`.

Discovery, filtering, and query semantics remain outside Boxz. Once supplied,
the edges are ordinary document data, so formatted source remains
self-contained and independently renderable.

## Canonical source

`Format` preserves layout-tree order, spring positions, and edge order. It emits
canonical indentation, quoting, and attribute spelling. Comments and incidental
whitespace are not retained. Formatting is idempotent: parsing and formatting
the result again produces the same source.

A programmatic layout builder is intentionally deferred. Text-parsed documents
and future API-built documents will converge on the same opaque representation
and use the same transformations, formatter, and renderer.

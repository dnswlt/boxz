# Language reference

A boxz document contains exactly one root element followed by an optional
`edges` block. Source order determines both layout order and edge-routing order.

## Elements

Every element has a globally unique identifier. A node is visible and
connectable; its optional quoted title is displayed inside it. When the title
is absent or empty, the identifier is displayed.

```boxz
node database
node api "API Server"
```

An `hbox` arranges its contents from west to east. A `vbox` arranges them from
north to south. Containers must contain at least one actual element and may be
nested freely. An optional quoted title makes the container boundary and its
top-aligned label visible; an untitled container remains structural and is
visible only in debug output.

```boxz
vbox system "System" {
  hbox services "Services" {
    node client
    node api "API Server"
  }
  node database
}
```

A container may also contain standalone `spring` items for relational spacing
and alignment. See [Springs](springs.md) for their behavior.

Container titles reserve a fixed-height strip but never establish the
container's minimum width. Labels that do not fit are truncated. Routing does
not treat labels as obstacles; automatic horizontal placement instead chooses
a position that covers as few final route segments as possible. See
[Container labels](container-labels.md) for the complete behavior.

## Edges

Edges are declared after the root and currently connect nodes only. They are
directed from left to right in the declaration:

```boxz
edges {
  client -> api
  api -> database
}
```

The router normally chooses endpoint sides. Appending `:N`, `:E`, `:S`, or `:W`
constrains one endpoint; side names are case-insensitive. A constraint that the
layout topology cannot provide is an error.

```boxz
edges {
  client:S -> database:N
}
```

Self-edges are not supported yet. Edge declaration order is significant:
earlier outer routes contribute to the congestion preference used for later
ones.

## Attributes

Attributes follow an element title in square brackets. A trailing comma is
allowed. The syntax accepts bare flags and scalar string, signed-number, or
identifier values so future attributes can use appropriate types.

```boxz
node worker "Worker" [spring]
node cache "Cache" [spring = true,]
hbox workers "Workers" [labelAlign = center] { node worker }
```

The defined attributes are:

- `spring` on a node accepts a bare flag, `true`, or `false`. A spring-enabled
  node must have a containing box, which defines its growth axis. See
  [Springs](springs.md).
- `labelAlign` on a titled container accepts `auto`, `left`, `center`, or
  `right`. Omission is equivalent to `auto`. It is an error to put
  `labelAlign` on an untitled container. See
  [Container labels](container-labels.md).

Unsupported attributes and incompatible values are errors.

## Comments

Go-style line and block comments are accepted:

```boxz
// A line comment
node api /* an inline block comment */
```

## Grammar

```text
document   = element [ edge-block ]
element    = node | hbox | vbox
node       = "node" identifier [ string ] [ attributes ]
hbox       = "hbox" identifier [ string ] [ attributes ] "{" { item } "}"
vbox       = "vbox" identifier [ string ] [ attributes ] "{" { item } "}"
item       = element | "spring"

edge-block = "edges" "{" { edge } "}"
edge       = endpoint "->" endpoint
endpoint   = identifier [ ":" side ]
side       = "N" | "E" | "S" | "W"

attributes = "[" [ attribute { "," attribute } [ "," ] ] "]"
attribute  = identifier [ "=" scalar ]
scalar     = string | signed-number | identifier
```

The parser accepts an empty attribute list, although it has no effect. A
container containing only springs is rejected because springs have no intrinsic
content.

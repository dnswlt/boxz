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
nested freely.

```boxz
vbox system {
  hbox services {
    node client
    node api "API Server"
  }
  node database
}
```

A container may also contain standalone `spring` items for relational spacing
and alignment. See [Springs](springs.md) for their behavior.

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

Attributes follow a node title in square brackets. A trailing comma is allowed.
The syntax accepts bare flags and scalar string, signed-number, or identifier
values so future attributes can use appropriate types.

```boxz
node worker "Worker" [spring]
node cache "Cache" [spring = true,]
```

Only the `spring` node attribute is currently defined. It accepts a bare flag,
`true`, or `false`; unsupported attributes and incompatible values are errors.
A spring-enabled node must have a containing box, which defines its growth axis.
See [Springs](springs.md) for the complete layout semantics.

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
hbox       = "hbox" identifier "{" { item } "}"
vbox       = "vbox" identifier "{" { item } "}"
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

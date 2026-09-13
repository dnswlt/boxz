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

A plain identifier is a letter or underscore followed by letters, digits, and
underscores. An identifier that also needs punctuation goes in backticks. Its
content is literal, with no escapes, and may contain letters, digits, and
printable ASCII punctuation; whitespace, control characters, and other symbols
are rejected. `` `api` `` and `api` name the same element.

```boxz
node `api-server` "API Server"
node `db:primary`
```

An `hbox` arranges its contents from west to east. A `vbox` arranges them from
north to south. Containers must contain at least one actual element and may be
nested freely. An optional quoted title adds a top-aligned label and makes the
container bounded by default. A bounded container has a visible boundary and
forms a routing region; unrelated edges cannot use channels inside it. An
untitled, unbounded container remains transparent layout structure and is
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
hbox subsystem [bounded] { node cache }
```

The defined attributes are:

- `spring` on a node accepts a bare flag, `true`, or `false`. A spring-enabled
  node must have a containing box, which defines its growth axis. See
  [Springs](springs.md).
- `labelAlign` on a titled container accepts `auto`, `left`, `center`, or
  `right`. Omission is equivalent to `auto`. It is an error to put
  `labelAlign` on an untitled container. See
  [Container labels](container-labels.md).
- `bounded` on a container accepts a bare flag, `true`, or `false`. A title
  implies `bounded = true`; an explicit value overrides that default. Bounded
  containers are drawn and create routing regions even when they have no
  title.

Unsupported attributes and incompatible values are errors.

## Comments

Go-style line and block comments are accepted:

```boxz
// A line comment
node api /* an inline block comment */
```

The parser accepts comments, but canonical formatting does not preserve them.
`Format` retains semantic element, spring, and edge order while normalizing
whitespace, quoting, attributes, and optional defaults.

## Grammar

```text
document   = element [ edge-block ]
element    = node | hbox | vbox
node       = "node" id [ string ] [ attributes ]
hbox       = "hbox" id [ string ] [ attributes ] "{" { item } "}"
vbox       = "vbox" id [ string ] [ attributes ] "{" { item } "}"
id         = identifier | "`" char { char } "`"   (char: letter, digit, or ASCII punctuation)
item       = element | "spring"

edge-block = "edges" "{" { edge } "}"
edge       = endpoint "->" endpoint
endpoint   = id [ ":" side ]
side       = "N" | "E" | "S" | "W"

attributes = "[" [ attribute { "," attribute } [ "," ] ] "]"
attribute  = identifier [ "=" scalar ]
scalar     = string | signed-number | identifier
```

The parser accepts an empty attribute list, although it has no effect. A
container containing only springs is rejected because springs have no intrinsic
content.

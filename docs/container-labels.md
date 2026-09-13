# Container labels

An explicit title gives an `hbox` or `vbox` a label and makes it bounded by
default:

```boxz
vbox system "Order System" {
  hbox services "Services" [labelAlign = left] {
    node api "API"
    node worker "Worker"
  }
}
```

Container IDs are never used as fallback titles. Labels and boundaries are
separate model properties: `[bounded]` makes an untitled container visible,
while `[bounded = false]` leaves a titled container transparent to routing.

```boxz
hbox subsystem [bounded] { node api }
hbox layer "Logical layer" [bounded = false] { node worker }
```

A bounded container forms a routing region. An edge may enter such a region
when one of its endpoints is inside, but unrelated edges cannot borrow its
internal channels as transit space. Label text itself remains display-only and
never becomes a routing obstacle.

## Geometry

A titled container reserves one fixed-height horizontal strip at its top. In an
`hbox`, the north channel band begins below the label strip. In a `vbox`, the
west and east channel bands flank it. Horizontal channel runs therefore never
overlap label text, although vertical hierarchy risers may cross it.

Only the strip height participates in measurement. The title's length does not
establish a minimum width, so adding or editing a group name cannot widen the
diagram. After final layout, the renderer caps the label backing to the strip's
inner width and truncates the visible title with an ellipsis when necessary.
The generated SVG retains the complete title in a `<title>` element.

## Placement

By default, label placement is automatic. After routing and lane offsets are
final, Boxz moves the label horizontally to cover as few route segments as
possible. Equal scores choose the leftmost position, keeping output stable.
This is a display heuristic: labels never become routing obstacles and cannot
change route selection.

Set `labelAlign` to control placement explicitly. `auto` requests the default
heuristic and is also the value used when the attribute is omitted:

```boxz
hbox automaticGroup "Automatic" [labelAlign = auto] { node a }
hbox leftGroup "Left" [labelAlign = left] { node b }
hbox centeredGroup "Centered" [labelAlign = center] { node c }
hbox rightGroup "Right" [labelAlign = right] { node d }
```

Alignment uses text-layout words rather than compass directions, hence `left`,
`center`, and `right` rather than edge-side names.

## Rendering

Bounded container boundaries are painted behind routes. Label backings and
opaque text are painted after routes and nodes. The backing starts at 60
percent opacity, so a vertical route crossing the strip remains visible even
when no conflict-free label position exists.

Containers are still not edge endpoints. A boundary and optional label decorate
the existing layout container; they do not create a composite node.

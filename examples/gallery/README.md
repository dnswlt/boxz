# Visual routing gallery

These examples are deliberately small and opinionated. They complement the Go
tests by making routing quality, unnecessary bends, and accidental overlaps easy
to inspect after a larger layout or routing change.

Render the complete gallery from the repository root:

```sh
./scripts/render-gallery.sh
```

The command writes `.gallery/index.html` and prints its absolute path. It enables
debug rendering so container bounds, channels, risers, crossbars, and ports are
visible. An alternative output directory may be passed as the first argument.

Debug colors:

- Cyan dashed lines: container-owned channel centerlines.
- Purple dotted lines: hierarchy risers.
- Amber dotted lines: sibling crossbars.
- Red dots: allocated edge ports.
- Light dashed rectangles: structural containers.

| Example | What to inspect |
| --- | --- |
| `01-direct-seams` | Short E-W neighbor routes and aligned N-S routes |
| `02-recursive-frontiers` | Direct routes through multiple nesting levels |
| `03-cyclic-seam-doglegs` | Separate tracks and compact doglegs for crossed edges |
| `04-channel-lanes` | Parallel channel lanes and distinct ports |
| `05-local-crossbars` | Local sibling-gap traversal instead of a root-channel detour |
| `06-seam-crossbar-sharing` | No overlap between a crossbar and seam access leg |
| `07-endpoint-fanout` | Straight exits from separate ports, without local humps |
| `08-explicit-sides` | N/S constraints honored alongside an automatic E-W route |
| `09-label-sizing` | Stable routing when labels make nodes different widths |

The first comment in each `.boxz` file repeats its visual acceptance criterion,
so an individual example remains useful outside the contact sheet.

#!/bin/sh
# Render the gallery with both edge routers and emit a side-by-side page.
#
# The built-in router and boxz-avoid start from identical node placement, so
# any visual difference is a routing difference. Requires the sidecar binary:
#   cmake -S avoidrouter -B avoidrouter/build && cmake --build avoidrouter/build
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
output_dir=${1:-"$repo_dir/.gallery-compare"}

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
renderer=$(mktemp "${TMPDIR:-/tmp}/boxz-compare.XXXXXX")
trap 'rm -f "$renderer"' EXIT HUP INT TERM

(cd "$repo_dir" && go build -o "$renderer" ./cmd/boxz)

index="$output_dir/index.html"
{
  printf '%s\n' '<!doctype html>'
  printf '%s\n' '<meta charset="utf-8">'
  printf '%s\n' '<meta name="viewport" content="width=device-width, initial-scale=1">'
  printf '%s\n' '<title>boxz router comparison</title>'
  printf '%s\n' '<style>'
  printf '%s\n' 'body { margin: 24px; color: #0f172a; background: #f8fafc; font: 14px ui-sans-serif, system-ui, sans-serif; }'
  printf '%s\n' 'h1 { margin: 0 0 4px; font-size: 24px; }'
  printf '%s\n' 'main { display: flex; flex-direction: column; gap: 24px; align-items: stretch; }'
  printf '%s\n' 'section { padding: 16px; background: white; border: 1px solid #cbd5e1; border-radius: 8px; }'
  printf '%s\n' 'h2 { margin: 0 0 4px; font-size: 16px; }'
  printf '%s\n' 'p.criterion { margin: 0 0 12px; color: #475569; }'
  printf '%s\n' '.pair { display: flex; gap: 16px; flex-wrap: wrap; }'
  printf '%s\n' 'figure { margin: 0; flex: 1 1 380px; min-width: 0; overflow: auto; }'
  printf '%s\n' 'figcaption { margin-bottom: 8px; font-weight: 600; color: #334155; }'
  printf '%s\n' 'img { display: block; max-width: none; }'
  printf '%s\n' '.same { color: #16a34a; } .diff { color: #b45309; }'
  printf '%s\n' '</style>'
  printf '%s\n' '<h1>boxz router comparison</h1>'
  printf '%s\n' '<p class="criterion">Left: built-in structural router. Right: libavoid via boxz-avoid. Node placement is identical by construction.</p>'
  printf '%s\n' '<main>'
} > "$index"

for input in "$repo_dir"/examples/gallery/*.boxz; do
  name=$(basename "$input" .boxz)
  criterion=$(sed -n '1s|^// *||p' "$input")
  "$renderer" -router builtin -o "$output_dir/$name.builtin.svg" "$input"
  "$renderer" -router avoid   -o "$output_dir/$name.avoid.svg"   "$input"
  if cmp -s "$output_dir/$name.builtin.svg" "$output_dir/$name.avoid.svg"; then
    verdict='<span class="same">identical output</span>'
  else
    verdict='<span class="diff">routes differ</span>'
  fi
  {
    printf '  <section>\n'
    printf '    <h2>%s &mdash; %s</h2>\n' "$name" "$verdict"
    printf '    <p class="criterion">%s</p>\n' "$criterion"
    printf '    <div class="pair">\n'
    printf '      <figure><figcaption>builtin</figcaption><img src="%s.builtin.svg" alt="%s builtin"></figure>\n' "$name" "$name"
    printf '      <figure><figcaption>avoid</figcaption><img src="%s.avoid.svg" alt="%s avoid"></figure>\n' "$name" "$name"
    printf '    </div>\n'
    printf '  </section>\n'
  } >> "$index"
done

printf '%s\n' '</main>' >> "$index"
printf '%s\n' "$index"

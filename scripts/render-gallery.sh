#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
output_dir=${1:-"$repo_dir/.gallery"}

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
renderer=$(mktemp "${TMPDIR:-/tmp}/boxz-gallery.XXXXXX")
trap 'rm -f "$renderer"' EXIT HUP INT TERM

(cd "$repo_dir" && go build -o "$renderer" ./cmd/boxz)

index="$output_dir/index.html"
{
  printf '%s\n' '<!doctype html>'
  printf '%s\n' '<meta charset="utf-8">'
  printf '%s\n' '<meta name="viewport" content="width=device-width, initial-scale=1">'
  printf '%s\n' '<title>boxz visual routing gallery</title>'
  printf '%s\n' '<style>'
  printf '%s\n' 'body { margin: 24px; color: #0f172a; background: #f8fafc; font: 14px ui-sans-serif, system-ui, sans-serif; }'
  printf '%s\n' 'h1 { margin: 0 0 20px; font-size: 24px; }'
  printf '%s\n' 'main { display: flex; flex-direction: column; gap: 20px; align-items: flex-start; }'
  printf '%s\n' 'figure { margin: 0; padding: 16px; overflow: auto; background: white; border: 1px solid #cbd5e1; border-radius: 8px; }'
  printf '%s\n' 'figcaption { margin-bottom: 12px; font-weight: 600; }'
  printf '%s\n' 'p { margin: -4px 0 12px; color: #475569; }'
  printf '%s\n' 'img { display: block; max-width: none; }'
  printf '%s\n' '</style>'
  printf '%s\n' '<h1>boxz visual routing gallery</h1>'
  printf '%s\n' '<main>'
} > "$index"

for input in "$repo_dir"/examples/gallery/*.boxz; do
  name=$(basename "$input" .boxz)
  criterion=$(sed -n '1s|^// *||p' "$input")
  "$renderer" -debug -o "$output_dir/$name.svg" "$input"
  {
    printf '  <figure>\n'
    printf '    <figcaption>%s</figcaption>\n' "$name"
    printf '    <p>%s</p>\n' "$criterion"
    printf '    <img src="%s.svg" alt="%s">\n' "$name" "$name"
    printf '  </figure>\n'
  } >> "$index"
done

printf '%s\n' '</main>' >> "$index"
printf '%s\n' "$index"

demo_dir    := "demos"
out_dir     := "bin"
binary_name := "demolens"
cs2_dir  := "~/.steam/steam/steamapps/common/Counter-Strike Global Offensive"
maps_dir := "tris"

help:
    @just --list

build:
    go build -o {{out_dir}}/{{binary_name}} ./cmd/demolens

run *args:
    go run ./cmd/demolens {{args}}

analyze-random:
    #!/usr/bin/env bash
    set -euo pipefail
    demo="$(ls {{demo_dir}}/*.dem | shuf -n1)"
    echo "Analyzing $demo..." >&2
    time go run ./cmd/demolens analyze "$demo"

test:
    go test ./...

lint:
    go vet ./...

fmt:
    go fmt ./...

tidy:
    go mod tidy

clean:
    rm -rf {{out_dir}}

# Extract an official map's collision into {{maps_dir}}/.
# Needs source2viewer-cli on PATH. See docs/maps.md.
extract map:
    go run ./cmd/demolens extract-map --cs2 "{{cs2_dir}}" --map {{map}} --out {{maps_dir}}

# Extract a workshop map keyed by its addon id (just extract-workshop <vpk> <addon_id>).
extract-workshop vpk id:
    go run ./cmd/demolens extract-map --vpk "{{vpk}}" --key {{id}} --out {{maps_dir}}

# Extract collision for every map in the CS2 install (skips ones with no collision).
extract-all:
    #!/usr/bin/env bash
    set -uo pipefail
    cs2="{{cs2_dir}}"; cs2="${cs2/#\~/$HOME}"
    go build -o {{out_dir}}/{{binary_name}} ./cmd/demolens
    for vpk in "$cs2/game/csgo/maps"/*.vpk; do
        name="$(basename "$vpk" .vpk)"
        if {{out_dir}}/{{binary_name}} extract-map --vpk "$vpk" --key "$name" --out {{maps_dir}} >/dev/null 2>&1; then
            echo "ok    $name"
        else
            echo "skip  $name"
        fi
    done

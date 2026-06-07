demo_dir    := "demos"
out_dir     := "bin"
binary_name := "demolens"

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
    go run ./cmd/demolens analyze "$demo"

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

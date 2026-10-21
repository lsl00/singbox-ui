#!/bin/sh
set -eu

output="${OUTPUT:-mihomo-tui}"
goos="${GOOS:-$(go env GOHOSTOS)}"
goarch="${GOARCH:-$(go env GOHOSTARCH)}"

CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
  go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$output" .

printf 'built %s for %s/%s\n' "$output" "$goos" "$goarch"

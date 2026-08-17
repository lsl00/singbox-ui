#!/bin/sh
set -eu

output="${OUTPUT:-singbox-go-tui}"
goos="${GOOS:-linux}"
goarch="${GOARCH:-amd64}"

CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
  go build -trimpath -ldflags="-s -w" -o "$output" .

printf 'built %s for %s/%s\n' "$output" "$goos" "$goarch"

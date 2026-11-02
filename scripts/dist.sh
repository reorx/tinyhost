#!/bin/sh
# Local release artifacts only; publishing is handled separately.
set -eu
build_version=${1:?usage: scripts/dist.sh VERSION [OUTPUT_DIR]}
version=${build_version#v}
output=${2:-dist}
mkdir -p "$output"
output=$(cd "$output" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
checksums="$work/checksums.txt"
: > "$checksums"
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    binary=tinyhost
    if [ "$os" = windows ]; then binary=tinyhost.exe; fi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
      -ldflags "-s -w -X github.com/reorx/tinyhost/internal/version.Version=$build_version" \
      -o "$work/$binary" ./cmd/tinyhost
    name="tinyhost_${version}_${os}_${arch}.tar.gz"
    tar -czf "$output/$name" -C "$work" "$binary"
    if command -v sha256sum >/dev/null 2>&1; then
      sum=$(sha256sum "$output/$name" | awk '{print $1}')
    else
      sum=$(shasum -a 256 "$output/$name" | awk '{print $1}')
    fi
    printf '%s  %s\n' "$sum" "$name" >> "$checksums"
    printf '%s\n' "$name"
  done
done
mv -f "$checksums" "$output/checksums.txt"

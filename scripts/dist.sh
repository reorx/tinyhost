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
build() {
    command=$1
    os=$2
    arch=$3
    binary=$command
    if [ "$os" = windows ]; then binary=tinyhost.exe; fi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
      -ldflags "-s -w -X github.com/reorx/tinyhost/internal/version.Version=$build_version" \
      -o "$work/$binary" "./cmd/$command"
    name="${command}_${version}_${os}_${arch}"
    if [ "$os" = windows ]; then
      name="$name.zip"
      (cd "$work" && zip -q "$name" "$binary")
    else
      name="$name.tar.gz"
      COPYFILE_DISABLE=1 tar -czf "$work/$name" -C "$work" "$binary"
    fi
    mv -f "$work/$name" "$output/$name"
    if command -v sha256sum >/dev/null 2>&1; then
      sum=$(sha256sum "$output/$name" | awk '{print $1}')
    else
      sum=$(shasum -a 256 "$output/$name" | awk '{print $1}')
    fi
    printf '%s  %s\n' "$sum" "$name" >> "$checksums"
    printf '%s\n' "$name"
}
for os in linux darwin windows; do
  for arch in amd64 arm64; do
    build tinyhost "$os" "$arch"
  done
done
for arch in amd64 arm64; do
  build tinyhost-server linux "$arch"
done
build tinyhost-server darwin arm64
mv -f "$checksums" "$output/checksums.txt"

#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASES_DIR="$REPO_ROOT/releases"
VERSION="${1:-$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo dev)}"
TARGETS=(
  darwin/amd64
  darwin/arm64
  linux/amd64
  linux/arm64
  linux/arm
  linux/386
  windows/amd64
  windows/arm64
  freebsd/amd64
  freebsd/arm64
)

binary_name() {
  local os="$1" arch="$2"
  local name="sqx-$os-$arch"
  [[ "$os" == "windows" ]] && name="$name.exe"
  echo "$name"
}

build_target() {
  local os="${1%/*}" arch="${1#*/}"
  local output="$RELEASES_DIR/$(binary_name "$os" "$arch")"
  echo "building $output"
  (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM=7 \
    go build -trimpath -ldflags "-s -w -X main.buildVersion=$VERSION" -o "$output" ./sqx)
}

write_checksums() {
  (cd "$RELEASES_DIR" && shasum -a 256 sqx-* > SHA256SUMS)
}

rm -f "$RELEASES_DIR"/sqx-* "$RELEASES_DIR/SHA256SUMS"
mkdir -p "$RELEASES_DIR"
for target in "${TARGETS[@]}"; do
  build_target "$target"
done
write_checksums
echo "built ${#TARGETS[@]} binaries for version $VERSION in $RELEASES_DIR"

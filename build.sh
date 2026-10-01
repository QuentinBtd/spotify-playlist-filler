#!/usr/bin/env bash
set -euo pipefail

# Build from the repository root, regardless of the caller's directory.
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
go_command=${GO:-go}
package=${1:-spotify-playlist-filler}
if [[ $# -gt 1 || ! "$package" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]]; then
    printf '%s\n' 'usage: build.sh [binary-name]' >&2
    exit 2
fi
IFS= read -r version < VERSION
mkdir -p build
# Deliberately supported desktop/server targets, rather than every Go port.
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    os=${target%/*}
    arch=${target#*/}
    output="build/${package}-${version}-${os}-${arch}"
    if [[ "$os" == windows ]]; then
        output+=.exe
    fi
    printf 'Building %s\n' "$target"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$go_command" build -trimpath -o "$output" ./cmd/spotify-playlist-filler
done

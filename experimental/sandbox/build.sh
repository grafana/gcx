#!/usr/bin/env bash
# Builds gcx from this repository as a wasip1 module: experimental/sandbox/gcx.wasm.
# Usage: experimental/sandbox/build.sh [output path]
#
# Third-party modules without wasip1 support get stub files from patches/,
# overlaid onto a vendored copy of the source, so the checkout is never modified.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=${1:-$here/gcx.wasm}
src=$here/build/src

mkdir -p "$src"
rsync -a --delete --exclude .git --exclude /bin --exclude /dist --exclude /vendor --exclude /build --exclude /experimental "$root/" "$src/"
cd "$src"
go mod vendor
cp -r "$here/patches/." vendor/

# Unix implementations whose build constraints wrongly admit wasip1.
for f in \
	github.com/moby/term/term_unix.go \
	github.com/moby/term/termios_unix.go \
	github.com/moby/term/termios_nonbsd.go \
	github.com/prometheus/prometheus/tsdb/fileutil/mmap_unix.go; do
	# Not sed -i: its syntax differs between GNU and BSD.
	sed -E 's|^//go:build (.*)$|//go:build (\1) \&\& !wasip1|; /^\/\/ \+build /d' "vendor/$f" >"vendor/$f.tmp"
	mv "vendor/$f.tmp" "vendor/$f"
done

GOOS=wasip1 GOARCH=wasm go build -mod=vendor -trimpath -o "$out" ./cmd/gcx
echo "built $out"

#!/usr/bin/env bash
# Builds gcx from this repository as wasip1 modules: experimental/sandbox/gcx.wasm,
# and gcx-preinit.wasm next to it, the same gcx pre-initialized (see
# preinit.sh and "Pre-initialized module" in README.md).
# Usage: experimental/sandbox/build.sh [output path]
#
# Third-party modules without wasip1 support get stub files from patches/,
# overlaid onto a vendored copy of the source, so the checkout is never modified.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=${1:-$here/gcx.wasm}
preinit_out=${out%.wasm}-preinit.wasm
src=$here/build/src

mkdir -p "$src"
rsync -a --delete --exclude .git --exclude /bin --exclude /dist --exclude /vendor --exclude /build --exclude /experimental "$root/" "$src/"
cd "$src"
go mod vendor
cp -r "$here/patches/." vendor/

# Files whose build constraints admit wasip1: Unix implementations that
# don't work there, and flock's unsupported fallback, which patches/ replaces.
for f in \
	github.com/gofrs/flock/flock_others.go \
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

# The pre-initialized module: gcx as a WASI reactor, with gcx_run, and with
# overlay/runtime added to the Go runtime (go build -overlay replaces or adds
# files without touching GOROOT), then snapshotted by preinit.sh.
overlay=$here/build/overlay.json
printf '{"Replace":{"%s":"%s"}}\n' \
	"$(go env GOROOT)/src/runtime/gcx_reseed_wasip1.go" "$here/overlay/runtime/gcx_reseed_wasip1.go" >"$overlay"
reactor=$here/build/gcx-reactor.wasm
GOOS=wasip1 GOARCH=wasm go build -mod=vendor -trimpath -buildmode=c-shared -tags gcxpreinit \
	-overlay="$overlay" -o "$reactor" ./cmd/gcx
"$here/preinit.sh" "$reactor" "$preinit_out"
echo "built $preinit_out"

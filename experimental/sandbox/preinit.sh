#!/usr/bin/env bash
# Pre-initializes a gcx reactor module with wizer: runs its _initialize (the
# Go runtime and every package init) and snapshots the result, so each
# instance started from the output skips init. The reactor must export
# gcx_init and gcx_run (see cmd/gcx/preinit_wasip1.go).
# Usage: experimental/sandbox/preinit.sh <reactor.wasm> <output.wasm>
#
# Needs wasmtime, which the repository's mise.toml pins.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	sed -n '6p' "$0" >&2
	exit 2
fi
if ! command -v wasmtime >/dev/null; then
	echo "preinit.sh: needs wasmtime for wizer; run mise install in the repository" >&2
	exit 1
fi

# The Go runtime caches WASI's preopened directories at init, by descriptor,
# so the snapshot must see the same mounts, in the same order, as each
# sandbox instance: a read-only root at /, then $HOME at /home (see Run in
# sandbox.go). Both are empty here.
mounts=$(mktemp -d)
trap 'rm -rf "$mounts"' EXIT
mkdir "$mounts/root" "$mounts/home"

# Init may not call gcx_http, which wizer can't provide: such a call traps
# and fails the snapshot. wasmtime passes the guest no environment of its
# own, only HOME=/home as Run sets it, so nothing the snapshot keeps
# depends on the build machine's environment.
wasmtime wizer \
	-S cli=y -W unknown-imports-trap=y \
	--dir "$mounts/root::/" --dir "$mounts/home::/home" --env HOME=/home \
	--init-func gcx_init \
	-o "$2" "$1"

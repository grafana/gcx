#!/bin/sh
# The smallest possible gcx extension: no compiler, no SDK, no dependencies.
#
# GCX_EXT_CONTEXT carries the context the parent gcx invocation resolved,
# including one set with `gcx --context <name> ext whoami`. Passing it back on
# every gcx call is the extension author's job: a bare `gcx` call silently
# targets current-context instead, which is the wrong stack.
set -eu

ctx_flag=""
[ -n "${GCX_EXT_CONTEXT:-}" ] && ctx_flag="--context ${GCX_EXT_CONTEXT}"

# shellcheck disable=SC2086
"$GCX_EXT_GCX_BIN" config current-context --output json $ctx_flag

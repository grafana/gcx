#!/bin/sh
set -eu
example_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$example_dir/../../.." && pwd)
(cd "$repo_root" && go build -buildvcs=false -o bin/gcx ./cmd/gcx)
(cd "$example_dir" && go build -o "$repo_root/bin/resource-explorer" .)
printf '\nRun: cd "%s" && GCX_AGENT_MODE=false bin/gcx plugins run --demo ./bin/resource-explorer\n' "$repo_root"

#!/usr/bin/env bash
# Measures the memory a process embedding the sandbox uses with each of one or
# more gcx checkouts, and prints markdown tables comparing them. See
# "Measuring memory" in README.md.
#
# Usage: experimental/sandbox/measure.sh <gcx.wasm> <name>=<gcx checkout>...
#
# Linux only: each run gets its own cgroup from `systemd-run --user --scope`,
# so this needs cgroup v2 and a systemd user manager with the memory
# controller delegated, as desktop distributions have. The first build is the
# baseline of the change rows. REPS (default 2) is the number of runs per
# build with and without FORCE_GC=1. OUT is where the builds, compilation
# caches and raw reports go (default: a new directory under $TMPDIR or /tmp);
# keep it off tmpfs, so mapped code pages behave like ones read from disk.
set -euo pipefail

if [[ $# -lt 2 ]]; then
	sed -n '5p' "$0" >&2
	exit 2
fi
here=$(cd "$(dirname "$0")" && pwd)
wasm=$(realpath "$1")
shift
reps=${REPS:-2}
out=${OUT:-$(mktemp -d "${TMPDIR:-/tmp}/gcx-measure.XXXXXX")}
mkdir -p "$out"
export GOFLAGS=-buildvcs=false

# One measure binary per checkout, built against that checkout's sandbox.
names=()
for arg in "$@"; do
	name=${arg%%=*}
	checkout=$(realpath "${arg#*=}")
	names+=("$name")
	dir=$out/$name
	mkdir -p "$dir/src" "$dir/cache"
	cp "$here/internal/cmd/measure/main.go" "$dir/src/"
	{
		printf 'module measure\n\ngo 1.25.0\n\nrequire github.com/grafana/gcx/experimental/sandbox v0.0.0\n\n'
		printf 'replace github.com/grafana/gcx/experimental/sandbox => %s/experimental/sandbox\n' "$checkout"
		# replace directives only apply from the main module, so copy the
		# checkout's wazero fork pin, if it has one.
		grep '^replace github.com/tetratelabs/wazero ' "$checkout/experimental/sandbox/go.mod" || true
	} >"$dir/src/go.mod"
	(cd "$dir/src" && go mod tidy && go build -o "$dir/measure" .)
	# Compile into the build's own cache, outside the measured cgroups.
	"$dir/measure" precompile "$wasm" "$dir/cache" >/dev/null
	echo "built and precompiled $name" >&2
done

# Alternate builds, so drift on the machine affects them alike.
for gc in 1 0; do
	for rep in $(seq "$reps"); do
		for name in "${names[@]}"; do
			dir=$out/$name
			"$dir/measure" evict "$wasm" "$dir/cache"
			env=()
			[[ $gc == 1 ]] && env=(-E FORCE_GC=1)
			systemd-run --user --scope -q -p MemoryAccounting=yes ${env[@]+"${env[@]}"} \
				"$dir/measure" run "$wasm" "$dir/cache" >"$out/$name-gc$gc-$rep.txt"
		done
	done
done

"$out/${names[0]}/measure" summarize "$out" "${names[@]}" | tee "$out/summary.md"
echo "reports in $out" >&2

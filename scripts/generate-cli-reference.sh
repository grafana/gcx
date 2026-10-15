#!/usr/bin/env bash
set -euo pipefail

scripts_dir=$(cd "$(dirname "$0")" && pwd)
output="$scripts_dir/../docs/sources"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT
version=${1:-}
if [[ -n "$version" ]]; then
  source_dir=$(cd "$scripts_dir/.." && pwd)
else
  version=$(gh api repos/grafana/gcx/releases/latest --jq .tag_name)
  gh api "repos/grafana/gcx/tarball/$version" > "$temp_dir/source.tar.gz"
  source_dir="$temp_dir/source"
  mkdir -p "$source_dir"
  tar -xzf "$temp_dir/source.tar.gz" -C "$source_dir" --strip-components=1

  # Use the current renderer with the release's command tree and metadata.
  mkdir -p "$source_dir/scripts/cli-reference"
  cp "$scripts_dir"/cli-reference/*.go "$source_dir/scripts/cli-reference/"
fi
echo "Generating references for $version"
renderer_dir="$source_dir/scripts/cli-reference"
cd "$source_dir"
export GCX_AGENT_MODE=false CGO_ENABLED=0
# Prevent local credentials or defaults from affecting the reference.
export GCX_CONFIG="$temp_dir/config.yaml"
go run ./scripts/config-reference "$temp_dir/config"
go run ./scripts/env-vars-reference "$temp_dir/env"
go run "$renderer_dir" --version "$version" --config "$temp_dir/config/index.md" --config-page "$scripts_dir/../docs/sources/configuration.md" --env "$temp_dir/env/index.md" --output-dir "$output"

echo "Review git diff -- docs/sources/cli-reference.md docs/sources/configuration.md"
echo "Include changes in the release PR, or open a documentation PR for a published release."

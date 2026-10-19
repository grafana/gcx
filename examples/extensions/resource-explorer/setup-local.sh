#!/bin/sh
set -eu
extension_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$extension_dir/../../.." && pwd)
state_dir="$repo_root/.resource-explorer-local"
mkdir -p "$state_dir/source/bin" "$state_dir/extensions"
(cd "$extension_dir" && go build -o "$state_dir/source/bin/resource-explorer" .)
cp "$extension_dir/gcx-extension.yaml" "$state_dir/source/gcx-extension.yaml"
if [ ! -d "$state_dir/source/.git" ]; then git init --quiet "$state_dir/source"; fi
git -C "$state_dir/source" add gcx-extension.yaml
if ! git -C "$state_dir/source" diff --cached --quiet; then
  git -C "$state_dir/source" -c commit.gpgsign=false -c user.name='Extension demo' -c user.email='extension-demo@example.invalid' commit --quiet -m 'add local resource-explorer manifest'
fi
GCX_EXTENSIONS_DIR="$state_dir/extensions" GCX_TELEMETRY=disabled "$repo_root/bin/gcx" ext install "$state_dir/source" --yes
printf '\nRun: GCX_EXTENSIONS_DIR="%s/extensions" GCX_AGENT_MODE=false GCX_TELEMETRY=disabled "%s/bin/gcx" ext resource-explorer --demo\n' "$state_dir" "$repo_root"

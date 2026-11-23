#!/usr/bin/env bash
# Generates the extension protocol for the SDK and for the gcx host. gcx does
# not import the SDK module, so both sides compile the same proto file.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
cd "$here/proto"

protoc --go_out="$here/internal/extensionpb" --go_opt=paths=source_relative \
  --go-grpc_out="$here/internal/extensionpb" --go-grpc_opt=paths=source_relative \
  extension.proto

host="$here/../internal/extensions/extensionpb"
pkg="Mextension.proto=github.com/grafana/gcx/internal/extensions/extensionpb"
protoc --go_out="$host" --go_opt=paths=source_relative,"$pkg" \
  --go-grpc_out="$host" --go-grpc_opt=paths=source_relative,"$pkg" \
  extension.proto

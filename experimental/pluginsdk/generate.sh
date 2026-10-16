#!/bin/sh
set -eu
cd "$(dirname "$0")"
# Use protoc 36.2, protoc-gen-go v1.36.11 and protoc-gen-go-grpc v1.6.1.
protoc --go_out=paths=source_relative:. --go-grpc_out=paths=source_relative:. plugin.proto

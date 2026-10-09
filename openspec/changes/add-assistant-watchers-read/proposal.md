# Proposal

## Why

Assistant Watchers cannot currently be inspected or pulled through gcx. This implements the read-only first slice of [#1493](https://github.com/grafana/gcx/issues/1493), under [#1470](https://github.com/grafana/gcx/issues/1470) and [RFC 002](https://github.com/grafana/gcx/blob/main/docs/rfcs/002-assistant-watchers.md), while the Watcher product is in public preview.

## What Changes

- Add experimental `gcx assistant watchers list`, `get WATCHER`, and `status WATCHER` commands.
- Register a configuration-only `Watcher` resource for `gcx resources get/pull watchers[/WATCHER]`, sharing typed reads with the dedicated commands.
- Fix the complete RFC manifest schema now, including notification secret inputs. Reads export configured secrets as `preserve: true`, never values or redacted placeholders.
- Resolve resource names derived from titles and accept server IDs. Report all conflicting candidates; bulk pull skips colliding names and writes the remaining resources.
- Exhaust collection pagination and describe coverage as resources visible to the caller, with an archived-only list option.
- Separate observed lifecycle, assessment and calibration in status; preserve unsupported check information without inventing missing observations.
- Keep writes unsupported and report actionable errors through generic push/delete.
- Resolve registered provider kinds when a discovered group prefers a version that does not contain that kind, preserving native and explicit-version selection.

## Capabilities

### New Capabilities

- `assistant-watchers`: configuration reads, safe manifest export, identity resolution and observed runtime status for Assistant Watchers. Later slices extend this same capability.

### Modified Capabilities

None.

## Impact

Extend the existing Assistant provider and reuse its authentication and transport. Add a domain client under `internal/assistant/watchers/`, manifest and typed adapter under `internal/assistant/watcher/`, and commands under `internal/providers/assistant/watchers/`. Update provider registration, experimental and agent metadata, shared command registries, reference and architecture documentation, and ownership entries for Watcher artifacts and Assistant reference docs. No new dependencies or new provider are planned. Selector-free generic get/pull also include registered kinds missing from the native preferred version, so pull-all can create additional resource directories, including MCP servers and Watchers.

Creation, configuration writes, calibration requests, lifecycle operations, run history and version history are outside this change. No Watcher or shared system is mutated by these reads.

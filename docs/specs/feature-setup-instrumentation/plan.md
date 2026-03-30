---
type: feature-plan
title: "Declarative Instrumentation Setup under gcx setup"
status: draft
spec: docs/specs/feature-setup-instrumentation/spec.md
created: 2026-03-30
---

# Architecture and Design Decisions

## Pipeline Architecture

```
User
 │
 ├─ gcx setup status                    ─┐
 ├─ gcx setup instrumentation status     │
 ├─ gcx setup instrumentation discover   ├─► cmd/gcx/setup/
 ├─ gcx setup instrumentation show       │   ├── command.go          (setup group + status)
 ├─ gcx setup instrumentation apply      │   └── instrumentation/
 │                                       │       ├── command.go      (instrumentation group)
 │                                       │       ├── status.go
 │                                       │       ├── discover.go
 │                                       │       ├── show.go
 │                                       │       └── apply.go
 │                                      ─┘
 │                                        │
 │                    ┌───────────────────┘
 │                    ▼
 │           internal/setup/instrumentation/
 │           ├── types.go           InstrumentationConfig manifest struct
 │           ├── client.go          Instrumentation + Discovery HTTP client
 │           ├── compare.go         Optimistic lock comparison logic
 │           └── client_test.go     Unit tests with HTTP mocks
 │                    │
 │                    ▼
 │           internal/fleet/                    (NEW — extracted)
 │           ├── client.go          Base HTTP client (doRequest, auth)
 │           ├── config.go          LoadClient(ctx) helper
 │           ├── errors.go          Error types + readErrorBody
 │           └── client_test.go     Unit tests
 │                    │
 │         ┌──────────┴──────────┐
 │         ▼                     ▼
 │  internal/providers/    internal/setup/
 │  fleet/ (refactored)    instrumentation/
 │  imports internal/fleet  imports internal/fleet
 │                    │
 │                    ▼
 │  Fleet Management API (Connect JSON-over-HTTP)
 │  ├── instrumentation.v1.InstrumentationService
 │  ├── discovery.v1.DiscoveryService
 │  └── pipeline.v1.PipelineService (existing)
 │
 ├─ gcx fleet pipelines update ──► Pipeline protection guard
 └─ gcx fleet pipelines delete ──► (checks beyla_k8s_appo11y_ prefix)

Prometheus query for status:
  internal/query/prometheus/ (existing client, reused for Beyla error query)
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| `cmd/gcx/setup/` wired via `rootCmd.AddCommand(setup.Command())` in `root/command.go`, same pattern as `dev.Command()` | FR-001: setup is an area, not a provider. Direct wiring avoids `providers.Register()` and keeps the command tree explicit. |
| `internal/fleet/` extracts `Client`, `NewClient`, `doRequest`, `readErrorBody`, auth logic from `internal/providers/fleet/client.go` | FR-030–035: Shared base eliminates duplication. The fleet provider becomes a thin wrapper importing `internal/fleet/`. |
| `internal/setup/instrumentation/` houses manifest types, instrumentation client, and comparison logic | FR-024–029, FR-036–037: Separates instrumentation domain from CLI layer. The `client.go` composes `internal/fleet.Client` and adds instrumentation-specific methods. |
| `internal/setup/instrumentation/types.go` defines `InstrumentationConfig` as a plain Go struct with `yaml`/`json` tags | FR-024–029, NC-002: Not a K8s resource. No `unstructured.Unstructured`, no dynamic client. |
| Optimistic lock comparison in `internal/setup/instrumentation/compare.go` | FR-017, NC-004: Isolated, unit-testable comparison function. Takes local and remote state, returns diff of remote-only items. |
| Pipeline protection guard added to `internal/providers/fleet/provider.go` in `newPipelineUpdateCommand` and `newPipelineDeleteCommand` | FR-038–040: Guard lives in the fleet provider where the commands are defined. Checks pipeline name prefix before API call. `--force` flag bypasses. |
| `status` command uses existing `internal/query/prometheus/` client for Beyla error query | FR-004: Reuses the Prometheus HTTP client already in the codebase. No new query infrastructure needed. |
| `providers.ConfigLoader` reused for setup commands (not `cmd/gcx/config.Options`) | Avoids import cycle. Setup commands use `ConfigLoader.LoadCloudConfig()` to get `CloudRESTConfig` with `StackInfo`, same as fleet provider. |
| `LoadClient` in `internal/fleet/config.go` encapsulates the `LoadCloudConfig → extract URL/ID → NewClient` pattern | FR-034: Both fleet provider and instrumentation commands share this client-construction logic. Reduces the 15-line boilerplate repeated in `fleetHelper.loadClient`, `NewPipelineTypedCRUD`, `NewCollectorTypedCRUD`. |
| Table codecs for `status` and `discover` are command-local (in `cmd/gcx/setup/instrumentation/`) | FR-044: Follows the same pattern as `slo definitions list` and `synth checks list` — custom `format.Codec` registered via `opts.IO.RegisterCustomCodec`. |
| Error prefix `"setup/instrumentation: "` applied at the CLI layer, not in the client | FR-046: Client returns domain errors. CLI commands wrap them with the prefix. Keeps the client reusable. |

## Compatibility

**Continues working unchanged:**
- All existing `gcx fleet pipelines` and `gcx fleet collectors` commands (FR-035, NC-010)
- All existing fleet provider tests pass without modification
- All other providers, `resources`, `config`, `dev`, `datasources`, `dashboards` commands
- `gcx commands` introspection (new `setup` commands appear automatically via Cobra tree)

**New:**
- `gcx setup` top-level command area
- `gcx setup status` aggregated product status
- `gcx setup instrumentation {status,discover,show,apply}` subcommands
- `internal/fleet/` shared base client package
- `internal/setup/instrumentation/` domain package
- Pipeline protection guard on `gcx fleet pipelines {update,delete}` for `beyla_k8s_appo11y_` prefix

**No deprecations** in this change.

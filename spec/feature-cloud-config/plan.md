---
type: feature-plan
title: "Grafana Cloud Shared Config and GCOM Discovery"
status: draft
spec: spec/feature-cloud-config/spec.md
created: 2026-03-21
---

# Architecture and Design Decisions

## Pipeline Architecture

```
User Config File / Env Vars
┌─────────────────────────────────────────────────────────┐
│  contexts:                                              │
│    mystack:                                             │
│      grafana:           ← existing Grafana config       │
│        server: https://mystack.grafana.net              │
│        token: "..."                                     │
│      cloud:             ← NEW cloud config              │
│        token: "..."     (GRAFANA_CLOUD_TOKEN)           │
│        stack: "mystack" (GRAFANA_CLOUD_STACK)           │
│        api-url: "..."   (GRAFANA_CLOUD_API_URL)         │
│      providers:         ← existing per-provider config  │
│        slo: { ... }                                     │
└─────────────────────────────────────────────────────────┘
            │                            │
            ▼                            ▼
┌──────────────────────┐   ┌──────────────────────────────┐
│ LoadGrafanaConfig()  │   │ LoadCloudConfig()            │
│ (renamed from        │   │ (NEW method on ConfigLoader) │
│  LoadRESTConfig)     │   │                              │
│                      │   │ 1. Load config + env vars    │
│ Returns:             │   │ 2. Validate cloud.token      │
│ NamespacedRESTConfig │   │ 3. ResolveStackSlug()        │
│                      │   │ 4. ResolveGCOMURL()          │
│ Used by: SLO, Synth, │   │ 5. GCOMClient.GetStack()    │
│ Alert, Incidents,    │   │                              │
│ core resource cmds   │   │ Returns: CloudRESTConfig     │
└──────────────────────┘   │ { Token, StackInfo, NS }     │
                           └──────────────┬───────────────┘
                                          │
                           ┌──────────────▼───────────────┐
                           │ internal/cloud/GCOMClient    │
                           │                              │
                           │ GET /api/instances/{slug}    │
                           │ Authorization: Bearer {tok}  │
                           │                              │
                           │ Returns: StackInfo           │
                           │ { ID, Slug, URL, OrgID,     │
                           │   AgentMgmt URL/ID,          │
                           │   Prometheus URL/ID, ... }   │
                           └──────────────┬───────────────┘
                                          │
                           ┌──────────────▼───────────────┐
                           │ Fleet Provider (refactored)  │
                           │                              │
                           │ Uses: LoadCloudConfig()      │
                           │ Gets URL from:               │
                           │   StackInfo.AgentMgmtURL     │
                           │ Gets ID from:                │
                           │   StackInfo.AgentMgmtID      │
                           │ Gets token from:             │
                           │   CloudRESTConfig.Token       │
                           │                              │
                           │ ConfigKeys() → nil           │
                           │ No GRAFANA_FLEET_* env vars  │
                           └──────────────────────────────┘
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Add `CloudConfig` as a new struct type parallel to `GrafanaConfig` (FR-001, FR-002) | Keeps cloud auth separate from Grafana instance auth. Both can coexist on the same context — a user may have both a Grafana instance and cloud platform credentials. |
| `ResolveStackSlug()` on `Context` with explicit > derived > empty precedence (FR-004) | The method lives on `Context` because it needs access to both `Cloud.Stack` and `Grafana.Server`. Derivation from `*.grafana.net` eliminates configuration for the common case. |
| `ResolveGCOMURL()` defaults to `https://grafana.com`, auto-prefixes `https://` (FR-005) | Production GCOM is the overwhelmingly common case. Auto-prefixing avoids user error from omitting the scheme. |
| GCOM client in `internal/cloud/` as a standalone package (FR-006, FR-007) | Dedicated package avoids import cycles and provides a clean home for future cloud platform clients (e.g., token management). |
| `CloudRESTConfig` in `internal/providers/` alongside `ConfigLoader` (FR-009) | Mirrors the existing pattern where `ConfigLoader` returns config structs consumed by providers. Avoids providers importing the cloud package directly. |
| `LoadCloudConfig` validates cloud.token and stack slug, not grafana.server (FR-010, FR-012) | Cloud-only contexts are valid. Validation happens at use-time in `LoadCloudConfig`, not in `Context.Validate()` which remains unchanged. |
| Rename `LoadRESTConfig` → `LoadGrafanaConfig` in a single atomic task (FR-013, FR-014) | Pure rename with zero behavioral change. Doing it atomically minimizes the window for merge conflicts. The 5 local `RESTConfigLoader` interface declarations (incidents, alert, synth/smcfg, slo/definitions, slo/reports) are renamed to `GrafanaConfigLoader`. |
| Fleet `configLoader` and `LoadFleetConfig` removed entirely (FR-015, FR-016, FR-017) | The fleet provider's `configLoader` is a copy of `providers.ConfigLoader` with fleet-specific env var handling bolted on. After the refactor, fleet uses `LoadCloudConfig` and gets its URL/ID from `StackInfo`. The `fleetHelper` type changes its dependency from the local `configLoader` to `providers.ConfigLoader`. |
| Fleet `FleetConfigLoader` interface replaced by `CloudConfigLoader` interface (FR-015) | The adapter factories (`NewPipelineAdapterFactory`, `NewCollectorAdapterFactory`) currently depend on `FleetConfigLoader`. After refactoring, they depend on a `CloudConfigLoader` interface with `LoadCloudConfig`. |
| `env.Parse` handles `GRAFANA_CLOUD_*` env vars via struct tags on `CloudConfig` (FR-011) | Follows existing pattern: `env.Parse(curCtx)` already resolves env tags on `GrafanaConfig`. Adding env tags to `CloudConfig` fields makes them resolve automatically. |

## Compatibility

**Continues working unchanged:**
- All existing `grafana.*` config paths and `GRAFANA_*` env vars (server, token, org-id, stack-id, etc.)
- `LoadGrafanaConfig` (renamed from `LoadRESTConfig`) returns identical results
- All provider commands (SLO, Synth, Alert, Incidents) that use `LoadGrafanaConfig`
- `config set`/`config view` for existing paths
- Per-provider config via `providers.<name>.*` paths and `GRAFANA_PROVIDER_*` env vars
- `Context.Validate()` behavior (still requires `grafana.server`)

**Deprecated / Removed:**
- `GRAFANA_FLEET_URL`, `GRAFANA_FLEET_INSTANCE_ID`, `GRAFANA_FLEET_TOKEN` env vars — removed
- `providers.fleet.url`, `providers.fleet.instance-id`, `providers.fleet.token` config keys — no longer used
- Fleet provider's custom `configLoader` struct and `LoadFleetConfig` method — deleted

**Newly available:**
- `contexts.<name>.cloud.token/stack/api-url` config paths
- `GRAFANA_CLOUD_TOKEN`, `GRAFANA_CLOUD_STACK`, `GRAFANA_CLOUD_API_URL` env vars
- `providers.ConfigLoader.LoadCloudConfig()` method for cloud provider authors
- `internal/cloud.GCOMClient` for GCOM API access
- Stack slug auto-derivation from `grafana.server` URL

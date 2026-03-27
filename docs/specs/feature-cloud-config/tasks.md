---
type: feature-tasks
title: "Grafana Cloud Shared Config and GCOM Discovery"
status: draft
spec: spec/feature-cloud-config/spec.md
plan: spec/feature-cloud-config/plan.md
created: 2026-03-21
---

# Implementation Tasks

## Dependency Graph

```
T1 (CloudConfig types + Context methods + config editor)
├──► T2 (GCOM client)
│     └──► T4 (LoadCloudConfig on ConfigLoader)
│           └──► T5 (Fleet provider refactor)

T3 (Rename LoadRESTConfig → LoadGrafanaConfig)  ← independent, no deps
```

## Wave 1: Foundation Types and Rename

### T1: CloudConfig type, Context methods, and config editor support
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Add the `CloudConfig` struct to `internal/config/types.go` with `Token`, `Stack`, and `APIUrl` fields including env tags and datapolicy annotations. Add the `Cloud *CloudConfig` field to `Context`. Implement `ResolveStackSlug()` and `ResolveGCOMURL()` methods on `Context`. The config editor already uses reflection on YAML tags, so adding the `Cloud` field with proper YAML tags (`cloud`, `token`, `stack`, `api-url`) enables `config set` support automatically.

**Deliverables:**
- `internal/config/types.go` — `CloudConfig` struct, `Cloud` field on `Context`, `ResolveStackSlug()`, `ResolveGCOMURL()`
- `internal/config/types_test.go` — unit tests for `ResolveStackSlug` and `ResolveGCOMURL`
- `internal/config/editor_test.go` — tests for `config set contexts.<name>.cloud.*` paths

**Acceptance criteria:**
- GIVEN a context with `cloud.stack` set to `"explicit"` and `grafana.server` set to `https://derived.grafana.net`
  WHEN `ResolveStackSlug()` is called
  THEN it returns `"explicit"`
- GIVEN a context with only `grafana.server` set to `https://mystack.grafana.net` and no `cloud.stack`
  WHEN `ResolveStackSlug()` is called
  THEN it returns `"mystack"`
- GIVEN a context with `grafana.server` set to `https://grafana.mycompany.com` and no `cloud.stack`
  WHEN `ResolveStackSlug()` is called
  THEN it returns `""`
- GIVEN a context with no `cloud.api-url` set
  WHEN `ResolveGCOMURL()` is called
  THEN it returns `"https://grafana.com"`
- GIVEN a context with `cloud.api-url` set to `"grafana-dev.com"`
  WHEN `ResolveGCOMURL()` is called
  THEN it returns `"https://grafana-dev.com"`
- GIVEN `gcx config set contexts.dev.cloud.token my-token` is run
  WHEN the config file is read back
  THEN `contexts.dev.cloud.token` is set to `"my-token"`
- GIVEN `gcx config set contexts.dev.cloud.stack mystack` is run
  WHEN the config file is read back
  THEN `contexts.dev.cloud.stack` is set to `"mystack"`
- GIVEN `gcx config set contexts.dev.cloud.api-url grafana-dev.com` is run
  WHEN the config file is read back
  THEN `contexts.dev.cloud.api-url` is set to `"grafana-dev.com"`

---

### T3: Rename LoadRESTConfig to LoadGrafanaConfig
**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: chore

Rename `LoadRESTConfig` to `LoadGrafanaConfig` on `providers.ConfigLoader`. Rename all 5 local `RESTConfigLoader` interface declarations to `GrafanaConfigLoader` (in `incidents/resource_adapter.go`, `alert/rules_commands.go`, `synth/smcfg/loader.go`, `slo/definitions/commands.go`, `slo/reports/commands.go`). Update all ~40 caller sites across `cmd/gcx/` and `internal/providers/`. This is a pure rename with zero behavioral change. Execute as a single atomic commit.

**Deliverables:**
- `internal/providers/configloader.go` — method renamed
- `internal/providers/incidents/resource_adapter.go` — interface renamed
- `internal/providers/incidents/commands.go` — callers updated
- `internal/providers/alert/rules_commands.go` — interface + callers renamed
- `internal/providers/alert/resource_adapter.go` — callers updated
- `internal/providers/alert/groups_commands.go` — callers updated
- `internal/providers/synth/smcfg/loader.go` — interface renamed
- `internal/providers/synth/checks/status.go` — callers updated
- `internal/providers/synth/provider.go` — callers updated
- `internal/providers/slo/definitions/commands.go` — interface + callers renamed
- `internal/providers/slo/definitions/resource_adapter.go` — callers updated
- `internal/providers/slo/definitions/status.go` — callers updated
- `internal/providers/slo/definitions/timeline.go` — callers updated
- `internal/providers/slo/reports/commands.go` — interface + callers renamed
- `internal/providers/slo/reports/status.go` — callers updated
- `internal/providers/slo/reports/timeline.go` — callers updated
- `cmd/gcx/` — all callers updated (~20 files)

**Acceptance criteria:**
- The system SHALL compile with zero errors after the rename across all packages
- The system SHALL pass all existing tests after the rename with no test modifications beyond updating the method name
- GIVEN the `LoadGrafanaConfig` method exists on `providers.ConfigLoader`
  WHEN `LoadGrafanaConfig` is called
  THEN it returns `config.NamespacedRESTConfig` exactly as `LoadRESTConfig` did before the rename

---

## Wave 2: GCOM Client

### T2: GCOM API client and StackInfo type
**Priority**: P0
**Effort**: Small
**Depends on**: T1
**Type**: task

Create `internal/cloud/` package with `GCOMClient` and `StackInfo` types. `GCOMClient` takes a base URL and token, exposes `GetStack(ctx, slug)` that calls `GET /api/instances/{slug}` with Bearer auth. `StackInfo` includes ID, Slug, Name, URL, OrgID, OrgSlug, Status, RegionSlug, and service instance IDs/URLs (Prometheus, Loki, Tempo, Pyroscope, Agent Management, Alertmanager). The client returns errors with HTTP status code and body on non-200 responses. The client MUST NOT follow redirects to a different domain.

**Deliverables:**
- `internal/cloud/gcom.go` — `GCOMClient`, `StackInfo`, `NewGCOMClient`, `GetStack`
- `internal/cloud/gcom_test.go` — unit tests using `httptest.Server`

**Acceptance criteria:**
- GIVEN a valid GCOM token and stack slug
  WHEN `GCOMClient.GetStack(ctx, slug)` is called
  THEN it sends `GET /api/instances/{slug}` with `Authorization: Bearer {token}` header and returns populated `StackInfo`
- GIVEN a GCOM API that returns HTTP 404
  WHEN `GCOMClient.GetStack(ctx, slug)` is called
  THEN it returns an error containing the status code

---

## Wave 3: Cloud Config Loader

### T4: LoadCloudConfig method on providers.ConfigLoader
**Priority**: P0
**Effort**: Medium
**Depends on**: T2, T3
**Type**: task

Add `CloudRESTConfig` struct to `internal/providers/` with Token (string), Stack (`cloud.StackInfo`), and Namespace (string). Add `LoadCloudConfig(ctx) (CloudRESTConfig, error)` method to `providers.ConfigLoader`. The method loads config with env var overrides (including `GRAFANA_CLOUD_*` via `env.Parse`), validates that `cloud.token` is present, resolves stack slug via `Context.ResolveStackSlug()`, resolves GCOM URL via `Context.ResolveGCOMURL()`, calls `GCOMClient.GetStack()`, and returns `CloudRESTConfig`. It MUST NOT require `grafana.server`. It MUST NOT fall back to `grafana.token` for cloud auth.

**Deliverables:**
- `internal/providers/configloader.go` — `CloudRESTConfig` struct, `LoadCloudConfig` method
- `internal/providers/configloader_test.go` — unit tests with mock GCOM server

**Acceptance criteria:**
- GIVEN no config file
  WHEN `GRAFANA_CLOUD_TOKEN` and `GRAFANA_CLOUD_STACK` environment variables are set
  THEN `LoadCloudConfig` uses the env var values to authenticate and discover stack info
- GIVEN a context with `cloud.token` not set
  WHEN `LoadCloudConfig` is called
  THEN it returns an error indicating the cloud token is missing
- GIVEN a context with `cloud.token` set but no stack slug resolvable
  WHEN `LoadCloudConfig` is called
  THEN it returns an error indicating the cloud stack is not configured

---

## Wave 4: Fleet Provider Refactor

### T5: Refactor fleet provider to use LoadCloudConfig
**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T4
**Type**: task

Remove the fleet provider's custom `configLoader` struct, `LoadFleetConfig` method, `loadConfig` helper, and `envOverride` function. Replace `fleetHelper.loader` type from `*configLoader` to `*providers.ConfigLoader`. Update `fleetHelper.withClient` and all direct `LoadFleetConfig` call sites in command RunE functions to call `LoadCloudConfig` and extract Fleet URL/ID/token from `CloudRESTConfig.Stack.AgentManagementInstanceURL`/`AgentManagementInstanceID` and `CloudRESTConfig.Token`. Replace `FleetConfigLoader` interface with a `CloudConfigLoader` interface (method `LoadCloudConfig`) and update `NewPipelineAdapterFactory` and `NewCollectorAdapterFactory`. Update `init()` to use `&providers.ConfigLoader{}` instead of `&configLoader{}`. Change `ConfigKeys()` to return nil. Remove `Validate()` provider config check (no longer needed since fleet has no provider config keys). Remove `GRAFANA_FLEET_*` env var handling.

**Deliverables:**
- `internal/providers/fleet/provider.go` — refactored (configLoader removed, fleetHelper updated, ConfigKeys returns nil, adapter factories updated)
- `internal/providers/fleet/provider_test.go` — tests updated for new config path

**Acceptance criteria:**
- GIVEN a config file with `contexts.mystack.cloud.token` set to a valid access policy token and `contexts.mystack.grafana.server` set to `https://mystack.grafana.net`
  WHEN a user runs `gcx fleet pipelines list`
  THEN the fleet provider authenticates using the cloud token and auto-discovers the Fleet Management URL via GCOM
- GIVEN the fleet provider is refactored
  WHEN `FleetProvider.ConfigKeys()` is called
  THEN it returns nil
- GIVEN the fleet provider uses `LoadCloudConfig`
  WHEN `GRAFANA_FLEET_URL`, `GRAFANA_FLEET_INSTANCE_ID`, and `GRAFANA_FLEET_TOKEN` env vars are NOT set
  THEN the fleet provider MUST NOT read those env vars (they are removed)

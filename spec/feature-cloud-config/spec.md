---
type: feature-spec
title: "Grafana Cloud Shared Config and GCOM Discovery"
status: done
created: 2026-03-21
---

# Grafana Cloud Shared Config and GCOM Discovery

## Problem Statement

Cloud providers in grafanactl (fleet, oncall, k6) each require separate per-provider authentication configuration (URL, instance ID, token) stored under `contexts.<name>.providers.<provider>.*`. Users must manually discover service URLs and configure each provider independently. There is no mechanism to authenticate once with a Grafana Cloud access policy token and auto-discover service endpoints.

The fleet provider duplicates config-loading logic: it has its own `configLoader` struct, `LoadFleetConfig` method, and `GRAFANA_FLEET_*` env vars instead of reusing the shared `providers.ConfigLoader`. This makes onboarding new cloud providers expensive and error-prone.

**Who is affected:** Users managing Grafana Cloud stacks who need fleet, oncall, or k6 commands. Provider authors who must duplicate auth boilerplate.

**Current workaround:** Users manually look up service URLs via the GCOM API or Grafana Cloud UI and configure each provider's URL/token/instance-id separately in the config file or env vars.

## Scope

### In Scope

- New `CloudConfig` struct (token, stack slug, API URL) added to `Context`
- Environment variable support: `GRAFANA_CLOUD_TOKEN`, `GRAFANA_CLOUD_STACK`, `GRAFANA_CLOUD_API_URL`
- Stack slug derivation from `grafana.server` URL when `cloud.stack` is not explicitly set
- GCOM API client (`internal/cloud/`) for stack info discovery
- `CloudRESTConfig` type and `LoadCloudConfig` method on `providers.ConfigLoader`
- Rename `LoadRESTConfig` to `LoadGrafanaConfig` across the codebase
- Refactor fleet provider to use `LoadCloudConfig` instead of per-provider config
- Config UX via `grafanactl config set contexts.<name>.cloud.*`

### Out of Scope

- **OnCall, K6 provider migration to cloud config** — future work after fleet validates the pattern
- **GCOM client caching or retry logic** — keep initial implementation simple; add later if needed
- **Cloud config validation in `Context.Validate()`** — cloud config is optional; validation happens in `LoadCloudConfig`
- **Token refresh or OAuth flows** — access policy tokens are long-lived; no refresh needed
- **CLI commands for cloud login** — `config set` is sufficient for initial release
- **TLS configuration for GCOM client** — GCOM endpoints use public CA certificates

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| CloudConfig placement | New `Cloud *CloudConfig` field on `Context` | Parallel to `Grafana *GrafanaConfig`; both can coexist on the same context | Plan |
| Stack slug resolution | Explicit `cloud.stack` > derived from `grafana.server` URL | Explicit config always wins; URL derivation covers the common case where users already have `grafana.server` set | Plan |
| GCOM URL default | `https://grafana.com` | Production GCOM; override via `cloud.api-url` for dev/staging | Plan |
| Method rename | `LoadRESTConfig` → `LoadGrafanaConfig` | Disambiguates from `LoadCloudConfig`; "REST" was an implementation detail | Plan |
| Fleet provider consolidation | Remove custom `configLoader`, use `providers.ConfigLoader.LoadCloudConfig` | Eliminates duplicated config-loading logic; fleet becomes the reference cloud provider | Plan |
| GCOM client package location | `internal/cloud/gcom.go` | Dedicated package; will host future cloud platform clients | Plan |
| Relaxed validation for cloud config | `LoadCloudConfig` does NOT require `grafana.server` | Cloud-only contexts are valid — user may not have a Grafana instance URL | Plan |

## Functional Requirements

**FR-001:** The system MUST define a `CloudConfig` struct in `internal/config/types.go` with fields: `Token` (string, `datapolicy:"secret"`, env tag `GRAFANA_CLOUD_TOKEN`), `Stack` (string, env tag `GRAFANA_CLOUD_STACK`), `APIUrl` (string, env tag `GRAFANA_CLOUD_API_URL`).

**FR-002:** The system MUST add a `Cloud *CloudConfig` field to the `Context` struct with JSON/YAML key `"cloud"` and `omitempty` semantics.

**FR-003:** The system MUST support setting cloud config values via the existing reflect-based config editor at paths `contexts.<name>.cloud.token`, `contexts.<name>.cloud.stack`, and `contexts.<name>.cloud.api-url`.

**FR-004:** The system MUST provide a `ResolveStackSlug()` method on `Context` that returns the stack slug using this precedence: (1) `Cloud.Stack` if non-empty, (2) derived from `Grafana.Server` URL by extracting the subdomain from `*.grafana.net` or `*.grafana-dev.net` patterns, (3) empty string if neither is available.

**FR-005:** The system MUST provide a `ResolveGCOMURL()` method on `Context` that returns the GCOM API base URL: `Cloud.APIUrl` prefixed with `https://` if set, otherwise `https://grafana.com`.

**FR-006:** The system MUST implement a `GCOMClient` in `internal/cloud/` with a `GetStack(ctx, slug)` method that calls `GET /api/instances/{slug}` with Bearer token authentication and returns a `StackInfo` struct.

**FR-007:** The `StackInfo` struct MUST include at minimum: `ID`, `Slug`, `Name`, `URL`, `OrgID`, `OrgSlug`, `Status`, `RegionSlug`, and service-specific instance IDs/URLs for Prometheus, Loki, Tempo, Pyroscope, Fleet Management (Agent Management), and Alertmanager.

**FR-008:** The `GCOMClient` MUST return an error containing the HTTP status code and response body when the GCOM API returns a non-200 status.

**FR-009:** The system MUST define a `CloudRESTConfig` struct in `internal/providers/` with fields: `Token` (string), `Stack` (`cloud.StackInfo`), and `Namespace` (string).

**FR-010:** The system MUST add a `LoadCloudConfig(ctx) (CloudRESTConfig, error)` method to `providers.ConfigLoader` that: (a) loads config with env var overrides, (b) validates that `cloud.token` is set, (c) resolves stack slug via `ResolveStackSlug()`, (d) calls GCOM to discover stack info, (e) returns `CloudRESTConfig`.

**FR-011:** `LoadCloudConfig` MUST apply `GRAFANA_CLOUD_TOKEN`, `GRAFANA_CLOUD_STACK`, and `GRAFANA_CLOUD_API_URL` environment variables as overrides to config-file values, following the same precedence pattern as existing env var handling.

**FR-012:** `LoadCloudConfig` MUST NOT require `grafana.server` to be configured — a context with only `cloud.*` fields is valid for cloud provider operations.

**FR-013:** The system MUST rename `LoadRESTConfig` to `LoadGrafanaConfig` on `providers.ConfigLoader` and update all callers across the codebase.

**FR-014:** The system MUST rename the `RESTConfigLoader` interface in the incidents provider to `GrafanaConfigLoader` with the updated method name.

**FR-015:** The fleet provider MUST be refactored to use `providers.ConfigLoader.LoadCloudConfig` instead of its custom `configLoader` and `LoadFleetConfig` method.

**FR-016:** After refactoring, the fleet provider's `ConfigKeys()` MUST return nil (no per-provider config keys).

**FR-017:** After refactoring, the fleet provider MUST obtain the Fleet Management service URL and instance ID from `StackInfo.AgentManagementInstanceURL` and `StackInfo.AgentManagementInstanceID` respectively.

**FR-018:** The `CloudConfig.Token` field MUST be redacted in config output via the `datapolicy:"secret"` tag.

## Acceptance Criteria

- GIVEN a config file with `contexts.mystack.cloud.token` set to a valid access policy token and `contexts.mystack.grafana.server` set to `https://mystack.grafana.net`
  WHEN a user runs `grafanactl fleet pipelines list`
  THEN the fleet provider authenticates using the cloud token and auto-discovers the Fleet Management URL via GCOM

- GIVEN no config file
  WHEN `GRAFANA_CLOUD_TOKEN` and `GRAFANA_CLOUD_STACK` environment variables are set
  THEN `LoadCloudConfig` uses the env var values to authenticate and discover stack info

- GIVEN a context with `cloud.stack` set to `"explicit"` and `grafana.server` set to `https://derived.grafana.net`
  WHEN `ResolveStackSlug()` is called
  THEN it returns `"explicit"` (explicit value takes precedence over URL derivation)

- GIVEN a context with only `grafana.server` set to `https://mystack.grafana.net` and no `cloud.stack`
  WHEN `ResolveStackSlug()` is called
  THEN it returns `"mystack"` (derived from the URL)

- GIVEN a context with `grafana.server` set to `https://grafana.mycompany.com` (non-grafana.net domain) and no `cloud.stack`
  WHEN `ResolveStackSlug()` is called
  THEN it returns `""` (empty string)

- GIVEN a context with no `cloud.api-url` set
  WHEN `ResolveGCOMURL()` is called
  THEN it returns `"https://grafana.com"`

- GIVEN a context with `cloud.api-url` set to `"grafana-dev.com"`
  WHEN `ResolveGCOMURL()` is called
  THEN it returns `"https://grafana-dev.com"`

- GIVEN a valid GCOM token and stack slug
  WHEN `GCOMClient.GetStack(ctx, slug)` is called
  THEN it sends `GET /api/instances/{slug}` with `Authorization: Bearer {token}` header and returns populated `StackInfo`

- GIVEN a GCOM API that returns HTTP 404
  WHEN `GCOMClient.GetStack(ctx, slug)` is called
  THEN it returns an error containing the status code

- GIVEN a context with `cloud.token` not set
  WHEN `LoadCloudConfig` is called
  THEN it returns an error indicating the cloud token is missing

- GIVEN a context with `cloud.token` set but no stack slug resolvable
  WHEN `LoadCloudConfig` is called
  THEN it returns an error indicating the cloud stack is not configured

- GIVEN the `LoadCloudConfig` method exists on `providers.ConfigLoader`
  WHEN `LoadGrafanaConfig` is called (the renamed method)
  THEN it returns `config.NamespacedRESTConfig` exactly as `LoadRESTConfig` did before the rename

- GIVEN the fleet provider is refactored
  WHEN `FleetProvider.ConfigKeys()` is called
  THEN it returns nil

- GIVEN `grafanactl config set contexts.dev.cloud.token my-token` is run
  WHEN the config file is read back
  THEN `contexts.dev.cloud.token` is set to `"my-token"`

- GIVEN `grafanactl config set contexts.dev.cloud.stack mystack` is run
  WHEN the config file is read back
  THEN `contexts.dev.cloud.stack` is set to `"mystack"`

- GIVEN `grafanactl config set contexts.dev.cloud.api-url grafana-dev.com` is run
  WHEN the config file is read back
  THEN `contexts.dev.cloud.api-url` is set to `"grafana-dev.com"`

- The system SHALL compile with zero errors after the `LoadRESTConfig` → `LoadGrafanaConfig` rename across all packages

- The system SHALL pass all existing tests after the rename with no test modifications beyond updating the method name

- GIVEN the fleet provider uses `LoadCloudConfig`
  WHEN `GRAFANA_FLEET_URL`, `GRAFANA_FLEET_INSTANCE_ID`, and `GRAFANA_FLEET_TOKEN` env vars are NOT set
  THEN the fleet provider MUST NOT read those env vars (they are removed)

## Negative Constraints

- The system MUST NEVER store the `CloudConfig.Token` value in logs or non-redacted config output.
- The system MUST NEVER send the cloud token to any endpoint other than the resolved GCOM URL or service endpoint URLs obtained from the GCOM StackInfo response.
- `LoadCloudConfig` MUST NEVER fall back to `grafana.token` or `grafana.password` for cloud authentication — cloud auth uses exclusively `cloud.token`.
- The GCOM client MUST NEVER follow HTTP redirects to a different domain than the configured GCOM URL.
- The fleet provider MUST NOT retain any custom config-loading logic after refactoring — all config loading MUST go through `providers.ConfigLoader`.
- The rename of `LoadRESTConfig` MUST NOT change any runtime behavior — it is a pure rename refactor.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Rename touches ~42 files / ~89 occurrences | High chance of merge conflicts with in-flight PRs | Execute rename as a single atomic commit; coordinate with active branches |
| GCOM API rate limits or availability | `LoadCloudConfig` fails if GCOM is unreachable | Clear error messages; future work: cache `StackInfo` locally |
| Fleet provider regressions | Fleet commands break after refactoring | Preserve existing fleet test coverage; add integration tests with mock GCOM |
| `*.grafana-dev.net` URL pattern changes | Stack slug derivation breaks for dev environments | Explicit `cloud.stack` config always available as override |
| Cloud-only context (no `grafana.server`) breaks existing validation | Commands that assume `grafana.server` is always present fail | `LoadCloudConfig` uses relaxed validation; `LoadGrafanaConfig` validation unchanged |

## Open Questions

- [DEFERRED] Will OnCall and K6 providers migrate to `LoadCloudConfig` in this release or a subsequent one? — Deferred to subsequent work after fleet validates the pattern.
- [RESOLVED] Should `CloudConfig` and `GrafanaConfig` be mutually exclusive on a context? — No, both can coexist. A context may target both Grafana instance APIs (via `grafana.*`) and cloud platform APIs (via `cloud.*`).
- [RESOLVED] Should the GCOM client support pagination or listing? — No, only `GetStack` by slug is needed for config discovery.

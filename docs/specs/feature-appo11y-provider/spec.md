---
type: feature-spec
title: "App O11y Provider"
status: done
parent: docs/adrs/appo11y-provider/
beads_id: gcx-3c3403a9
created: 2026-03-30
---

# App O11y Provider

## Problem Statement

Grafana App Observability exposes two singleton configuration resources — **Overrides** (metrics generator settings) and **Settings** (plugin-level config) — through plugin proxy REST endpoints. Currently, these resources can only be managed via `grafana-cloud-cli`'s `app-o11y` command group. There is no way to manage them through `gcx`, which means users cannot leverage gcx's unified resource model, output formatting, or generic resource path (`gcx resources get ...`) for App O11y configuration.

The workaround today is to use `grafana-cloud-cli` directly or make raw HTTP calls to the plugin proxy endpoints. Neither integrates with gcx's context/config system, output codecs, or resource pipeline.

## Scope

### In Scope

- New `appo11y` provider package at `internal/providers/appo11y/` following the SLO provider pattern
- Two singleton resource kinds: **Overrides** (`overrides.v1alpha1.appo11y.ext.grafana.app`) and **Settings** (`settings.v1alpha1.appo11y.ext.grafana.app`)
- TypedCRUD resource adapters with `GetFn` and `UpdateFn` only (no List, Create, Delete)
- Provider CLI commands: `gcx appo11y overrides get`, `gcx appo11y overrides update -f`, `gcx appo11y settings get`, `gcx appo11y settings update -f`
- Generic resource path access via `gcx resources get` and `gcx resources schemas`/`examples`
- ETag-based optimistic concurrency for Overrides (stored as `appo11y.ext.grafana.app/etag` annotation)
- Table, wide, JSON, and YAML output formats for both kinds
- Go types mirroring the source `MetricsGeneratorConfig` and `PluginSettings` structs
- Provider self-registration via `init()` + `providers.Register()`
- HTTP client creation via `rest.HTTPClientFor()`
- Unit tests for adapter logic, type conversion, and client HTTP interactions

### Out of Scope

- **Migration tooling** from `grafana-cloud-cli` — handled by a separate migration spec
- **List semantics** — both resources are singletons; there is no collection endpoint
- **Create / Delete operations** — the API does not support resource lifecycle; only get and update exist
- **Custom authentication** — the provider uses the standard Grafana SA token; no additional auth config is needed
- **Validation of config values** — the provider passes config to the API as-is; server-side validation applies
- **Prometheus metrics or telemetry** — not part of initial provider implementation

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Singleton resources with hardcoded name `"default"` | TypedCRUD adapter with only `GetFn`/`UpdateFn` | API exposes exactly one instance per kind; no list/create/delete endpoints exist | ADR |
| ETag stored as K8s annotation | `appo11y.ext.grafana.app/etag` annotation in metadata | Survives K8s envelope round-trips; same pattern used by other providers with optimistic concurrency | ADR |
| Command name `appo11y` (no hyphen) | Single word, no separator | Consistent with gcx naming conventions; avoids shell quoting issues | ADR |
| Per-kind subpackages | `overrides/` and `settings/` under `internal/providers/appo11y/` | Follows established SLO provider pattern; isolates type definitions and adapter logic per resource kind | ADR |
| Standard adapter verbs only | `get` and `update` | Maps directly to available API operations (GET and POST); no other verbs are meaningful for these singletons | ADR |
| No custom ConfigKeys or Validate | `ConfigKeys() = nil`, `Validate() = nil` | Uses standard Grafana SA token auth; no provider-specific config needed | ADR |

## Functional Requirements

**FR-001**: The provider MUST register itself via `init()` calling `providers.Register()` with the name `"appo11y"` and group `"appo11y.ext.grafana.app"`.

**FR-002**: The provider MUST expose two resource kinds — `Overrides` and `Settings` — both in API group `appo11y.ext.grafana.app` at version `v1alpha1`.

**FR-003**: Both resource adapters MUST implement `GetFn` that returns a single resource wrapped in a K8s-style envelope with `apiVersion`, `kind`, `metadata` (including `name: "default"`), and `spec`.

**FR-004**: Both resource adapters MUST implement `UpdateFn` that accepts a K8s-envelope resource read from a file (`-f <file>`), extracts the spec, and POSTs it to the corresponding API endpoint.

**FR-005**: The Overrides adapter MUST send the `If-Match` HTTP header with the ETag value from the `appo11y.ext.grafana.app/etag` annotation when performing an update.

**FR-006**: The Overrides adapter MUST store the ETag returned by the GET response in the `appo11y.ext.grafana.app/etag` annotation on the resource metadata.

**FR-007**: The `ListFn`, `CreateFn`, and `DeleteFn` fields MUST be nil for both adapters.

**FR-008**: The provider MUST create its HTTP client using `rest.HTTPClientFor()` from the gcx config system.

**FR-009**: The Overrides client MUST call `GET /api/plugin-proxy/grafana-app-observability-app/overrides` and deserialize the response body directly into `MetricsGeneratorConfig`.

**FR-010**: The Settings client MUST call `GET /api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` and deserialize the response body directly into `PluginSettings`.

**FR-011**: The Overrides client MUST call `POST /api/plugin-proxy/grafana-app-observability-app/overrides` for updates and check status only (discard response body).

**FR-012**: The Settings client MUST call `POST /api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` for updates and check status only (discard response body).

**FR-013**: The `gcx appo11y overrides get` command MUST support `-o json`, `-o yaml`, `-o table`, and `-o wide` output formats.

**FR-014**: The `gcx appo11y settings get` command MUST support `-o json`, `-o yaml`, `-o table`, and `-o wide` output formats.

**FR-015**: Overrides table output MUST display columns: NAME, COLLECTION, INTERVAL, SERVICE GRAPHS, SPAN METRICS.

**FR-016**: Overrides wide output MUST display the table columns plus: SG DIMENSIONS, SM DIMENSIONS.

**FR-017**: Settings table output MUST display columns: NAME, LOG QUERY MODE, METRICS MODE.

**FR-018**: Settings wide output MUST display the table columns plus: LOGS QUERY (NS), LOGS QUERY (NO NS).

**FR-019**: Both resources MUST be accessible via the generic resource path: `gcx resources get {kind}.v1alpha1.appo11y.ext.grafana.app/default`.

**FR-020**: Both resources MUST appear in `gcx resources schemas` and `gcx resources examples` output.

**FR-021**: The provider MUST follow the Options pattern: opts struct with `setup(flags)`, `Validate()`, and constructor function for each command.

**FR-022**: The provider MUST use `cmdio.Options` for output codec selection.

**FR-023**: The provider MUST use `ConfigLoader` for `--config`/`--context` flag handling.

**FR-024**: The Go types for `MetricsGeneratorConfig` MUST include: `CostAttribution map[string]any`, `MetricsGenerator` with `DisableCollection bool`, `CollectionInterval string`, `Processor` with `ServiceGraphs` and `SpanMetrics` sub-structs matching the source schema.

**FR-025**: The Go types for `PluginSettings` MUST include: `JSONData` with fields `DefaultLogQueryMode`, `LogsQueryWithNamespace`, `LogsQueryWithoutNamespace`, and `MetricsMode` (all `string`).

**FR-026**: The provider package MUST be organized as:
```
internal/providers/appo11y/
├── provider.go
├── client.go
├── overrides/
│   ├── types.go
│   ├── adapter.go
│   ├── resource_adapter.go
│   └── commands.go
└── settings/
    ├── types.go
    ├── adapter.go
    ├── resource_adapter.go
    └── commands.go
```

## Acceptance Criteria

### Provider Registration

- GIVEN gcx is built with the appo11y provider package imported
  WHEN the binary starts
  THEN the provider "appo11y" MUST be registered in the provider registry with group "appo11y.ext.grafana.app"

- GIVEN the appo11y provider is registered
  WHEN `gcx providers list` is executed
  THEN the output MUST include an entry for "appo11y"

### Overrides Get

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y overrides get` is executed
  THEN the CLI MUST issue `GET /api/plugin-proxy/grafana-app-observability-app/overrides` and display the result in table format with columns NAME, COLLECTION, INTERVAL, SERVICE GRAPHS, SPAN METRICS

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y overrides get -o json` is executed
  THEN the output MUST be a JSON object with `apiVersion: "appo11y.ext.grafana.app/v1alpha1"`, `kind: "Overrides"`, `metadata.name: "default"`, and `spec` containing the full `MetricsGeneratorConfig`

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y overrides get -o yaml` is executed
  THEN the output MUST be a YAML document with the same envelope structure as JSON

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y overrides get -o wide` is executed
  THEN the table MUST include additional columns SG DIMENSIONS and SM DIMENSIONS

- GIVEN the API returns an ETag header in the GET overrides response
  WHEN the response is converted to a K8s envelope
  THEN the `metadata.annotations` MUST contain key `appo11y.ext.grafana.app/etag` with the ETag value

### Overrides Update

- GIVEN a YAML/JSON file containing a valid Overrides resource with an ETag annotation
  WHEN `gcx appo11y overrides update -f overrides.yaml` is executed
  THEN the CLI MUST issue `POST /api/plugin-proxy/grafana-app-observability-app/overrides` with the spec as the request body and `If-Match: <etag>` header

- GIVEN a YAML/JSON file containing a valid Overrides resource without an ETag annotation
  WHEN `gcx appo11y overrides update -f overrides.yaml` is executed
  THEN the CLI MUST issue the POST request without an `If-Match` header

- GIVEN the API returns HTTP 412 Precondition Failed on overrides update
  WHEN the response is received
  THEN the CLI MUST report an error indicating a concurrent modification conflict

### Settings Get

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y settings get` is executed
  THEN the CLI MUST issue `GET /api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` and display the result in table format with columns NAME, LOG QUERY MODE, METRICS MODE

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y settings get -o json` is executed
  THEN the output MUST be a JSON object with `apiVersion: "appo11y.ext.grafana.app/v1alpha1"`, `kind: "Settings"`, `metadata.name: "default"`, and `spec` containing the full `PluginSettings`

- GIVEN a valid Grafana context with SA token
  WHEN `gcx appo11y settings get -o wide` is executed
  THEN the table MUST include additional columns LOGS QUERY (NS) and LOGS QUERY (NO NS)

### Settings Update

- GIVEN a YAML/JSON file containing a valid Settings resource
  WHEN `gcx appo11y settings update -f settings.yaml` is executed
  THEN the CLI MUST issue `POST /api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` with the spec as the request body and no `If-Match` header

### Generic Resource Path

- GIVEN the appo11y provider is registered
  WHEN `gcx resources get overrides.v1alpha1.appo11y.ext.grafana.app/default` is executed
  THEN the result MUST be identical to `gcx appo11y overrides get -o json`

- GIVEN the appo11y provider is registered
  WHEN `gcx resources get settings.v1alpha1.appo11y.ext.grafana.app/default` is executed
  THEN the result MUST be identical to `gcx appo11y settings get -o json`

### Schema and Examples

- GIVEN the appo11y provider is registered
  WHEN `gcx resources schemas` is executed
  THEN the output MUST include schemas for both `Overrides` and `Settings` kinds in group `appo11y.ext.grafana.app`

- GIVEN the appo11y provider is registered
  WHEN `gcx resources examples` is executed
  THEN the output MUST include example resources for both `Overrides` and `Settings` kinds

### Unsupported Operations

- GIVEN the appo11y provider is registered
  WHEN a user attempts to list, create, or delete an appo11y resource
  THEN the CLI MUST return an error indicating the operation is not supported for this resource kind

## Negative Constraints

- **NC-001**: The provider MUST NEVER expose raw HTTP response bodies or stack traces to the user. All API errors MUST be translated to user-friendly messages.

- **NC-002**: The provider MUST NEVER send an `If-Match` header for Settings updates. Only Overrides uses ETag concurrency.

- **NC-003**: The provider MUST NEVER implement `ListFn`, `CreateFn`, or `DeleteFn` for either resource kind.

- **NC-004**: The provider MUST NEVER require provider-specific configuration keys. `ConfigKeys()` MUST return nil.

- **NC-005**: The provider MUST NEVER use a hardcoded Grafana URL or token. All connection details MUST come from the gcx config context system.

- **NC-006**: The provider MUST NEVER deserialize the POST response body for update operations. Status code check only.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Plugin proxy endpoints change path or response format in future Grafana versions | Provider breaks silently or returns malformed data | Pin to `v1alpha1`; version the API group; add integration tests against a real Grafana instance in CI |
| ETag header name varies across Grafana versions or reverse proxies | Optimistic concurrency silently disabled | Document expected header name (`ETag`); log a warning if ETag is absent from GET response |
| Singleton "default" name collides with future multi-instance support | Breaking change to resource identity | The `v1alpha1` version signals instability; future multi-instance support would use `v1beta1` or `v1` |
| App Observability plugin not installed on target Grafana instance | Confusing 404 or proxy error | Translate 404 from plugin proxy into a clear error: "Grafana App Observability plugin is not installed or not enabled" |

## Open Questions

- [RESOLVED] Command group name: `appo11y` (no hyphen) — decided in ADR.
- [RESOLVED] Adapter pattern: TypedCRUD with GetFn/UpdateFn only — decided in ADR.
- [RESOLVED] Diff support: handled by `resources diff` (generic pipeline), no provider-specific command needed.
- [RESOLVED] Push/pull pipeline compatibility: investigated the pusher/puller code paths.
  `resources pull .../default` works (FilterTypeSingle → GetFn). `resources push -f` works
  (upsertResource → Get succeeds → Update path). Bulk `resources pull` (no selector) silently
  skips singletons (ListFn=nil → ErrUnsupported → warning log). This is correct behavior —
  singleton configs should not be bulk-pulled. No framework changes needed.

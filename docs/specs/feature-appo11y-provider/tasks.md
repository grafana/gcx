---
type: feature-tasks
title: "App O11y Provider"
status: draft
spec: docs/specs/feature-appo11y-provider/spec.md
plan: docs/specs/feature-appo11y-provider/plan.md
created: 2026-03-30
---

# Implementation Tasks

## Dependency Graph

```
T1 (types + client + provider shell)
├──► T2 (overrides subpackage: adapter, commands, table codecs)
├──► T3 (settings subpackage: adapter, commands, table codecs)
└──► T4 (registration wiring + integration verification)
      depends on T2, T3
```

## Wave 1: Foundation

### T1: Provider shell, shared HTTP client, and Go types
**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Create the `internal/providers/appo11y/` package with the provider struct, shared HTTP client, and Go types for both resource kinds. The provider implements `providers.Provider` with `Name() = "appo11y"`, `ShortDesc()`, `ConfigKeys() = nil`, `Validate() = nil`. The client struct wraps `*http.Client` and exposes four methods: `GetOverrides`, `UpdateOverrides`, `GetSettings`, `UpdateSettings`. Go types mirror the source schema: `MetricsGeneratorConfig` (with unexported `etag` field, `CostAttribution`, `MetricsGenerator` with `DisableCollection`, `CollectionInterval`, `Processor` with `ServiceGraphs` and `SpanMetrics` sub-structs) and `PluginSettings` (with `JSONData` containing `DefaultLogQueryMode`, `LogsQueryWithNamespace`, `LogsQueryWithoutNamespace`, `MetricsMode`). Both types implement `ResourceNamer` with `GetResourceName() = "default"` and `SetResourceName()` (no-op). Include unit tests for client HTTP interactions using `httptest.Server`.

**Deliverables:**
- `internal/providers/appo11y/provider.go`
- `internal/providers/appo11y/client.go`
- `internal/providers/appo11y/client_test.go`
- `internal/providers/appo11y/overrides/types.go`
- `internal/providers/appo11y/settings/types.go`

**Acceptance criteria:**
- GIVEN the client is constructed with a valid config
  WHEN `GetOverrides` is called against an httptest server returning a JSON `MetricsGeneratorConfig` with an `ETag` header
  THEN the returned struct MUST contain the deserialized config and the unexported `etag` field MUST hold the ETag value

- GIVEN the client is constructed with a valid config
  WHEN `UpdateOverrides` is called with a config and a non-empty etag string
  THEN the HTTP request MUST be a POST to `/api/plugin-proxy/grafana-app-observability-app/overrides` with `If-Match` header set to the etag value

- GIVEN the client is constructed with a valid config
  WHEN `UpdateOverrides` is called with an empty etag string
  THEN the HTTP request MUST NOT include an `If-Match` header

- GIVEN the client is constructed with a valid config
  WHEN `GetSettings` is called against an httptest server returning a JSON `PluginSettings`
  THEN the returned struct MUST contain the deserialized settings

- GIVEN the client is constructed with a valid config
  WHEN `UpdateSettings` is called with a settings struct
  THEN the HTTP request MUST be a POST to `/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` with no `If-Match` header

- GIVEN the API returns HTTP 404
  WHEN any client method is called
  THEN the error message MUST contain "Grafana App Observability plugin is not installed or not enabled"

- GIVEN the API returns HTTP 412 on UpdateOverrides
  WHEN the response is received
  THEN the error message MUST contain "concurrent modification conflict"

---

## Wave 2: Resource subpackages

### T2: Overrides subpackage — adapter, resource_adapter, commands, table codec
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the `overrides/` subpackage with: (1) `resource_adapter.go` containing `StaticDescriptor()`, `OverridesSchema()`, `OverridesExample()`, `NewTypedCRUD()`, and `NewLazyFactory()`; (2) `adapter.go` with `ToResource`/`FromResource` conversion helpers; (3) `commands.go` with `Commands()` returning the `overrides` cobra command group containing `get` and `update` subcommands; (4) table and wide codecs. The TypedCRUD wires `GetFn` to populate the ETag annotation via `MetadataFn`, and `UpdateFn` to extract the ETag annotation from the input resource and pass it to `client.UpdateOverrides`. `ListFn`, `CreateFn`, `DeleteFn` are nil. The `get` command supports `-o table|wide|json|yaml`. The `update` command accepts `-f <file>`. Include unit tests for adapter logic and table codec output.

**Deliverables:**
- `internal/providers/appo11y/overrides/adapter.go`
- `internal/providers/appo11y/overrides/resource_adapter.go`
- `internal/providers/appo11y/overrides/commands.go`
- `internal/providers/appo11y/overrides/adapter_test.go`
- `internal/providers/appo11y/overrides/commands_test.go`

**Acceptance criteria:**
- GIVEN a valid Grafana context
  WHEN `NewTypedCRUD` is called and the resulting CRUD's `GetFn` is invoked
  THEN the returned `TypedObject` MUST have `apiVersion: "appo11y.ext.grafana.app/v1alpha1"`, `kind: "Overrides"`, `metadata.name: "default"`, and `metadata.annotations["appo11y.ext.grafana.app/etag"]` set to the ETag from the HTTP response

- GIVEN a TypedObject with an ETag annotation
  WHEN the CRUD's `UpdateFn` is invoked
  THEN the client MUST receive the ETag value for the `If-Match` header

- GIVEN a TypedObject without an ETag annotation
  WHEN the CRUD's `UpdateFn` is invoked
  THEN the client MUST be called with an empty etag (no `If-Match` header)

- GIVEN the overrides `get` command is invoked with `-o table`
  WHEN the output is rendered
  THEN the table MUST display columns NAME, COLLECTION, INTERVAL, SERVICE GRAPHS, SPAN METRICS

- GIVEN the overrides `get` command is invoked with `-o wide`
  WHEN the output is rendered
  THEN the table MUST display the table columns plus SG DIMENSIONS and SM DIMENSIONS

- GIVEN the overrides `get` command is invoked with `-o json`
  WHEN the output is rendered
  THEN the output MUST be a JSON object with the full K8s envelope including `spec` containing `MetricsGeneratorConfig`

- GIVEN the overrides `get` command is invoked with `-o yaml`
  WHEN the output is rendered
  THEN the output MUST be a YAML document with the same envelope structure as JSON

- GIVEN `ListFn`, `CreateFn`, and `DeleteFn` on the overrides TypedCRUD
  WHEN inspected
  THEN all three MUST be nil

---

### T3: Settings subpackage — adapter, resource_adapter, commands, table codec
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the `settings/` subpackage with the same structure as overrides but simpler (no ETag). `resource_adapter.go` with `StaticDescriptor()`, `SettingsSchema()`, `SettingsExample()`, `NewTypedCRUD()`, `NewLazyFactory()`. `adapter.go` with `ToResource`/`FromResource`. `commands.go` with `get` and `update` subcommands. Table codec shows NAME, LOG QUERY MODE, METRICS MODE; wide adds LOGS QUERY (NS), LOGS QUERY (NO NS). No `MetadataFn` needed (no ETag). `ListFn`, `CreateFn`, `DeleteFn` are nil. Include unit tests.

**Deliverables:**
- `internal/providers/appo11y/settings/adapter.go`
- `internal/providers/appo11y/settings/resource_adapter.go`
- `internal/providers/appo11y/settings/commands.go`
- `internal/providers/appo11y/settings/adapter_test.go`
- `internal/providers/appo11y/settings/commands_test.go`

**Acceptance criteria:**
- GIVEN a valid Grafana context
  WHEN `NewTypedCRUD` is called and the resulting CRUD's `GetFn` is invoked
  THEN the returned `TypedObject` MUST have `apiVersion: "appo11y.ext.grafana.app/v1alpha1"`, `kind: "Settings"`, `metadata.name: "default"`, and `spec` containing the full `PluginSettings`

- GIVEN the settings `get` command is invoked with `-o table`
  WHEN the output is rendered
  THEN the table MUST display columns NAME, LOG QUERY MODE, METRICS MODE

- GIVEN the settings `get` command is invoked with `-o wide`
  WHEN the output is rendered
  THEN the table MUST display the table columns plus LOGS QUERY (NS) and LOGS QUERY (NO NS)

- GIVEN the settings `get` command is invoked with `-o json`
  WHEN the output is rendered
  THEN the output MUST be a JSON object with the full K8s envelope

- GIVEN the settings `update -f` command is invoked with a valid settings file
  WHEN the update executes
  THEN the CLI MUST POST to `/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` with the spec as body and MUST NOT send an `If-Match` header

- GIVEN `ListFn`, `CreateFn`, and `DeleteFn` on the settings TypedCRUD
  WHEN inspected
  THEN all three MUST be nil

---

## Wave 3: Wiring and integration verification

### T4: Provider registration, blank import, TypedRegistrations, and verification
**Priority**: P0
**Effort**: Small
**Depends on**: T2, T3
**Type**: task

Wire the provider into gcx: (1) Complete `provider.go` with `Commands()` returning the `appo11y` command group containing overrides and settings subcommands, and `TypedRegistrations()` returning registrations for both kinds with schemas and examples. (2) Add blank import `_ "github.com/grafana/gcx/internal/providers/appo11y"` to `cmd/gcx/root/command.go`. (3) Add a provider registration test verifying the provider appears in the registry. (4) Verify `make lint` and `make tests` pass.

**Deliverables:**
- `internal/providers/appo11y/provider.go` (updated with Commands + TypedRegistrations)
- `cmd/gcx/root/command.go` (add blank import)
- `internal/providers/appo11y/provider_test.go`

**Acceptance criteria:**
- GIVEN gcx is built with the appo11y provider imported
  WHEN the binary starts
  THEN the provider "appo11y" MUST be registered in the provider registry with group "appo11y.ext.grafana.app"

- GIVEN the appo11y provider is registered
  WHEN `TypedRegistrations()` is called
  THEN it MUST return two registrations: one for Overrides (kind "Overrides", group "appo11y.ext.grafana.app", version "v1alpha1") and one for Settings (kind "Settings", same group and version), each with non-nil Schema and Example

- GIVEN the appo11y provider is registered
  WHEN a user attempts to list, create, or delete an appo11y resource via the generic adapter
  THEN the adapter MUST return `errors.ErrUnsupported`

- GIVEN all provider code is in place
  WHEN `make lint` is executed
  THEN it MUST pass with no errors

- GIVEN all provider code is in place
  WHEN `make tests` is executed
  THEN all tests MUST pass including the new appo11y tests

---
type: feature-tasks
title: "Fold Provider CRUD into Resources Subcommand"
status: draft
spec: spec/feature-fold-provider-crud-into-resources/spec.md
plan: spec/feature-fold-provider-crud-into-resources/plan.md
created: 2026-03-10
---

# Implementation Tasks

## Dependency Graph

```
T1 (ResourceAdapter interface + Descriptor registration)
├──→ T2 (SLO adapter implementation)
├──→ T3 (Synth adapter implementation)
├──→ T4 (Alert adapter implementation)
└──→ T5 (ResourceClientRouter)
         │
         ▼
     T6 (Wire router into Pusher/Puller/Deleter + FetchResources)
         │
         ▼
     T7 (Deprecation warnings on provider commands)
         │
         ▼
     T8 (Integration tests + docs)
```

## Wave 1: Foundation

### T1: ResourceAdapter Interface and Static Descriptor Registration

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Define the `ResourceAdapter` interface in a new `internal/resources/adapter/` package. The interface MUST expose `List`, `Get`, `Create`, `Update`, and `Delete` methods matching the signatures specified in FR-001. Add a `Descriptor()` method that returns the `resources.Descriptor` the adapter serves, plus an `Aliases() []string` method for short name registration. Define an `adapter.Factory` type (a closure returning a `ResourceAdapter` and error) for lazy initialization.

Extend the `Provider` interface in `internal/providers/provider.go` with a `ResourceAdapters() []adapter.Factory` method. Providers that support resource adapters return their factories; others return `nil`. Update the root registration loop in `cmd/grafanactl/root/command.go` to call `Registry.RegisterAdapter()` for each factory returned by providers.

Extend `RegistryIndex` with a `RegisterStatic(desc resources.Descriptor, aliases []string)` method that injects a provider descriptor into the existing index maps (`kindNames`, `singularNames`, `pluralNames`, `shortGroups`, `longGroups`, `preferredVersions`, `descriptors`). This enables `LookupPartialGVK` to resolve provider types without any parser changes.

Add a `Registry.RegisterAdapter(factory adapter.Factory)` method on the discovery `Registry` that calls `RegisterStatic` and also stores the factory for later retrieval by GVK. Add a `Registry.GetAdapter(gvk) (adapter.Factory, bool)` lookup method.

**Deliverables:**
- `internal/resources/adapter/adapter.go` — `ResourceAdapter` interface + `Factory` type
- `internal/providers/provider.go` — `Provider` interface extended with `ResourceAdapters()` method
- `cmd/grafanactl/root/command.go` — registration loop updated to register adapter factories
- `internal/resources/discovery/registry_index.go` — `RegisterStatic` method added
- `internal/resources/discovery/registry.go` — `RegisterAdapter`, `GetAdapter` methods added
- `internal/resources/adapter/adapter_test.go` — interface compliance tests
- `internal/resources/discovery/registry_index_test.go` — tests for `RegisterStatic`

**Acceptance criteria:**
- GIVEN a static descriptor with aliases `["slo"]` and kind `SLO` in group `slo.ext.grafana.app` WHEN `RegisterStatic` is called THEN `LookupPartialGVK` with `Resource: "slo"` returns that descriptor
- GIVEN a static descriptor registered via `RegisterStatic` WHEN `GetDescriptors()` is called THEN the static descriptor appears alongside dynamically discovered descriptors
- GIVEN a static descriptor registered via `RegisterStatic` WHEN `GetPreferredVersions()` is called THEN the static descriptor appears in the preferred versions list
- GIVEN no static descriptors registered WHEN `LookupPartialGVK` is called with a native resource name THEN behavior is identical to current (no regression)

---

## Wave 2: Provider Adapter Implementations

### T2: SLO ResourceAdapter Implementation

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement `ResourceAdapter` for SLO definitions by wrapping the existing `definitions.Client`. The adapter MUST use the existing `ToResource`/`FromResource` functions to marshal between `Slo` typed structs and `unstructured.Unstructured`. The adapter factory MUST accept a `config.NamespacedRESTConfig` and create the `definitions.Client` internally (FR-015). Register the static descriptor with group `slo.ext.grafana.app`, version `v1alpha1`, kind `SLO`, singular `slo`, plural `slos`, aliases `["slo"]`.

**Deliverables:**
- `internal/providers/slo/definitions/resource_adapter.go` — `ResourceAdapter` implementation
- `internal/providers/slo/definitions/resource_adapter_test.go` — unit tests with round-trip verification
- `internal/providers/slo/provider.go` — `ResourceAdapters()` method returning SLO definitions adapter factory

**Acceptance criteria:**
- GIVEN an SLO adapter created with valid config WHEN `List` is called THEN it returns `[]*Resource` objects with `apiVersion: slo.ext.grafana.app/v1alpha1`, `kind: SLO`, and populated `metadata` and `spec` fields
- GIVEN an SLO adapter WHEN `Get(ctx, "abc-123")` is called THEN it returns a single `*Resource` with `metadata.name: abc-123`
- GIVEN a `*Resource` with SLO spec WHEN `Create` is called THEN the adapter converts it via `FromResource` and calls `Client.Create`
- GIVEN a `*Resource` with SLO spec WHEN `Update` is called THEN the adapter converts it via `FromResource` and calls `Client.Update`
- GIVEN an SLO name WHEN `Delete` is called THEN the adapter calls `Client.Delete` with that name
- GIVEN the SLO provider package is imported WHEN the process starts THEN the static descriptor is registered in the discovery registry with alias `slo`

---

### T3: Synth ResourceAdapter Implementation

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement `ResourceAdapter` for Synthetic Monitoring checks and probes. The checks adapter wraps `checks.Client`; the probes adapter wraps `probes.Client`. The adapter factory MUST use `smcfg.LoadSMConfig` for auth (FR-015) instead of `NamespacedRESTConfig`. Register static descriptors: checks with group `syntheticmonitoring.ext.grafana.app`, version `v1alpha1`, kind `Check`, aliases `["checks"]`; probes with same group, kind `Probe`, aliases `["probes"]`.

**Deliverables:**
- `internal/providers/synth/checks/resource_adapter.go` — checks `ResourceAdapter` implementation
- `internal/providers/synth/checks/resource_adapter_test.go` — unit tests
- `internal/providers/synth/probes/resource_adapter.go` — probes `ResourceAdapter` implementation
- `internal/providers/synth/probes/resource_adapter_test.go` — unit tests
- `internal/providers/synth/provider.go` — `ResourceAdapters()` method returning checks + probes adapter factories

**Acceptance criteria:**
- GIVEN a Synth checks adapter WHEN `List` is called THEN it returns `[]*Resource` objects with `apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1` and `kind: Check`
- GIVEN a Synth checks adapter factory WHEN the SM config is not set THEN the factory is NOT invoked (lazy init) and `grafanactl resources get dashboards` succeeds without error (FR-016)
- GIVEN the Synth provider package is imported WHEN the process starts THEN descriptors for `Check` and `Probe` are registered with aliases `checks` and `probes`

---

### T4: Alert ResourceAdapter Implementation

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement `ResourceAdapter` for alert rules and groups. The alert provider currently has no `ToResource`/`FromResource` adapters, so these MUST be created. Rules adapter wraps `alert.Client`; groups adapter wraps `alert.Client`. Register static descriptors: rules with group `alerting.ext.grafana.app`, version `v1alpha1`, kind `AlertRule`, aliases `["rules"]`; groups with same group, kind `AlertRuleGroup`, aliases `["groups"]`.

Note: The existing alert `Client` is read-only (List/Get only, no Create/Update/Delete). The adapter MUST return `errors.ErrUnsupported` for `Create`, `Update`, and `Delete` operations until the alert API supports mutations. This is consistent with FR-001 (the interface exists) while being honest about current API limitations.

**Deliverables:**
- `internal/providers/alert/adapter.go` — `ToResource`/`FromResource` for rules and groups
- `internal/providers/alert/adapter_test.go` — round-trip tests
- `internal/providers/alert/resource_adapter.go` — `ResourceAdapter` for rules and groups
- `internal/providers/alert/resource_adapter_test.go` — unit tests
- `internal/providers/alert/provider.go` — `ResourceAdapters()` method returning rules + groups adapter factories

**Acceptance criteria:**
- GIVEN an alert rules adapter WHEN `List` is called THEN it returns `[]*Resource` objects with `apiVersion: alerting.ext.grafana.app/v1alpha1` and `kind: AlertRule`
- GIVEN an alert groups adapter WHEN `List` is called THEN it returns `[]*Resource` objects with kind `AlertRuleGroup`
- GIVEN an alert rules adapter WHEN `Create` is called THEN it returns `errors.ErrUnsupported`
- GIVEN the alert provider package is imported WHEN the process starts THEN descriptors for `AlertRule` and `AlertRuleGroup` are registered with aliases `rules` and `groups`

---

## Wave 3: Router and Pipeline Integration

### T5: ResourceClientRouter

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create a `ResourceClientRouter` in `internal/resources/adapter/` that implements `remote.PushClient`, `remote.PullClient`, and `remote.DeleteClient`. The router holds a reference to the existing `dynamic` K8s client and a map of `schema.GroupVersionKind → ResourceAdapter`. For each operation, it checks whether the descriptor's GVK maps to a registered adapter; if so, it delegates to the adapter. Otherwise, it delegates to the dynamic client.

The router MUST use lazy initialization for adapters: it stores adapter factories (func closures) and only invokes them on first access for a given GVK (FR-016). Adapter instances MUST be cached after first creation. Adapter instances MUST NOT be stored as package-level globals.

**Deliverables:**
- `internal/resources/adapter/router.go` — `ResourceClientRouter` implementation
- `internal/resources/adapter/router_test.go` — unit tests with mock adapters and mock dynamic client

**Acceptance criteria:**
- GIVEN a router with an SLO adapter registered WHEN `List` is called with an SLO descriptor THEN the SLO adapter's `List` is invoked
- GIVEN a router with an SLO adapter registered WHEN `List` is called with a dashboard descriptor THEN the dynamic client's `List` is invoked
- GIVEN a router with a Synth adapter factory that panics WHEN `List` is called with a dashboard descriptor THEN the Synth factory is never invoked (lazy init)
- GIVEN a router WHEN `List` is called twice for the same provider GVK THEN the adapter factory is invoked only once (caching)

---

### T6: Wire Router into Resource Command Pipeline

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T2, T3, T4, T5
**Type**: task

Modify `FetchResources`, `NewDefaultPusher`, `NewDefaultPuller`, and `NewDeleter` (or their call sites in `cmd/grafanactl/resources/`) to construct a `ResourceClientRouter` that wraps the dynamic client and all registered adapters. The router MUST be passed as the client to `Pusher`, `Puller`, and `Deleter`.

Modify the `resources list` command to merge provider descriptors into its output (FR-013). The `Registry` already returns provider descriptors via `SupportedResources()` after T1, so this requires ensuring the `list` command uses the enhanced registry.

Modify the `resources edit` command to use the router for get-modify-put on provider resources (FR-018).

Ensure `resources push` with no type argument auto-detects provider types from YAML files (FR-017): this works automatically because the router's `supportedDescriptors()` returns the merged set and `pushSingleResource` checks GVK membership.

**Deliverables:**
- `cmd/grafanactl/resources/fetch.go` — updated to use `ResourceClientRouter`
- `cmd/grafanactl/resources/push.go` — updated to use `ResourceClientRouter`
- `cmd/grafanactl/resources/pull.go` — updated to use `ResourceClientRouter` (if `NewDefaultPuller` call site needs changes)
- `cmd/grafanactl/resources/delete.go` — updated to use `ResourceClientRouter`
- `cmd/grafanactl/resources/edit.go` — updated to use `ResourceClientRouter`
- `cmd/grafanactl/resources/list.go` — verified to include provider descriptors

**Acceptance criteria:**
- GIVEN a Grafana instance with SLO definitions WHEN the user runs `grafanactl resources list slo` THEN the output displays SLO definitions in the same tabular format as native resources (NAME, NAMESPACE, KIND, AGE columns)
- GIVEN a Grafana instance with SLO definitions WHEN the user runs `grafanactl resources get slo/<uuid>` THEN the output displays the SLO definition as a YAML-encoded object with `apiVersion: slo.ext.grafana.app/v1alpha1`, `kind: SLO`, `metadata`, and `spec` fields
- GIVEN SLO definition YAML files on disk WHEN the user runs `grafanactl resources push slo -p ./slo-defs/` THEN the SLO definitions are created or updated via the SLO REST API and a summary reports the count of pushed resources
- GIVEN a Grafana instance with SLO definitions WHEN the user runs `grafanactl resources pull slo -d ./output/` THEN SLO definitions are written to `./output/` as YAML files with the standard resource envelope
- GIVEN a Grafana instance with an SLO definition named `abc-123` WHEN the user runs `grafanactl resources delete slo/abc-123` THEN the SLO definition is deleted via the SLO REST API
- GIVEN a Grafana instance with Synthetic Monitoring configured WHEN the user runs `grafanactl resources list checks` THEN Synthetic Monitoring checks are listed in standard resource format
- GIVEN a Grafana instance with alerting rules configured WHEN the user runs `grafanactl resources list rules` THEN alert rules are listed in standard resource format
- GIVEN no provider config is set WHEN the user runs `grafanactl resources list dashboards` THEN the command succeeds without errors related to provider initialization
- GIVEN provider types are registered in the discovery registry WHEN the user runs `grafanactl resources list` (no arguments) THEN provider-backed resource types appear alongside native resource types in the output
- GIVEN an SLO definition YAML file with `metadata.namespace: foo` WHEN the user runs `grafanactl resources push slo -p ./` in a context with namespace `bar` THEN the `NamespaceOverrider` processor sets the namespace to `bar` and the SLO is pushed with namespace `bar`
- GIVEN an SLO definition YAML file WHEN the user runs `grafanactl resources push slo --omit-manager-fields -p ./` THEN the SLO is pushed without manager field annotations
- GIVEN an SLO definition exists on the server WHEN the user runs `grafanactl resources edit slo/<uuid>` THEN the resource is fetched via the adapter's Get method, opened in `$EDITOR`, and the modified version is submitted via the adapter's Update method

---

## Wave 4: Deprecation and Polish

### T7: Deprecation Warnings on Provider Top-Level Commands

**Priority**: P1
**Effort**: Small
**Depends on**: T6
**Type**: task

Add `PersistentPreRun` hooks to the top-level `slo`, `synth`, and `alert` commands that print a deprecation warning to stderr. The warning MUST direct users to the equivalent `grafanactl resources` command. The warning MUST be suppressed when agent mode is active or when `--json` output is requested (risk mitigation for CI/agent workflows).

**Deliverables:**
- `internal/providers/slo/provider.go` — deprecation hook added
- `internal/providers/synth/provider.go` — deprecation hook added
- `internal/providers/alert/provider.go` — deprecation hook added
- `internal/providers/deprecation.go` — shared deprecation warning helper
- `internal/providers/deprecation_test.go` — unit tests

**Acceptance criteria:**
- GIVEN the user runs `grafanactl slo definitions list` WHEN the command executes THEN a deprecation warning is printed to stderr AND the command still produces correct output
- GIVEN agent mode is active WHEN the user runs `grafanactl slo definitions list` THEN no deprecation warning is printed to stderr
- GIVEN `--json` flag is active WHEN the user runs `grafanactl slo definitions list --json ?` THEN no deprecation warning is printed to stderr

---

### T8: Integration Tests, Documentation, and Skills Update

**Priority**: P1
**Effort**: Medium
**Depends on**: T6, T7
**Type**: chore

Add integration-level tests that exercise the full pipeline: selector parsing -> discovery -> router -> adapter -> (mock) REST client. Update CLI reference examples in `cmd/grafanactl/resources/` command help text to include provider resource examples. Update agent skills (`.claude/skills/`) that reference provider-specific command paths.

Verify that `make all` passes (lint + tests + build + docs).

**Deliverables:**
- `internal/resources/adapter/integration_test.go` — end-to-end pipeline tests with mock adapters
- `cmd/grafanactl/resources/*.go` — updated command examples showing provider resource usage
- `.claude/skills/` — updated skill files referencing unified resource paths
- Verified `make all` passes

**Acceptance criteria:**
- GIVEN the `ResourceAdapter` interface exists WHEN a new provider is implemented THEN it can participate in the resources pipeline by implementing `ResourceAdapter` and registering a static descriptor, without modifying the resources command code
- GIVEN all changes are applied WHEN `make all` is run THEN it completes successfully with no lint errors, test failures, or doc drift

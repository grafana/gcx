---
type: feature-tasks
title: "TypedResourceAdapter[T] Foundation"
status: draft
spec: spec/feature-typed-resource-adapter-foundation/spec.md
plan: spec/feature-typed-resource-adapter-foundation/plan.md
created: 2026-03-20
---

# Implementation Tasks

## Dependency Graph

```
T1 (TypedCRUD generic + tests)
├─→ T2 (Refactor synth checks — proof of concept)
├─→ T3 (Refactor synth probes)
├─→ T4 (Refactor SLO definitions)
└─→ T5 (Refactor alert rules + groups)
     │
     └─→ T6 (Final verification: make all)
```

## Wave 1: Generic Foundation

### T1: Implement TypedCRUD[T] and TypedRegistration[T] with unit tests

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Implement the `TypedCRUD[T]` generic struct and `TypedRegistration[T]` registration bridge in `internal/resources/adapter/typed.go`. The generic holds typed function pointers (`NameFn`, `ListFn`, `GetFn`, `CreateFn`, `UpdateFn`, `DeleteFn`), configuration fields (`Namespace`, `StripFields`, `RestoreNameFn`, `MetadataFn`), and provides `AsAdapter(desc, aliases)` returning a `ResourceAdapter`. `TypedRegistration[T]` provides `ToRegistration()` returning an `adapter.Registration`. Write comprehensive unit tests in `typed_test.go` using a mock `TestWidget` struct to verify all CRUD operations, nil-function ErrUnsupported behavior, MetadataFn merge semantics, and MetadataFn name/namespace protection.

**Deliverables:**
- `internal/resources/adapter/typed.go`
- `internal/resources/adapter/typed_test.go`

**Acceptance criteria:**
- GIVEN the `TypedCRUD[T]` generic with a mock `TestWidget` struct implementing all CRUD functions
  WHEN `List` is called
  THEN it returns an `UnstructuredList` where each item has correct apiVersion, kind, metadata.name (from NameFn), metadata.namespace, and spec (with StripFields removed)

- GIVEN the `TypedCRUD[T]` generic with a mock `TestWidget` struct
  WHEN `Get` is called with a name string
  THEN it returns an `Unstructured` object with correct K8s envelope and stripped spec fields

- GIVEN the `TypedCRUD[T]` generic with a mock `TestWidget` struct and a `RestoreNameFn`
  WHEN `Create` is called with an `Unstructured` containing a spec
  THEN it unmarshals the spec to `*T`, calls `RestoreNameFn` with `metadata.name`, calls `CreateFn`, and returns the result wrapped in a K8s envelope

- GIVEN the `TypedCRUD[T]` generic with a mock `TestWidget` struct and a `RestoreNameFn`
  WHEN `Update` is called with an `Unstructured` containing a spec
  THEN it unmarshals the spec to `*T`, calls `RestoreNameFn` with `metadata.name`, calls `UpdateFn` with (name, spec), and returns the result wrapped in a K8s envelope

- GIVEN the `TypedCRUD[T]` generic with a mock `TestWidget` struct
  WHEN `Delete` is called with a name string
  THEN it calls `DeleteFn` with that name and returns the error

- GIVEN a `TypedCRUD[T]` where `CreateFn` is nil
  WHEN `Create` is called
  THEN it returns `errors.ErrUnsupported`

- GIVEN a `TypedCRUD[T]` where `UpdateFn` is nil
  WHEN `Update` is called
  THEN it returns `errors.ErrUnsupported`

- GIVEN a `TypedCRUD[T]` where `DeleteFn` is nil
  WHEN `Delete` is called
  THEN it returns `errors.ErrUnsupported`

- GIVEN a `TypedCRUD[T]` with a `MetadataFn` that returns `{"uid": "abc-123", "custom": "value"}`
  WHEN `List` is called
  THEN each item in the returned `UnstructuredList` has `metadata.uid` set to `"abc-123"` and `metadata.custom` set to `"value"`, while `metadata.name` and `metadata.namespace` retain their values from `NameFn` and `Namespace` respectively

- GIVEN a `TypedCRUD[T]` with a `MetadataFn` that returns `{"name": "override-attempt"}`
  WHEN `Get` is called
  THEN `metadata.name` is set by `NameFn` (not overwritten by MetadataFn)

- GIVEN a `TypedCRUD[T]` where `MetadataFn` is nil
  WHEN `List` or `Get` is called
  THEN the metadata contains only `name` and `namespace` (no error, no extra fields)

---

## Wave 2: Provider Refactoring

### T2: Refactor synth checks adapter to use TypedCRUD[CheckSpec]

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Refactor `internal/providers/synth/checks/resource_adapter.go` to replace the hand-written `ResourceAdapter` struct with `TypedCRUD[CheckSpec]` + `AsAdapter()`. The `ListFn` closure captures `checksClient` and `probesClient`, calls `client.List()`, builds the probe name map, converts each `Check` to `CheckSpec` with resolved probe names, and returns `[]CheckSpec`. The `GetFn` closure extracts the numeric ID from the slug name, calls `client.Get()`, and converts to `CheckSpec`. The `CreateFn`/`UpdateFn` closures handle tenant resolution, probe ID resolution, and `SpecToCheck` conversion. Configure `StripFields` to remove server-managed fields. Configure `MetadataFn` to set `metadata.uid` for checks with non-zero IDs. Update the `init()` registration in `synth/provider.go` to use `TypedRegistration[CheckSpec].ToRegistration()`. Remove or reduce `adapter.go` (`ToResource`/`FromResource`) -- keep `slugifyJob`, `extractIDFromSlug`, `SpecToCheck`, `FileNamer`, and probe resolution helpers. Update existing tests to work with the refactored adapter.

**Deliverables:**
- `internal/providers/synth/checks/resource_adapter.go` (rewritten)
- `internal/providers/synth/checks/adapter.go` (reduced -- ToResource/FromResource removed)
- `internal/providers/synth/provider.go` (registration updated)
- `internal/providers/synth/checks/resource_adapter_test.go` (updated)

**Acceptance criteria:**
- GIVEN the refactored synth checks adapter using `TypedCRUD[CheckSpec]`
  WHEN `grafanactl resources get checks` is executed against a Synthetic Monitoring API
  THEN the output is byte-for-byte identical to the output produced by the pre-refactor adapter

- GIVEN the refactored synth checks adapter
  WHEN a check is created via `grafanactl resources push` with a check YAML
  THEN the check is created successfully with probe names resolved to IDs

- GIVEN the refactored synth checks adapter
  WHEN a check is updated via `grafanactl resources push` with a modified check YAML
  THEN the check is updated successfully with the numeric ID recovered from metadata.name

- GIVEN all refactored adapters
  WHEN `make all` is run (lint + tests + build + docs)
  THEN it completes with exit code 0

---

### T3: Refactor synth probes adapter to use TypedCRUD[Probe]

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Refactor `internal/providers/synth/probes/resource_adapter.go` to replace the hand-written `ResourceAdapter` struct with `TypedCRUD[Probe]` + `AsAdapter()`. Probes are read-only, so `CreateFn`, `UpdateFn`, and `DeleteFn` are nil (auto-returning `ErrUnsupported`). The `ListFn` closure calls `client.List()` and returns `[]Probe`. The `GetFn` closure lists all probes and filters by ID (matching existing behavior since the SM API has no single-probe GET). Configure `NameFn` to return `strconv.FormatInt(probe.ID, 10)`. Configure `StripFields` to remove `id`, `tenantId`, `created`, `modified`, `onlineChange`, `online`, `version`. Remove `ToResource` from `adapter.go`. Update registration in `synth/provider.go`. Update tests.

**Deliverables:**
- `internal/providers/synth/probes/resource_adapter.go` (rewritten)
- `internal/providers/synth/probes/adapter.go` (removed -- ToResource absorbed)
- `internal/providers/synth/provider.go` (registration updated, if not done in T2)
- `internal/providers/synth/probes/resource_adapter_test.go` (updated)

**Acceptance criteria:**
- GIVEN the refactored synth probes adapter using `TypedCRUD[Probe]`
  WHEN `grafanactl resources get probes` is executed
  THEN the output is identical to the pre-refactor adapter output

- GIVEN all refactored adapters
  WHEN `make all` is run (lint + tests + build + docs)
  THEN it completes with exit code 0

---

### T4: Refactor SLO definitions adapter to use TypedCRUD[Slo]

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Refactor `internal/providers/slo/definitions/resource_adapter.go` to replace the hand-written `ResourceAdapter` struct with `TypedCRUD[Slo]` + `AsAdapter()`. The `ListFn` closure calls `client.List()` and returns `[]Slo`. The `GetFn` closure calls `client.Get(name)`. The `CreateFn` closure calls `client.Create()`, then `client.Get(createResp.UUID)` to return the full representation (matching existing behavior). The `UpdateFn` closure calls `client.Update(name, slo)`, then `client.Get(name)`. The `DeleteFn` closure calls `client.Delete(name)`. Configure `NameFn` to return `slo.UUID`. Configure `StripFields` to remove `uuid`, `readOnly`. Configure `RestoreNameFn` to restore `slo.UUID = name`. Remove `ToResource`/`FromResource` from `adapter.go`, keep `FileNamer`. Update the `init()` registration. Update tests.

**Deliverables:**
- `internal/providers/slo/definitions/resource_adapter.go` (rewritten)
- `internal/providers/slo/definitions/adapter.go` (reduced -- ToResource/FromResource removed, FileNamer kept)
- `internal/providers/slo/definitions/resource_adapter_test.go` (updated)

**Acceptance criteria:**
- GIVEN the refactored SLO definitions adapter using `TypedCRUD[Slo]`
  WHEN `grafanactl resources get slos` is executed
  THEN the output is identical to the pre-refactor adapter output

- GIVEN the refactored SLO definitions adapter
  WHEN an SLO is created and then updated via `grafanactl resources push`
  THEN the SLO is created/updated successfully with UUID managed via metadata.name

- GIVEN all refactored adapters
  WHEN `make all` is run (lint + tests + build + docs)
  THEN it completes with exit code 0

---

### T5: Refactor alert rules and groups adapters to use TypedCRUD

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Refactor `internal/providers/alert/resource_adapter.go` to replace the hand-written `RulesAdapter` and `GroupsAdapter` structs with two `TypedCRUD` instances: `TypedCRUD[RuleStatus]` and `TypedCRUD[RuleGroup]`. Both are read-only (nil write fns). The rules `ListFn` closure calls `client.List()`, flattens groups[].rules[] into `[]RuleStatus`. The rules `GetFn` calls `client.GetRule(name)`. Configure rules `NameFn` to return `rule.UID`, `StripFields` to remove `uid`. The groups `ListFn` calls `client.ListGroups()`. The groups `GetFn` calls `client.GetGroup(name)`. Configure groups `NameFn` to return `group.Name`, `StripFields` to remove `name`. Remove `RuleToResource`/`RuleFromResource`/`GroupToResource`/`GroupFromResource` from `adapter.go`. Keep any helpers used outside the adapter. Update the `init()` registration. Update tests.

**Deliverables:**
- `internal/providers/alert/resource_adapter.go` (rewritten)
- `internal/providers/alert/adapter.go` (reduced -- ToResource/FromResource functions removed)
- `internal/providers/alert/resource_adapter_test.go` (updated, if it exists)

**Acceptance criteria:**
- GIVEN the refactored alert rules and groups adapters
  WHEN `grafanactl resources get alertrules` and `grafanactl resources get alertrulegroups` are executed
  THEN the output is identical to the pre-refactor adapter output

- GIVEN all refactored adapters
  WHEN `make all` is run (lint + tests + build + docs)
  THEN it completes with exit code 0

---

## Wave 3: Final Verification

### T6: Full build verification and cleanup

**Priority**: P0
**Effort**: Small
**Depends on**: T2, T3, T4, T5
**Type**: chore

Run `GRAFANACTL_AGENT_MODE=false make all` to verify lint, tests, build, and docs all pass with all five adapters refactored. Remove any dead code (unused imports, orphaned test helpers) identified by the linter. Verify that `StaticDescriptor()`, `StaticAliases()`, `StaticGVK()` functions still exist and work for any callers outside the adapter (e.g., provider registration, CLI commands). Verify no new dependencies were added to `internal/resources/adapter/`.

**Deliverables:**
- Clean `make all` output
- Any final dead-code removal commits

**Acceptance criteria:**
- GIVEN all refactored adapters
  WHEN `make all` is run (lint + tests + build + docs)
  THEN it completes with exit code 0

- GIVEN the `internal/resources/adapter/` package
  WHEN its import list is inspected
  THEN it contains no new external dependencies beyond standard library, `k8s.io/apimachinery`, and `github.com/grafana/grafanactl/internal/resources`

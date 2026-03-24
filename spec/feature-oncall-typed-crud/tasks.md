---
type: feature-tasks
title: "Table-driven TypedCRUD[T] for OnCall Adapter"
status: approved
spec: spec/feature-oncall-typed-crud/spec.md
plan: spec/feature-oncall-typed-crud/plan.md
created: 2026-03-24
---

# Implementation Tasks

## Dependency Graph

```
T1 (registration helper + options) ──→ T2 (17 registrations) ──→ T3 (dead code cleanup + verification)
```

All tasks are sequential: T1 introduces the generic infrastructure, T2 uses it to register all types (replacing the old code), and T3 removes leftover dead code and verifies everything.

## Wave 1: Registration Infrastructure

### T1: Implement `registerOnCallResource[T]` and functional options

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Implement the generic registration helper and CRUD option types in `resource_adapter.go`. This includes: the `resourceMeta` struct, `crudOption[T]` type alias, `withCreate[T]`/`withUpdate[T]`/`withDelete[T]` constructors, and the `registerOnCallResource[T]` function itself. The function MUST call `adapter.Register()` with a factory that constructs a `TypedCRUD[T]` via `loader.LoadOnCallClient`, wiring NameFn, ListFn, GetFn, StripFields, Namespace, Descriptor, and Aliases. Keep the existing `RegisterAdapters` and `allResources` code intact during this task so the build compiles at all times.

**Deliverables:**
- `internal/providers/oncall/resource_adapter.go` — new types and functions added (alongside existing code, not yet replacing it)

**Acceptance criteria:**
- GIVEN the `registerOnCallResource[T]` function exists
  WHEN it is called with no `crudOption` arguments
  THEN the resulting `TypedCRUD[T]` has nil `CreateFn`, nil `UpdateFn`, nil `DeleteFn`

- GIVEN the `withCreate[T]` option is passed
  WHEN the factory is invoked
  THEN the `TypedCRUD[T]` has a non-nil `CreateFn` that calls the provided closure

- GIVEN the `withUpdate[T]` option is passed
  WHEN the factory is invoked
  THEN the `TypedCRUD[T]` has a non-nil `UpdateFn` that calls the provided closure

- GIVEN the `withDelete[T]` option is passed
  WHEN the factory is invoked
  THEN the `TypedCRUD[T]` has a non-nil `DeleteFn` that calls the provided closure

- GIVEN the `resourceMeta` struct
  WHEN inspected
  THEN it contains fields: `Descriptor resources.Descriptor`, `Aliases []string`, `Schema json.RawMessage`, `Example json.RawMessage`

- GIVEN the `registerOnCallResource[T]` function
  WHEN it is called with a `resourceMeta` that has a non-nil Schema
  THEN the `adapter.Registration` passed to `adapter.Register` includes that Schema

---

## Wave 2: Resource Registrations

### T2: Register all 17 OnCall resource types and replace `RegisterAdapters`

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T1
**Type**: task

Replace the body of `RegisterAdapters` (or rename to `registerAllAdapters`) to call `registerOnCallResource[T]` 17 times, one for each OnCall resource type. Each registration MUST:

- Use the exact Descriptor and Aliases from the current `allResources()` definitions
- Pass the correct NameFn (`func(t T) string { return t.ID }`)
- Pass the correct ListFn and GetFn from the client
- Pass the correct CRUD options per the spec's CRUD matrix (FR-010 through FR-014)
- Handle special cases: Shift (convert to ShiftRequest), ResolutionNote (convert to Input types), ShiftSwap (convert to Input types)
- Pass nil GetFn for UserGroup and SlackChannel (with a wrapper returning `errors.ErrUnsupported`)
- Pass empty string for filter-parameter list methods (EscalationPolicy, Route, Alert, ResolutionNote)
- Include Integration schema/example

At the end of this task, the old `allResources()` loop, `newSubResourceFactory`, and `subResourceAdapter` are no longer called by `init()`.

**Deliverables:**
- `internal/providers/oncall/resource_adapter.go` — rewritten `RegisterAdapters`/`registerAllAdapters` function with 17 `registerOnCallResource[T]` calls

**Acceptance criteria:**
- GIVEN the oncall package is loaded
  WHEN `adapter.AllRegistrations()` is called
  THEN exactly 17 registrations with group `oncall.ext.grafana.app` and version `v1alpha1` are present, one for each of the 17 OnCall resource kinds

- GIVEN a registered Integration adapter
  WHEN `List` is called
  THEN it returns an `UnstructuredList` where each item has `apiVersion: oncall.ext.grafana.app/v1alpha1`, `kind: Integration`, `metadata.name` set to the integration ID, and `spec` containing all integration fields except `id`

- GIVEN a registered Shift adapter
  WHEN `Create` is called with an unstructured Shift object
  THEN the adapter converts the spec to a `ShiftRequest` via JSON marshal/unmarshal and calls `client.CreateShift` with the `ShiftRequest`

- GIVEN a registered Shift adapter
  WHEN `Update` is called with an unstructured Shift object and a name
  THEN the adapter converts the spec to a `ShiftRequest` via JSON marshal/unmarshal and calls `client.UpdateShift` with the name and `ShiftRequest`

- GIVEN a registered ResolutionNote adapter
  WHEN `Create` is called with an unstructured ResolutionNote object
  THEN the adapter extracts `alert_group_id` and `text` from the spec and calls `client.CreateResolutionNote` with a `CreateResolutionNoteInput`

- GIVEN a registered ResolutionNote adapter
  WHEN `Update` is called with an unstructured ResolutionNote object and a name
  THEN the adapter extracts `text` from the spec and calls `client.UpdateResolutionNote` with the name and an `UpdateResolutionNoteInput`

- GIVEN a registered ShiftSwap adapter
  WHEN `Create` is called with an unstructured ShiftSwap object
  THEN the adapter extracts `schedule`, `swap_start`, `swap_end`, `beneficiary` from the spec and calls `client.CreateShiftSwap` with a `CreateShiftSwapInput`

- GIVEN a registered ShiftSwap adapter
  WHEN `Update` is called with an unstructured ShiftSwap object and a name
  THEN the adapter extracts `swap_start`, `swap_end` from the spec and calls `client.UpdateShiftSwap` with the name and an `UpdateShiftSwapInput`

- GIVEN a registered User adapter
  WHEN `Create` is called
  THEN the adapter returns `errors.ErrUnsupported`

- GIVEN a registered UserGroup adapter
  WHEN `Get` is called
  THEN the adapter returns `errors.ErrUnsupported`

- GIVEN a registered SlackChannel adapter
  WHEN `Get` is called
  THEN the adapter returns `errors.ErrUnsupported`

- GIVEN a registered AlertGroup adapter
  WHEN `Delete` is called with a name
  THEN the adapter calls `client.DeleteAlertGroup` and returns the result

- GIVEN a registered AlertGroup adapter
  WHEN `Create` is called
  THEN the adapter returns `errors.ErrUnsupported`

- GIVEN the existing `resource_adapter_test.go` test file
  WHEN `go test ./internal/providers/oncall/...` is run
  THEN all tests pass without modification

- The system SHALL register all 17 OnCall resources using the `registerOnCallResource[T]` generic function — no resource SHALL use direct `adapter.Register` calls or manual `TypedCRUD` construction outside this helper

---

## Wave 3: Dead Code Cleanup and Verification

### T3: Delete dead code and run full verification

**Priority**: P1
**Effort**: Small
**Depends on**: T2
**Type**: chore

Remove all dead code artifacts from the oncall package: `subResourceAdapter` struct and all its methods (`Descriptor`, `Aliases`, `List`, `Get`, `Create`, `Update`, `Delete`, `listRaw`, `getRaw`, `createRaw`, `updateRaw`, `deleteRaw`, `itemToResource`), the `toAnySlice` function, the `fromResource[T]` function in `adapter.go`, the `resourceDef` struct, the `allResources()` function, and `newSubResourceFactory`. Also remove the `var _ adapter.ResourceAdapter = &subResourceAdapter{}` assertion. Clean up any unused imports. Run `make all` (with `GRAFANACTL_AGENT_MODE=false`) to verify lint, tests, build, and docs generation pass.

**Deliverables:**
- `internal/providers/oncall/resource_adapter.go` — dead code removed
- `internal/providers/oncall/adapter.go` — `fromResource[T]` removed

**Acceptance criteria:**
- GIVEN the refactored codebase
  WHEN searching for `subResourceAdapter`, `toAnySlice`, `fromResource`, `resourceDef`, or `allResources` in the oncall package
  THEN none of these symbols exist

- GIVEN the refactored codebase
  WHEN `make lint` is run
  THEN no new lint errors are introduced

- GIVEN the refactored codebase
  WHEN `go test ./internal/providers/oncall/...` is run
  THEN all tests pass without modification

- GIVEN the refactored codebase
  WHEN `GRAFANACTL_AGENT_MODE=false make all` is run
  THEN the build succeeds with no errors

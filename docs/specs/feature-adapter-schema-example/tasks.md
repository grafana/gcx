---
type: feature-tasks
title: "Add Schema() and Example() methods to ResourceAdapter interface"
status: approved
spec: docs/specs/feature-adapter-schema-example/spec.md
plan: docs/specs/feature-adapter-schema-example/plan.md
created: 2026-03-25
---

# Implementation Tasks

## Dependency Graph

```
T1 (interface + typedAdapter fields + ToRegistration wiring + mocks + round-trip test)
 │
 ▼
T2 (update schemas/examples commands to use adapter methods)
```

## Wave 1: Core Interface and Implementation

### T1: Add Schema()/Example() to ResourceAdapter and implement on typedAdapter
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Add `Schema() json.RawMessage` and `Example() json.RawMessage` to the `ResourceAdapter` interface in `adapter.go`. Add `schema` and `example` fields of type `json.RawMessage` to `typedAdapter[T]` in `typed.go` (NOT on `TypedCRUD[T]` — schema/example are static registration metadata, not runtime CRUD behavior). Implement the two new methods on `typedAdapter[T]` by returning those fields. Update the `ToRegistration()` factory closure in `typed.go` to set schema/example on the `typedAdapter[T]` wrapper after it wraps the `TypedCRUD`. Update the single `mockAdapter` struct in `adapter_test.go` with stub methods returning `nil`. Add a round-trip test in `typed_test.go` verifying `TypedRegistration[T]` → `ToRegistration()` → `Factory()` → `adapter.Schema()`/`adapter.Example()`.

**Deliverables:**
- `internal/resources/adapter/adapter.go` — interface updated with two new methods
- `internal/resources/adapter/typed.go` — `typedAdapter[T]` fields added, methods added, `ToRegistration()` closure updated (TypedCRUD untouched)
- `internal/resources/adapter/adapter_test.go` — `mockAdapter` stub methods added
- `internal/resources/adapter/typed_test.go` — round-trip test for schema/example wiring

**Acceptance criteria:**
- GIVEN the ResourceAdapter interface definition
  WHEN a developer inspects it
  THEN it contains `Schema() json.RawMessage` and `Example() json.RawMessage` methods

- GIVEN a TypedRegistration[T] with Schema and Example set
  WHEN ToRegistration() is called and the resulting Factory is invoked
  THEN the returned ResourceAdapter's Schema() returns the same json.RawMessage as TypedRegistration.Schema
  AND the returned ResourceAdapter's Example() returns the same json.RawMessage as TypedRegistration.Example

- GIVEN a TypedRegistration[T] with Schema and Example both nil
  WHEN ToRegistration() is called and the resulting Factory is invoked
  THEN the returned ResourceAdapter's Schema() returns nil
  AND the returned ResourceAdapter's Example() returns nil

- GIVEN the mockAdapter struct in adapter_test.go
  WHEN Schema() or Example() is called on it
  THEN it returns nil without error

- GIVEN the full codebase after changes
  WHEN `make all` is run (with GCX_AGENT_MODE=false)
  THEN all tests pass, linting passes, build succeeds, and docs generate without drift

---

## Wave 2: Command Integration

### T2: Update schemas and examples commands to use adapter methods
**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Update `resolveSchema()` in `schemas.go` to accept an optional adapter instance and call `adapter.Schema()` as a fallback path (alongside the existing `adapter.SchemaForGVK` global lookup). Update `examplesCmd` in `examples.go` to attempt adapter-based lookup when an adapter is available from the registry, falling back to the existing `adapter.ExampleForGVK` global function. The output of both commands MUST remain byte-identical to the current output — this is purely an internal wiring change that adds a new code path while preserving the existing one.

**Deliverables:**
- `cmd/gcx/resources/schemas.go` — `resolveSchema` updated to use adapter method when available
- `cmd/gcx/resources/examples.go` — example lookup updated to use adapter method when available

**Acceptance criteria:**
- GIVEN a provider resource type with a registered schema (e.g., SLO)
  WHEN `gcx resources schemas slo -o json` is run
  THEN the output is byte-identical to the output before this change

- GIVEN a provider resource type with a registered example (e.g., SLO)
  WHEN `gcx resources examples slo -o yaml` is run
  THEN the output is byte-identical to the output before this change

- GIVEN the full codebase after changes
  WHEN `make all` is run (with GCX_AGENT_MODE=false)
  THEN all tests pass, linting passes, build succeeds, and docs generate without drift

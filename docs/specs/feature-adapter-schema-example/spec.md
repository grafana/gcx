---
type: feature-spec
title: "Add Schema() and Example() methods to ResourceAdapter interface"
status: done
beads_id: grafanactl-experiments-ph3
created: 2026-03-25
---

# Add Schema() and Example() methods to ResourceAdapter interface

## Problem Statement

Schema and example data for provider-backed resource types are currently accessible only through global lookup functions (`adapter.SchemaForGVK` and `adapter.ExampleForGVK`) that scan a global `[]Registration` slice. Any code that holds a `ResourceAdapter` instance — such as provider-specific CLI commands or future auto-generated CRUD commands — cannot retrieve schema or example data from the adapter itself. Callers must know the GVK and call a separate global function, coupling them to the registration subsystem.

The downstream feature "Auto-generate provider CRUD commands from adapter registrations" (grafanactl-experiments-gab) is blocked because generated commands need to access schema/example through the adapter, not through disconnected global functions.

The current workaround is to call `adapter.SchemaForGVK(gvk)` and `adapter.ExampleForGVK(gvk)` directly, which works for the centralized `resources schemas` and `resources examples` commands but does not scale to per-provider command generation.

## Scope

### In Scope

- Add `Schema() json.RawMessage` and `Example() json.RawMessage` methods directly to the `ResourceAdapter` interface
- Implement the new methods on `typedAdapter[T]` with schema/example stored as fields on `typedAdapter[T]` itself (not on `TypedCRUD[T]`)
- Wire schema/example from `TypedRegistration[T]` through the `ToRegistration()` factory closure into `typedAdapter[T]`
- Update the 2 test `mockAdapter` structs (in `adapter_test.go` and `integration_test.go`) with stub methods returning nil
- Update `resources schemas` and `resources examples` commands to use the adapter methods when an adapter instance is available
- Ensure `make all` passes with zero regressions
- Keep global `SchemaForGVK` / `ExampleForGVK` functions for backward compatibility

### Out of Scope

- **Auto-generated provider CRUD commands** — That is the downstream feature (grafanactl-experiments-gab). This spec only unblocks it.
- **Runtime-dynamic schema/example** — Schema and example are static per resource type. This spec does not add support for schemas that change at runtime.
- **Removing the global `SchemaForGVK` / `ExampleForGVK` functions** — They remain for backward compatibility and convenience. Removal is a separate future cleanup.
- **Adding schema/example to providers that currently lack them** — Existing provider registrations are unchanged.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| How to expose schema/example on adapters | Add `Schema()` and `Example()` directly to the `ResourceAdapter` interface (Option 1) | All 16 production adapters flow through `TypedCRUD[T].AsAdapter()` → `typedAdapter[T]`. There are zero manual `ResourceAdapter` implementations in production code. Only 2 test mocks in `adapter_test.go` and `integration_test.go` (plus `countingAdapter` which embeds `mockAdapter`) need stub methods returning nil. The "breaking change" blast radius is trivially small. Option 1 is simpler: no optional interface dance, no type assertions, no fallback helpers. | Codebase analysis: grep confirms all 9 provider `resource_adapter.go` files use `TypedCRUD`/`AsAdapter()`; only 2 test files define `mockAdapter` |
| Where schema/example values are stored at runtime | On `typedAdapter[T]` directly (not on `TypedCRUD[T]`) | Schema/example are static registration metadata; `TypedCRUD[T]` is runtime CRUD behavior. Mixing them on the same struct muddies the abstraction and would entangle schema/example in the upcoming `TypedCRUD[T ResourceIdentity]` refactor. The `ToRegistration()` factory closure sets them on `typedAdapter[T]` after wrapping the `TypedCRUD`. | Coordination with in-flight TypedCRUD refactor |
| Keep global functions | Yes — `SchemaForGVK` and `ExampleForGVK` remain unchanged | Existing callers in `schemas.go` and `examples.go` continue to work. Commands MAY be updated to prefer the adapter methods but the globals remain available. | Backward compatibility |

## Functional Requirements

- **FR-001**: The `ResourceAdapter` interface MUST include a `Schema() json.RawMessage` method that returns the JSON Schema for the resource type, or nil if no schema is registered.

- **FR-002**: The `ResourceAdapter` interface MUST include an `Example() json.RawMessage` method that returns an example manifest for the resource type, or nil if no example is registered.

- **FR-003**: `typedAdapter[T]` MUST hold `schema` and `example` fields of type `json.RawMessage` and implement `Schema()` and `Example()` by returning them. These fields MUST NOT be on `TypedCRUD[T]` — schema/example are static registration metadata, not runtime CRUD behavior.

- **FR-004**: `TypedRegistration[T].ToRegistration()` MUST propagate `Schema` and `Example` values into the `typedAdapter[T]` wrapper (via the factory closure), so that the resulting adapter returns the correct data from `Schema()` and `Example()`. `ToRegistration()` MUST remain the single wiring point — no alternative propagation paths.

- **FR-005**: `typed_test.go` MUST include a round-trip test verifying schema/example survive `TypedRegistration[T]` → `ToRegistration()` → `Factory()` → `adapter.Schema()`/`adapter.Example()`. This test anchors correctness for the in-flight `TypedCRUD[T ResourceIdentity]` refactor.

- **FR-006**: The `mockAdapter` struct in `adapter_test.go` MUST be updated with `Schema()` and `Example()` stub methods that return nil.

- **FR-007**: The `mockAdapter` struct in `integration_test.go` MUST be updated with `Schema()` and `Example()` stub methods that return nil. The `countingAdapter` in `router_test.go` embeds `mockAdapter` and MUST NOT need separate method additions.

- **FR-008**: The `resources schemas` command MUST continue to produce identical output.

- **FR-009**: The `resources examples` command MUST continue to produce identical output.

- **FR-010**: The global `SchemaForGVK` and `ExampleForGVK` functions MUST remain unchanged in signature and behavior.

## Acceptance Criteria

- GIVEN the `ResourceAdapter` interface
  WHEN a caller inspects its method set
  THEN it MUST include `Schema() json.RawMessage` and `Example() json.RawMessage`

- GIVEN a `ResourceAdapter` instance produced via `TypedRegistration[T]` with schema data set
  WHEN `Schema()` is called
  THEN it MUST return the registered `json.RawMessage` schema

- GIVEN a `ResourceAdapter` instance produced via `TypedRegistration[T]` with example data set
  WHEN `Example()` is called
  THEN it MUST return the registered `json.RawMessage` example

- GIVEN a `ResourceAdapter` instance produced via `TypedRegistration[T]` with no schema set
  WHEN `Schema()` is called
  THEN it MUST return nil

- GIVEN a `ResourceAdapter` instance produced via `TypedRegistration[T]` with no example set
  WHEN `Example()` is called
  THEN it MUST return nil

- GIVEN a `TypedRegistration[T]` with `Schema` and `Example` set
  WHEN `ToRegistration()` is called and the factory is invoked
  THEN the resulting adapter's `Schema()` MUST return the same schema data
  AND the resulting adapter's `Example()` MUST return the same example data

- GIVEN the test `mockAdapter` struct in `adapter_test.go`
  WHEN the test suite is compiled
  THEN `mockAdapter` MUST satisfy the `ResourceAdapter` interface (compile-time `var _ adapter.ResourceAdapter = &mockAdapter{}` check MUST pass)

- GIVEN the `countingAdapter` struct in `router_test.go` that embeds `mockAdapter`
  WHEN the test suite is compiled
  THEN `countingAdapter` MUST satisfy the `ResourceAdapter` interface via the embedded `mockAdapter` stubs

- GIVEN an existing provider registration (e.g., SLO, OnCall, Synth, Incidents, Fleet, Alert, KG, K6)
  WHEN `make all` is run
  THEN compilation, linting, and all tests MUST pass with zero changes to provider code

- GIVEN the `resources schemas` command with an adapter-backed resource
  WHEN executed with `-o json`
  THEN the output MUST include the provider schema (same behavior as before)

- GIVEN the `resources examples` command with an adapter-backed resource
  WHEN executed with `-o yaml`
  THEN the output MUST include the provider example (same behavior as before)

## Negative Constraints

- NEVER remove or change the signature of the existing `SchemaForGVK` or `ExampleForGVK` global functions.
- NEVER require provider implementations to change their `init()` / `RegisterAdapters()` registration code for this feature.
- DO NOT store mutable or runtime-dependent data in `Schema()` or `Example()` — schema and example values MUST be static per resource type.
- DO NOT export `typedAdapter[T]` — it MUST remain unexported.
- DO NOT introduce an optional interface (`SchemaProvider` or similar) — the methods MUST be on the `ResourceAdapter` interface directly.
- DO NOT add `schema` or `example` fields to `TypedCRUD[T]` — schema/example are static registration metadata and MUST NOT be entangled with runtime CRUD behavior. Store them on `typedAdapter[T]` instead.
- NEVER add `Schema()` or `Example()` methods to production provider code — all production adapters already flow through `typedAdapter[T]` which will implement them automatically.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Future manual `ResourceAdapter` implementations outside the codebase will need to add `Schema()` and `Example()` methods | Low — all known production adapters use `TypedCRUD[T]`; external implementors are not a supported use case | Document the interface change in the PR description. Stub implementations returning nil are trivial. |
| In-flight `TypedCRUD[T ResourceIdentity]` refactor could conflict | Low — schema/example are stored on `typedAdapter[T]`, not `TypedCRUD[T]`, so the refactor does not touch schema wiring | Explicit separation of static metadata (typedAdapter) from CRUD behavior (TypedCRUD); round-trip test in `typed_test.go` anchors correctness |
| Test mocks in other packages (e.g., `cmd/grafanactl/root/command_test.go`, `cmd/grafanactl/providers/command_test.go`) might implement `ResourceAdapter` manually | Low — grep confirms these files reference the type but use factory functions, not manual struct implementations | Run `make all` to catch any compilation failures |

## Open Questions

- [RESOLVED]: Which option to use (1 or 2)? — Option 1 (add methods directly to `ResourceAdapter` interface). All production adapters use `TypedCRUD[T].AsAdapter()`, so the blast radius is limited to 2 test mock structs. Simpler than an optional interface pattern.
- [DEFERRED]: Should the global `SchemaForGVK` / `ExampleForGVK` functions be deprecated? — Not in this spec. They remain for backward compatibility and convenience.

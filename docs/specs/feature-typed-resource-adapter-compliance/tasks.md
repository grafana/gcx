---
type: feature-tasks
title: "Typed Resource Adapter Compliance"
status: done
spec: docs/specs/feature-typed-resource-adapter-compliance/spec.md
plan: docs/specs/feature-typed-resource-adapter-compliance/plan.md
created: 2026-03-26
---

# Implementation Tasks

## Dependency Graph

```
T1 (ResourceIdentity interface)
 │
 ├──► T2 (Domain types: SLO, Synth, Alert, KG, Incidents, Fleet)
 │
 ├──► T3 (Domain types: OnCall 17 types)
 │
 ├──► T4 (Domain types: K6 5 types)
 │
 └──► T5 (TypedObject[T] + TypedCRUD constraint + typed methods)
       │
       ├──► T6 (TypedRegistrations() + unified registration)
       │     │
       │     ├──► T7 (Migrate SLO, Synth, Fleet, KG)
       │     │
       │     ├──► T8 (Migrate OnCall, Incidents, Alert)
       │     │
       │     └──► T9 (Migrate K6)
       │           │
       │           └──► T10 (Provider CRUD command migration: SLO, Synth, Fleet)
       │                 │
       │                 ├──► T11 (Provider CRUD command migration: OnCall, K6, KG, Incidents, Alert)
       │                 │     │
       │                 │     └──► T12 (Cleanup: remove NameFn/RestoreNameFn, ResourceAdapters())
       │                 │           │
       │                 │           └──► T13 (CONSTITUTION.md + DESIGN.md updates)
       │                 │
       │                 └──► T11 (parallel dependency)
```

## Wave 1: Foundation Types

### T1: ResourceIdentity Interface

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Define the `ResourceIdentity` interface in `internal/resources/adapter/identity.go`. The interface declares `GetResourceName() string` and `SetResourceName(string)`. This is the foundational type constraint that all domain types will implement and that `TypedCRUD` will require.

**Deliverables:**
- `internal/resources/adapter/identity.go`
- `internal/resources/adapter/identity_test.go`

**Acceptance criteria:**
- GIVEN the adapter package
  WHEN a type implements `GetResourceName() string` and `SetResourceName(string)`
  THEN it satisfies the `ResourceIdentity` interface at compile time
- GIVEN the `ResourceIdentity` interface
  WHEN inspected
  THEN it declares exactly two methods: `GetResourceName() string` and `SetResourceName(string)`
- The `ResourceIdentity` interface MUST NOT depend on any package outside `internal/resources/adapter`

---

## Wave 2: Domain Type Compliance

### T2: ResourceIdentity on SLO, Synth, Alert, KG, Incidents, Fleet Domain Types

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement `GetResourceName()` and `SetResourceName()` on domain types for 6 providers: SLO (`Slo` — UUID), Synth (`checkResource` — int64 name, `Probe` — int64 ID), Alert (`RuleStatus` — UID, `RuleGroup` — Name), KG (`Rule` — Name), Incidents (`Incident` — IncidentID), Fleet (`Pipeline` — string name, `Collector` — string name). Each `SetResourceName` must parse numeric IDs silently (swallow parse errors) where the identity field is numeric.

**Deliverables:**
- `internal/providers/slo/definitions/types.go` (Slo methods)
- `internal/providers/synth/checks/types.go` or equivalent (checkResource methods)
- `internal/providers/synth/probes/types.go` or equivalent (Probe methods)
- `internal/providers/alert/types.go` (RuleStatus, RuleGroup methods)
- `internal/providers/kg/types.go` (Rule methods)
- `internal/providers/incidents/types.go` (Incident methods)
- `internal/providers/fleet/types.go` or `provider.go` (Pipeline, Collector methods)
- Tests for each domain type's ResourceIdentity implementation

**Acceptance criteria:**
- GIVEN an `Slo` with UUID "abc-123"
  WHEN `GetResourceName()` is called
  THEN it returns "abc-123"
- GIVEN an empty `Slo`
  WHEN `SetResourceName("abc-123")` is called
  THEN `slo.UUID` equals "abc-123"
- GIVEN a `Probe` with ID 42
  WHEN `GetResourceName()` is called
  THEN it returns "42"
- GIVEN an empty `Probe`
  WHEN `SetResourceName("not-a-number")` is called
  THEN it does NOT panic and the ID field is zero-valued
- GIVEN any domain type from these 6 providers
  WHEN assigned to a variable of type `adapter.ResourceIdentity`
  THEN the assignment compiles without error
- All existing tests in these provider packages MUST continue to pass

---

### T3: ResourceIdentity on OnCall 17 Domain Types

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement `GetResourceName()` and `SetResourceName()` on all 17 OnCall domain types. All use `ID string` as their identity field, making the implementation uniform. Types: Integration, EscalationChain, EscalationPolicy, Schedule, Shift, IntegrationRoute, OutgoingWebhook, AlertGroup, User, Team, UserGroup, SlackChannel, Alert, Organization, ResolutionNote, ShiftSwap, PersonalNotificationRule.

**Deliverables:**
- `internal/providers/oncall/types.go` (methods on all 17 types)
- `internal/providers/oncall/types_identity_test.go`

**Acceptance criteria:**
- GIVEN any of the 17 OnCall domain types with `ID` set to "XYZ"
  WHEN `GetResourceName()` is called
  THEN it returns "XYZ"
- GIVEN any of the 17 OnCall domain types
  WHEN `SetResourceName("ABC")` is called
  THEN the `ID` field equals "ABC"
- GIVEN any of the 17 OnCall domain types
  WHEN assigned to a variable of type `adapter.ResourceIdentity`
  THEN the assignment compiles without error
- All existing OnCall tests MUST continue to pass

---

### T4: ResourceIdentity on K6 5 Domain Types

**Priority**: P0
**Effort**: Small
**Depends on**: T1
**Type**: task

Implement `GetResourceName()` and `SetResourceName()` on all 5 K6 domain types. Project, LoadTest, Schedule, EnvVar use `ID int` (converted via strconv). LoadZone uses `Name string`. `SetResourceName` for int-based types MUST swallow parse errors.

**Deliverables:**
- `internal/providers/k6/types.go` (methods on all 5 types)
- `internal/providers/k6/types_identity_test.go`

**Acceptance criteria:**
- GIVEN a K6 `Project` with `ID = 42`
  WHEN `GetResourceName()` is called
  THEN it returns "42"
- GIVEN a K6 `Project`
  WHEN `SetResourceName("not-a-number")` is called
  THEN it does NOT panic and `ID` is zero-valued
- GIVEN a K6 `LoadZone` with `Name = "us-east-1"`
  WHEN `GetResourceName()` is called
  THEN it returns "us-east-1"
- All existing K6 tests MUST continue to pass

---

### T5: TypedObject[T], TypedCRUD Constraint Change, and Typed Public Methods

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Three changes to `internal/resources/adapter/typed.go`:

1. Define `TypedObject[T ResourceIdentity]` struct with embedded `metav1.ObjectMeta`, `TypeMeta`, and `Spec T` field. JSON serialization MUST produce `{apiVersion, kind, metadata, spec}`.
2. Change `TypedCRUD[T any]` to `TypedCRUD[T ResourceIdentity]`. This propagates to `typedAdapter[T]` and `TypedRegistration[T]`.
3. Add typed public methods: `List(ctx) ([]TypedObject[T], error)`, `Get(ctx, name) (*TypedObject[T], error)`, `Create(ctx, *TypedObject[T]) (*TypedObject[T], error)`, `Update(ctx, name, *TypedObject[T]) (*TypedObject[T], error)`, `Delete(ctx, name) error`. These delegate to the existing `ListFn`/`GetFn`/`CreateFn`/`UpdateFn`/`DeleteFn` function pointers.

The `toUnstructured` method MUST use `T.GetResourceName()` instead of `NameFn` when `NameFn` is nil. The `fromUnstructured` method MUST call `T.SetResourceName()` instead of `RestoreNameFn` when `RestoreNameFn` is nil. Both `NameFn` and `RestoreNameFn` remain on the struct during this task (removal is T12) to avoid breaking existing code.

**Deliverables:**
- `internal/resources/adapter/typed.go` (modified: constraint change, TypedObject, typed methods)
- `internal/resources/adapter/typed_test.go` (new or updated tests)

**Acceptance criteria:**
- GIVEN `TypedCRUD[T]` declaration
  WHEN `T` does not implement `ResourceIdentity`
  THEN compilation fails
- GIVEN a `TypedCRUD[T]` with `NameFn` set to nil and `T` implementing `ResourceIdentity`
  WHEN `toUnstructured(item)` is called
  THEN `metadata.name` equals `item.GetResourceName()`
- GIVEN a `TypedCRUD[T]` with `RestoreNameFn` set to nil
  WHEN `fromUnstructured(obj)` is called
  THEN `item.SetResourceName(obj.GetName())` is called on the result
- GIVEN a `TypedCRUD[MyType]` with `ListFn` configured
  WHEN `crud.List(ctx)` is called
  THEN it returns `[]TypedObject[MyType]` with each element wrapping a domain object
- GIVEN a `TypedCRUD[MyType]` with nil `CreateFn`
  WHEN `crud.Create(ctx, item)` is called
  THEN it returns `errors.ErrUnsupported`
- GIVEN a `TypedObject[Slo]` with Spec populated
  WHEN marshaled to JSON
  THEN the output contains `apiVersion`, `kind`, `metadata`, and `spec` fields
- `AsAdapter()` MUST continue to return a valid `ResourceAdapter`
- The `Factory` type signature MUST NOT change

---

## Wave 3: Unified Registration

### T6: Provider.TypedRegistrations() and Unified Registration in providers.Register()

**Priority**: P1
**Effort**: Medium
**Depends on**: T5
**Type**: task

1. Add `TypedRegistrations() []adapter.Registration` method to the `Provider` interface.
2. Modify `providers.Register()` to call `p.TypedRegistrations()` and call `adapter.Register()` for each returned registration, making provider identity and adapter registration atomic.
3. Remove `ResourceAdapters()` from the `Provider` interface.
4. Update the `mockProvider` in `provider_test.go`.

All 8 providers MUST return their registrations from `TypedRegistrations()`. Providers that currently have separate `adapter.Register()` calls in standalone `init()` functions will retain those calls temporarily — they are migrated in T7-T9.

**Deliverables:**
- `internal/providers/provider.go` (interface change)
- `internal/providers/registry.go` (Register function change)
- `internal/providers/provider_test.go` (mock update)

**Acceptance criteria:**
- GIVEN the `Provider` interface
  WHEN inspected
  THEN it has a `TypedRegistrations()` method and does NOT have a `ResourceAdapters()` method
- GIVEN a provider with 2 resource types
  WHEN `providers.Register(provider)` is called
  THEN `adapter.AllTypedRegistrations()` contains 2 new entries matching the provider's registrations
- GIVEN a provider returning nil from `TypedRegistrations()`
  WHEN `providers.Register(provider)` is called
  THEN no adapter registrations are added and no error occurs
- The `adapter.Register()` function MUST NOT be removed (other code may still call it during migration)
- All existing provider tests MUST continue to pass

---

## Wave 4: Provider Migration (Registration Consolidation)

### T7: Consolidate Registration — SLO, Synth, Fleet, KG

**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T6, T2
**Type**: task

Migrate 4 providers to return their registrations from `TypedRegistrations()` and remove standalone `adapter.Register()` calls from their `init()` functions. Each provider MUST have exactly 1 `init()` with 1 `providers.Register()` call after this task.

- **SLO**: Move `adapter.Register()` from `slo/definitions/resource_adapter.go init()` into `SLOProvider.TypedRegistrations()`. Collapse to 1 init in `slo/provider.go`.
- **Synth**: Move 2 `adapter.Register()` calls from `synth/provider.go init()` into `SynthProvider.TypedRegistrations()`. Already has 1 init.
- **Fleet**: Move 2 `adapter.Register()` calls from `fleet/provider.go init()` into `FleetProvider.TypedRegistrations()`. Already has 1 init.
- **KG**: Move `adapter.Register()` from `kg/resource_adapter.go init()` into `KGProvider.TypedRegistrations()`. Collapse to 1 init in `kg/provider.go`.

Update `TypedCRUD` instantiations to use `ResourceIdentity` methods (remove `NameFn`/`RestoreNameFn` usage, rely on `T.GetResourceName()`/`T.SetResourceName()` fallback from T5).

**Deliverables:**
- `internal/providers/slo/provider.go`
- `internal/providers/slo/definitions/resource_adapter.go` (remove init, keep factory functions)
- `internal/providers/synth/provider.go`
- `internal/providers/fleet/provider.go`
- `internal/providers/kg/provider.go`
- `internal/providers/kg/resource_adapter.go` (remove init, keep factory functions)

**Acceptance criteria:**
- GIVEN the SLO provider package
  WHEN `grep -c 'func init()' internal/providers/slo/**/*.go` is run
  THEN the count is exactly 1
- GIVEN the KG provider package
  WHEN `grep -c 'func init()' internal/providers/kg/**/*.go` is run
  THEN the count is exactly 1
- GIVEN `SLOProvider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 1 registration with GVK matching SLO definitions
- GIVEN `SynthProvider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 2 registrations (checks and probes)
- GIVEN `FleetProvider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 2 registrations (pipeline and collector)
- All existing tests for these 4 providers MUST continue to pass
- `make lint` MUST pass

---

### T8: Consolidate Registration — OnCall, Incidents, Alert

**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T6, T3
**Type**: task

Migrate 3 providers to return their registrations from `TypedRegistrations()` and collapse to 1 init per provider.

- **OnCall**: Move `RegisterAdapters()` call from `oncall/provider.go init()` into `OnCallProvider.TypedRegistrations()`. The `registerOnCallResource` helper MUST be refactored to build and return `Registration` objects instead of calling `adapter.Register()` directly. Remove the `nameFn` parameter since `T` now implements `ResourceIdentity`. Collapse to 1 init.
- **Incidents**: Move `adapter.Register()` from `incidents/resource_adapter.go init()` into `IncidentsProvider.TypedRegistrations()`. Collapse to 1 init in `incidents/provider.go`.
- **Alert**: Move 2 `adapter.Register()` calls from `alert/resource_adapter.go init()` into `AlertProvider.TypedRegistrations()`. Collapse to 1 init in `alert/provider.go`.

**Deliverables:**
- `internal/providers/oncall/provider.go`
- `internal/providers/oncall/resource_adapter.go` (refactor to return registrations)
- `internal/providers/incidents/provider.go`
- `internal/providers/incidents/resource_adapter.go` (remove init)
- `internal/providers/alert/provider.go`
- `internal/providers/alert/resource_adapter.go` (remove init)

**Acceptance criteria:**
- GIVEN each of oncall, incidents, alert provider packages
  WHEN `func init()` occurrences are counted across all `.go` files in the package
  THEN the count is exactly 1
- GIVEN `OnCallProvider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 17 registrations
- GIVEN `IncidentsProvider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 1 registration
- GIVEN `AlertProvider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 2 registrations (RuleStatus, RuleGroup)
- The `registerOnCallResource` helper MUST NOT call `adapter.Register()` directly
- All existing tests for these 3 providers MUST continue to pass
- `make lint` MUST pass

---

### T9: Consolidate Registration — K6

**Priority**: P1
**Effort**: Medium
**Depends on**: T6, T4
**Type**: task

Migrate K6 provider to return its registrations from `TypedRegistrations()`. The loop in `k6/resource_adapter.go init()` that calls `adapter.Register()` for each of 5 resource types MUST be moved into `K6Provider.TypedRegistrations()`. Collapse to 1 init in `k6/provider.go`.

**Deliverables:**
- `internal/providers/k6/provider.go`
- `internal/providers/k6/resource_adapter.go` (remove init, keep factory functions)

**Acceptance criteria:**
- GIVEN the K6 provider package
  WHEN `func init()` occurrences are counted across all `.go` files in the package
  THEN the count is exactly 1
- GIVEN `K6Provider.TypedRegistrations()`
  WHEN called
  THEN it returns exactly 5 registrations (Project, LoadTest, Schedule, EnvVar, LoadZone)
- All existing K6 tests MUST continue to pass
- `make lint` MUST pass

---

## Wave 5: Provider CRUD Command Migration

### T10: Migrate Provider CRUD Commands — SLO, Synth, Fleet

**Priority**: P2
**Effort**: Medium-Large
**Depends on**: T7
**Type**: task

Migrate SLO definitions, Synth checks/probes, and Fleet pipeline/collector CLI commands to use `TypedCRUD` typed methods (`List()`, `Get()`, `Create()`, `Update()`, `Delete()`) instead of calling REST clients directly.

For each provider:
1. Create a shared typed factory function that returns `*TypedCRUD[T]` (reused by both CLI commands and adapter registration).
2. Refactor CLI command `RunE` functions to obtain a `*TypedCRUD[T]` via the shared factory and call typed methods.
3. Remove direct REST client instantiation from CLI command handlers.

Table-format codecs (e.g., `sloTableCodec`) continue to operate on `[]T` — the `List()` typed method returns `[]TypedObject[T]` directly, which wraps the domain type the table codec needs.

**Deliverables:**
- `internal/providers/slo/definitions/commands.go` (refactored)
- `internal/providers/slo/definitions/resource_adapter.go` (shared factory)
- `internal/providers/synth/checks/commands.go` (refactored, if exists)
- `internal/providers/synth/probes/commands.go` (refactored, if exists)
- `internal/providers/fleet/provider.go` (refactored command handlers)

**Acceptance criteria:**
- GIVEN the SLO `list` command
  WHEN executed
  THEN it uses `TypedCRUD[Slo].List()` and produces identical output to the current implementation
- GIVEN the SLO `get` command with a UUID argument
  WHEN executed
  THEN it uses `TypedCRUD[Slo].Get()` and produces identical output
- GIVEN the SLO `push` command with a file argument
  WHEN executed
  THEN it uses `TypedCRUD[Slo].Create()` or `Update()` via the typed methods
- GIVEN any CLI command in these 3 providers
  WHEN its `RunE` function is inspected
  THEN it does NOT instantiate a REST client directly (uses TypedCRUD instead)
- All existing command tests MUST continue to pass
- External CLI behavior (output, exit codes, error messages) MUST NOT change

---

### T11: Migrate Provider CRUD Commands — OnCall, K6, KG, Incidents, Alert

**Priority**: P2
**Effort**: Large
**Depends on**: T8, T9, T10
**Type**: task

Migrate remaining 5 providers' CLI commands to use `TypedCRUD` typed methods. This is the largest task because OnCall has 17 resource type command groups and K6 has 5.

For providers with commands that currently call REST clients directly (OnCall, K6, KG, Incidents, Alert):
1. Create shared typed factory functions returning `*TypedCRUD[T]`.
2. Refactor CLI command `RunE` functions to use typed methods.
3. Remove direct REST client calls from command handlers.

OnCall's `commands.go` and `commands_extra.go` contain many command groups. Each MUST be migrated to use the corresponding `TypedCRUD[T]` instance.

**Deliverables:**
- `internal/providers/oncall/commands.go` (refactored)
- `internal/providers/oncall/commands_extra.go` (refactored)
- `internal/providers/k6/commands.go` (refactored)
- `internal/providers/kg/commands.go` (refactored)
- `internal/providers/incidents/commands.go` (refactored)
- `internal/providers/alert/resource_adapter.go` or commands file (refactored)

**Acceptance criteria:**
- GIVEN any OnCall resource command (e.g., `oncall integrations list`)
  WHEN its `RunE` function is inspected
  THEN it does NOT instantiate a REST client directly
- GIVEN any K6 resource command (e.g., `k6 projects list`)
  WHEN its `RunE` function is inspected
  THEN it does NOT instantiate a REST client directly
- GIVEN the `incidents list` command
  WHEN executed
  THEN it uses `TypedCRUD[Incident].List()` and produces identical output
- External CLI behavior (output, exit codes, error messages) MUST NOT change for any provider command
- All existing command tests for these 5 providers MUST continue to pass
- `make tests` MUST pass

---

## Wave 6: Cleanup and Documentation

### T12: Remove NameFn/RestoreNameFn and Dead Code

**Priority**: P2
**Effort**: Medium
**Depends on**: T11
**Type**: chore

Remove `NameFn` and `RestoreNameFn` fields from `TypedCRUD[T]`. Update `toUnstructured` and `fromUnstructured` to use `T.GetResourceName()`/`T.SetResourceName()` exclusively (remove the nil-check fallback added in T5). Remove any remaining `NameFn`/`RestoreNameFn` assignments across all providers. Verify no code references these fields.

**Deliverables:**
- `internal/resources/adapter/typed.go` (fields removed)
- All provider files that previously set `NameFn`/`RestoreNameFn` (field assignments removed)

**Acceptance criteria:**
- GIVEN the `TypedCRUD` struct definition
  WHEN inspected
  THEN it does NOT contain `NameFn` or `RestoreNameFn` fields
- GIVEN a codebase-wide search for `NameFn` and `RestoreNameFn`
  WHEN executed
  THEN zero results are found in production code (test code may reference for historical context)
- `toUnstructured` MUST call `T.GetResourceName()` unconditionally
- `fromUnstructured` MUST call `T.SetResourceName()` unconditionally
- `make all` MUST pass (lint + tests + build + docs)

---

### T13: CONSTITUTION.md and DESIGN.md Updates

**Priority**: P2
**Effort**: Small
**Depends on**: T12
**Type**: chore

Update CONSTITUTION.md with new invariants (FR-015):
1. Replace "Self-registering providers" with unified `Provider.TypedRegistrations()` pattern (providers own adapter registrations, single `init()` per provider, `providers.Register()` populates both registries atomically)
2. Add invariant: all provider domain types MUST implement `ResourceIdentity`
3. Add invariant: provider CRUD commands MUST use `TypedCRUD[T]` for data access, not raw API clients
4. Add invariant: all `ResourceAdapter` implementations MUST provide `Schema()` and `Example()` (codifying PR #18 convention)
5. Update "Typed resource trajectory" paragraph to reflect TypedObject[T] and ResourceIdentity are implemented (not aspirational)

Update DESIGN.md (FR-016):
1. Update Package Map to include `ResourceIdentity`, `TypedObject[T]`, `SchemaFromType[T]` in `internal/resources/adapter/`
2. Update Provider System description to reflect `TypedRegistrations()` replacing `ResourceAdapters()`
3. Add or update ADR table entry for ADR 004 (this work)
4. Include all provider packages currently missing from the Package Map (oncall, fleet, k6, kg, incidents)
5. Document Schema()/Example() convention from PR #18

**Deliverables:**
- `CONSTITUTION.md`
- `DESIGN.md`

**Acceptance criteria:**
- GIVEN CONSTITUTION.md
  WHEN inspected
  THEN it contains invariants about ResourceIdentity, TypedCRUD constraint, single init per provider, TypedCRUD typed methods for provider commands, TypedRegistrations() source of truth, and Schema()/Example() requirement
- GIVEN DESIGN.md
  WHEN inspected
  THEN it documents ResourceIdentity, TypedObject[T], SchemaFromType[T], the unified registration flow, all 8 provider packages, and Schema()/Example() convention
- GIVEN DESIGN.md ADR table
  WHEN inspected
  THEN ADR 004 for "Typed Resource Adapter Compliance" is listed
- GIVEN DESIGN.md registration flow
  WHEN inspected
  THEN it shows `providers.Register()` calling `adapter.Register()` atomically (not two separate paths)
- `make docs` MUST pass (no docs drift)

---
type: feature-spec
title: "Table-driven TypedCRUD[T] for OnCall Adapter"
status: done
beads_id: gcx-experiments-ghh
created: 2026-03-24
---

# Table-driven TypedCRUD[T] for OnCall Adapter

## Problem Statement

The OnCall provider uses a `subResourceAdapter` struct that dispatches all 17 resource types through 5 switch blocks (`listRaw`, `getRaw`, `createRaw`, `updateRaw`, `deleteRaw`) — totalling 569 LOC in `internal/providers/oncall/resource_adapter.go`. This pattern has three problems:

1. **Type erasure**: All CRUD methods use `any` + `toAnySlice`, losing compile-time type safety. A mismatched type in a switch case compiles but panics at runtime.
2. **Switch bloat**: Every new OnCall resource requires adding a case to up to 5 switch statements, each duplicating the same marshal/unmarshal boilerplate.
3. **Inconsistency**: The incidents, fleet, and knowledge-graph providers already use `TypedCRUD[T]`. OnCall is the last provider using the old switch-dispatch pattern.

Additionally, the current adapter does not wire all client capabilities: `ResolutionNote` (create/update), `ShiftSwap` (create/update), and `PersonalNotificationRule` (create/update/delete) have client methods but no adapter wiring, silently returning "not supported" errors.

The current workaround is to maintain the switch-based code manually, adding cases for each new resource type and CRUD verb.

## Scope

### In Scope

- Replace `subResourceAdapter` and its 5 switch-dispatch methods with 17 `registerOnCallResource[T]` calls using `TypedCRUD[T]`
- Implement a generic `registerOnCallResource[T]` registration helper with functional options (`withCreate`, `withUpdate`, `withDelete`) for optional write operations
- Handle the 3 special-case types where create/update use different request types: `Shift` (uses `ShiftRequest`), `ResolutionNote` (uses `Create/UpdateResolutionNoteInput`), `ShiftSwap` (uses `Create/UpdateShiftSwapInput`)
- Wire all CRUD operations that the `*Client` already supports (see CRUD matrix below)
- Delete dead code: `subResourceAdapter`, `toAnySlice`, `fromResource[T]`, `itemToResource`, `resourceDef`, `allResources()`
- Preserve all 17 resource type registrations with identical descriptors (kind, singular, plural, group, version) and aliases
- Preserve existing schema and example JSON for Integration (and any future schemas)
- Ensure existing tests in `resource_adapter_test.go` pass without modification
- Handle list methods that take filter parameters (e.g., `ListEscalationPolicies(ctx, "")`) by passing empty string (no filter), preserving current behavior
- Handle `UserGroup` and `SlackChannel` which have `List` but no `Get` client method

### Out of Scope

- Adding new OnCall resource types — this spec covers only the 17 existing types
- Changing the `*Client` method signatures or adding new client methods
- Modifying `TypedCRUD[T]` in `internal/resources/adapter/typed.go` — the existing generic is used as-is
- Adding `Verbs []string` metadata to `Registration` — deferred per ADR follow-up
- Extracting `registerOnCallResource[T]` to a shared utility — deferred until other providers need it
- Adding behavioral tests for CRUD operations (adapter behavior tests) — existing tests are round-trip only
- Changing the provider commands (`list`, `get`, etc.) or the `OnCallProvider` struct

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Registration pattern | Generic `registerOnCallResource[T]` called 17 times in `init()` | Eliminates all switch blocks; each registration is self-documenting (~10 LOC) | ADR-001 |
| Optional write operations | Functional options (`withCreate`, `withUpdate`, `withDelete`) | Only 7/17 types support create, 7 update, 8 delete — options express this cleanly without nil checks | ADR-001 |
| Special-case type conversion | Custom closures in `withCreate`/`withUpdate` for Shift, ResolutionNote, ShiftSwap | These types use different request structs for write ops; closures handle the conversion inline | ADR-001, codebase analysis |
| Wire all client-supported CRUD | Wire every operation the `*Client` exposes, not just what the old adapter wired | The old adapter silently dropped ResolutionNote create/update, ShiftSwap create/update, and PersonalNotificationRule create/update/delete — these are valid operations the client supports | ADR-001 CRUD matrix |
| UserGroup and SlackChannel Get | Use `nil` GetFn (or omit) since client has no Get method | These are list-only resources; TypedCRUD handles nil GetFn by returning ErrUnsupported | Codebase analysis |
| StripFields | `[]string{"id"}` for all 17 types | All OnCall types use `id` as the server-managed identity field, consistent with current behavior | Existing code |
| resourceMeta struct | Replace `resourceDef` with a simpler `resourceMeta` holding Descriptor, Aliases, Schema, Example | resourceDef carried idField which is no longer needed; resourceMeta is lighter | ADR-001 |

## Functional Requirements

- FR-001: The system MUST register exactly 17 OnCall resource types via `adapter.Register()` in the oncall package `init()` function, one for each type: Integration, EscalationChain, EscalationPolicy, Schedule, Shift, Route, OutgoingWebhook, AlertGroup, User, Team, UserGroup, SlackChannel, Alert, Organization, ResolutionNote, ShiftSwap, PersonalNotificationRule.

- FR-002: Each registration MUST produce a `TypedCRUD[T]`-backed `ResourceAdapter` via `AsAdapter()`, where `T` is the concrete domain type from `types.go` (e.g., `TypedCRUD[Integration]`, `TypedCRUD[Shift]`).

- FR-003: The system MUST implement a generic `registerOnCallResource[T any]` function that accepts: an `OnCallConfigLoader`, resource metadata, a name-extraction function, list and get functions, and variadic CRUD options.

- FR-004: The system MUST implement functional option types `withCreate[T]`, `withUpdate[T]`, and `withDelete[T]` that set the corresponding `CreateFn`, `UpdateFn`, `DeleteFn` on the `TypedCRUD[T]` instance.

- FR-005: Each registered adapter MUST use `StripFields: []string{"id"}` to remove the `id` field from the spec when converting domain objects to unstructured envelopes.

- FR-006: Each registered adapter MUST preserve the exact same `Descriptor` (Group: `oncall.ext.grafana.app`, Version: `v1alpha1`, Kind, Singular, Plural) and `Aliases` as the current `allResources()` definitions.

- FR-007: The `Shift` registration MUST convert `Shift` to `ShiftRequest` for create and update operations, using JSON marshal/unmarshal, matching the current `createRaw`/`updateRaw` behavior.

- FR-008: The `ResolutionNote` registration MUST convert `ResolutionNote` to `CreateResolutionNoteInput` for create and to `UpdateResolutionNoteInput` for update, extracting the appropriate fields.

- FR-009: The `ShiftSwap` registration MUST convert `ShiftSwap` to `CreateShiftSwapInput` for create and to `UpdateShiftSwapInput` for update, extracting the appropriate fields.

- FR-010: The following types MUST support Create: Integration, EscalationChain, EscalationPolicy, Schedule, Shift, Route, OutgoingWebhook, ResolutionNote, ShiftSwap, PersonalNotificationRule.

- FR-011: The following types MUST support Update: Integration, EscalationChain, EscalationPolicy, Schedule, Shift, Route, OutgoingWebhook, ResolutionNote, ShiftSwap, PersonalNotificationRule.

- FR-012: The following types MUST support Delete: Integration, EscalationChain, EscalationPolicy, Schedule, Shift, Route, OutgoingWebhook, AlertGroup, ResolutionNote, ShiftSwap, PersonalNotificationRule.

- FR-013: The following types MUST NOT support Create, Update, or Delete (read-only): User, Team, UserGroup, SlackChannel, Alert, Organization.

- FR-014: The following types MUST NOT support Get (list-only): UserGroup, SlackChannel. Calling Get on these types MUST return `errors.ErrUnsupported`.

- FR-015: List functions for EscalationPolicy, Route, Alert, and ResolutionNote MUST pass an empty string as the filter parameter to the client method (e.g., `client.ListEscalationPolicies(ctx, "")`), preserving current behavior.

- FR-016: The system MUST delete the following code artifacts: `subResourceAdapter` struct and all its methods, `toAnySlice` function, `fromResource[T]` function in `adapter.go`, `itemToResource` method, `resourceDef` struct, and `allResources()` function.

- FR-017: The Integration resource registration MUST include the existing `integrationSchema()` and `integrationExample()` JSON payloads in its `Registration.Schema` and `Registration.Example` fields.

- FR-018: The `NameFn` for each type MUST extract the `ID` field (the `id` JSON tag) from the domain object, matching the current behavior where `metadata.name` is set to the resource's API ID.

## Acceptance Criteria

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

- GIVEN the refactored codebase
  WHEN `make lint` is run
  THEN no new lint errors are introduced

- GIVEN the refactored codebase
  WHEN searching for `subResourceAdapter`, `toAnySlice`, `fromResource[T]`, `resourceDef`, or `allResources()` in the oncall package
  THEN none of these symbols exist

- The system SHALL register all 17 OnCall resources using the `registerOnCallResource[T]` generic function — no resource SHALL use direct `adapter.Register` calls or manual `TypedCRUD` construction outside this helper

- WHEN the `registerOnCallResource[T]` function is called with no `crudOption` arguments
  THEN the resulting adapter supports only List and Get (or only List if GetFn is nil), and Create/Update/Delete return `errors.ErrUnsupported`

## Negative Constraints

- NEVER use `any` type for domain objects in the adapter layer — all 17 types MUST use their concrete Go types via generics
- NEVER use type switches or kind-string dispatch to select CRUD behavior — each type's behavior MUST be determined at registration time via `registerOnCallResource[T]` and functional options
- DO NOT modify `internal/resources/adapter/typed.go` — use the existing `TypedCRUD[T]` API as-is
- DO NOT modify `internal/providers/oncall/types.go` — all domain types and input types remain unchanged
- DO NOT modify `internal/providers/oncall/client.go` — all client methods remain unchanged
- DO NOT modify `internal/providers/oncall/resource_adapter_test.go` — existing tests MUST pass as-is
- DO NOT change the `OnCallProvider` struct, its methods, or the provider CLI commands
- NEVER add a `Get` method for `UserGroup` or `SlackChannel` — these are list-only resources per the OnCall API

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| JSON marshal/unmarshal round-trip for Shift → ShiftRequest loses fields that exist in ShiftRequest but not Shift (e.g., `rolling_users`, `week_start`, `start_rotation_from_user_index`) | Create/update of shifts with these fields silently drops them | Verify field coverage between Shift and ShiftRequest types; accept this as pre-existing behavior since the current adapter has the same limitation |
| Newly wired CRUD operations (ResolutionNote create/update, ShiftSwap create/update, PersonalNotificationRule CRUD) were not previously tested via the adapter | Regressions in untested paths | These operations are wired to existing, tested client methods; add integration test coverage in follow-up |
| `UserGroup` and `SlackChannel` lack `Get` client methods, requiring special handling in `registerOnCallResource` | Registration helper signature assumes GetFn is required | Allow nil GetFn parameter — TypedCRUD already handles nil GetFn by delegating to typedAdapter which will return ErrUnsupported, OR pass a stub that returns ErrUnsupported |
| Init-time registration order may differ from current `allResources()` slice order | Discovery listing order changes | Registration order does not affect correctness; the discovery registry does not guarantee ordering |

## Open Questions

- [RESOLVED]: The `registerOnCallResource[T]` signature in the ADR shows `GetFn` as required, but `UserGroup` and `SlackChannel` have no client Get method — Pass a nil `GetFn`; TypedCRUD handles nil GetFn by returning ErrUnsupported via the typedAdapter delegation. This is consistent with Key Decisions row 5.

- [NEEDS CLARIFICATION]: ResolutionNote and ShiftSwap have `DeleteResolutionNote` and `DeleteShiftSwap` methods on the client, but the current adapter does not wire delete for these types. The ADR CRUD matrix marks them as "no*" with a note. This spec wires delete for both (FR-012) since the client methods exist and there is no reason to suppress them. If this is incorrect, FR-012 MUST be amended before implementation.

- [DEFERRED]: Adding `Verbs []string` to `Registration` so that the system can report which CRUD verbs a resource supports without instantiating the adapter. Will address in a follow-up spec per ADR.

- [DEFERRED]: Extracting `registerOnCallResource[T]` to a shared `adapter` utility if other providers grow beyond 4 resource types. Will address if/when needed.

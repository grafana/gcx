---
type: feature-spec
title: "TypedResourceAdapter[T] Foundation"
status: approved
beads_id: grafanactl-experiments-bq3
created: 2026-03-20
---

# TypedResourceAdapter[T] Foundation

## Problem Statement

Every provider resource adapter in grafanactl repeats ~200 lines of boilerplate code: static descriptor/alias globals, a ResourceAdapter struct, Descriptor()/Aliases() methods, and List/Get/Create/Update/Delete implementations that each perform the same JSON marshal/unmarshal, K8s envelope wrapping, name management, and namespace injection. The five existing adapters (SLO definitions, synth checks, synth probes, alert rules, alert groups) total ~1,100 LOC of near-identical scaffolding. The consolidation plan requires porting 40+ additional resource types from the cloud CLI; at 200 LOC each, this boilerplate would add ~8,000 LOC and proportional maintenance burden.

Developers adding new providers must manually replicate the marshal -> strip fields -> wrap envelope -> return Unstructured pattern, which is error-prone and violates DRY. There is no current workaround other than copy-pasting an existing adapter and modifying it.

## Scope

### In Scope

- A new `TypedResourceAdapter[T]` generic in `internal/resources/adapter/` that auto-generates a `ResourceAdapter` from typed CRUD function pointers
- A `TypedRegistration[T]` struct for wiring typed CRUD into the global adapter registration system
- JSON marshal/unmarshal between typed struct `T` and `map[string]any`
- K8s envelope construction (apiVersion, kind, metadata.name, metadata.namespace, spec)
- Configurable server-managed field stripping (per-provider strip list)
- Configurable name extraction (`NameFn`) and name restoration for round-trip fidelity
- Optional `MetadataFn func(T) map[string]any` for injecting custom metadata fields (e.g., metadata.uid) during K8s envelope construction
- Read-only adapter support (nil write function pointers auto-return `errors.ErrUnsupported`)
- Unit tests with mock typed CRUD verifying full round-trip (List, Get, Create, Update, Delete)
- Refactoring synth checks adapter to use `TypedResourceAdapter[CheckSpec]`
- Refactoring synth probes adapter to use `TypedResourceAdapter[Probe]`
- Refactoring SLO definitions adapter to use `TypedResourceAdapter[Slo]`
- Refactoring alert rules adapter to use `TypedResourceAdapter[RuleStatus]`
- Refactoring alert rule groups adapter to use `TypedResourceAdapter[RuleGroup]`

### Out of Scope

- Porting new resource types from the cloud CLI (Phase 1+ work; depends on this foundation)
- Changes to the `ResourceAdapter` interface itself (existing interface is preserved)
- Changes to the `ResourceClientRouter` or `Registration` struct (they continue to work unchanged)
- Changes to provider REST clients (`checks.Client`, `probes.Client`, `definitions.Client`, `alert.Client`) -- these are reused as-is
- Changes to provider type definitions (`Check`, `CheckSpec`, `Slo`, `Probe`, `RuleStatus`, `RuleGroup`)
- Modifying the `Factory` type signature or lazy initialization pattern
- Provider-specific helper functions that remain useful outside the adapter (e.g., `FileNamer`, `slugifyJob`, `extractIDFromSlug`)

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Generic parameter is the API-level typed struct, not the spec-only struct | Use the full API struct (e.g., `Slo`, `Check`) as `T`, not a spec-only subset | NameFn needs access to identifier fields (UUID, ID, UID) that live on the API struct, not the spec. StripFields removes them from the serialized spec. | Codebase analysis of existing ToResource patterns |
| Strip fields via configurable string slice, not struct tags | `StripFields []string` on `TypedCRUD[T]` | Each provider strips different fields (SLO: uuid/readOnly; checks: id/tenantId/created/modified/channels; probes: id/tenantId/created/modified/onlineChange/online/version; alert rules: uid; groups: name). A string slice is simpler than struct tag parsing and matches the existing `delete(specMap, field)` pattern. | Codebase analysis |
| TypedCRUD function pointers use closures for extra dependencies | Closures capture extra clients (e.g., probesClient for checks, tenant resolution) rather than adding fields to TypedCRUD | Keeps the generic simple. Checks adapter needs probeNameMap/probeIDMap and tenant ID -- these are naturally expressed as closures over the checks factory's captured clients. | Consolidation plan Task 0.2 |
| Nil write function pointers auto-return ErrUnsupported | When `CreateFn`, `UpdateFn`, or `DeleteFn` is nil, the generated adapter returns `errors.ErrUnsupported` | Three of five existing adapters are read-only (probes, alert rules, alert groups). This eliminates three-method boilerplate stubs per read-only adapter. | Codebase analysis of probes/alert adapters |
| GetFn receives the K8s metadata.name string, provider does name-to-ID parsing | `GetFn func(ctx context.Context, name string) (*T, error)` | Some providers need slug-to-ID extraction (checks), some use the name directly (SLO UUID, alert rule UID). Parsing logic belongs in the closure, not the generic. | Codebase analysis of checks Get vs SLO Get |
| UpdateFn receives name + spec separately | `UpdateFn func(ctx context.Context, name string, spec *T) (*T, error)` | SLO Update needs UUID extracted from metadata.name. Checks Update needs ID extracted from slug. Passing name separately lets the closure handle parsing. | Codebase analysis |
| FromResource restores identity via RestoreNameFn | `RestoreNameFn func(name string, item *T)` optional callback applied after JSON unmarshal from spec | SLO restores UUID, alert rules restore UID, alert groups restore Name. Checks recover numeric ID from slug. Without this, round-trip Create/Update loses the identifier. | Codebase analysis of existing FromResource functions |
| Checks adapter uses CheckSpec (not Check) as T with custom ListFn | `TypedCRUD[CheckSpec]` where ListFn internally calls client.List, maps Check->CheckSpec with probe name resolution | CheckSpec is what gets serialized to/from the K8s spec field. The ListFn closure handles the Check->CheckSpec transformation and probe name lookup. | Codebase analysis of checks ToResource pattern |
| Optional MetadataFn for custom metadata fields | `MetadataFn func(T) map[string]any` on `TypedCRUD[T]`, merged into metadata during envelope construction | Providers like synth checks need to set metadata.uid or other custom metadata fields. A function returning a map is more flexible than adding individual fields to TypedCRUD and keeps provider-specific metadata logic in closures. | Open question resolution -- synth checks sets metadata.uid as secondary ID source |

## Functional Requirements

- FR-001: The system MUST provide a generic `TypedCRUD[T any]` struct in `internal/resources/adapter/typed.go` that holds typed function pointers: `NameFn`, `ListFn`, `GetFn`, `CreateFn`, `UpdateFn`, `DeleteFn`, plus `Namespace string`, `StripFields []string`, `RestoreNameFn`, and `MetadataFn func(T) map[string]any`.

- FR-002: The system MUST provide a `TypedRegistration[T any]` struct that holds `Descriptor`, `Aliases`, `GVK`, and a `Factory func(ctx context.Context) (*TypedCRUD[T], error)`.

- FR-003: `TypedRegistration[T]` MUST provide a `ToRegistration()` method that returns an `adapter.Registration` compatible with the existing `adapter.Register()` function.

- FR-004: `TypedCRUD[T]` MUST provide an `AsAdapter(desc resources.Descriptor, aliases []string) ResourceAdapter` method (or equivalent) that returns a `ResourceAdapter` implementation.

- FR-005: The generated `List` method MUST call `ListFn`, JSON-marshal each returned `T`, delete all fields named in `StripFields` from the marshaled map, wrap each in a K8s envelope with the adapter's apiVersion/kind/metadata.name (via `NameFn`)/metadata.namespace, merge any fields returned by `MetadataFn` (if non-nil) into the envelope's metadata, and return an `*unstructured.UnstructuredList`.

- FR-006: The generated `Get` method MUST call `GetFn` with the provided name string, JSON-marshal the result, strip fields, wrap in a K8s envelope, merge any fields returned by `MetadataFn` (if non-nil) into the envelope's metadata, and return an `*unstructured.Unstructured`.

- FR-007: The generated `Create` method MUST extract the spec map from the input `*unstructured.Unstructured`, JSON-unmarshal it to `*T`, apply `RestoreNameFn` if non-nil (passing `obj.GetName()`), call `CreateFn`, then marshal and wrap the result as a K8s envelope `*unstructured.Unstructured`.

- FR-008: The generated `Update` method MUST extract the spec map from the input `*unstructured.Unstructured`, JSON-unmarshal it to `*T`, apply `RestoreNameFn` if non-nil (passing `obj.GetName()`), call `UpdateFn` with the name from `obj.GetName()` and the unmarshaled `*T`, then marshal and wrap the result.

- FR-009: The generated `Delete` method MUST call `DeleteFn` with the name string extracted from the method parameter.

- FR-010: When `CreateFn` is nil, the generated `Create` method MUST return `nil, errors.ErrUnsupported`.

- FR-011: When `UpdateFn` is nil, the generated `Update` method MUST return `nil, errors.ErrUnsupported`.

- FR-012: When `DeleteFn` is nil, the generated `Delete` method MUST return `errors.ErrUnsupported`.

- FR-013: The generated `Descriptor()` method MUST return the descriptor provided at construction time.

- FR-014: The generated `Aliases()` method MUST return the aliases provided at construction time.

- FR-015: The synth checks adapter (`internal/providers/synth/checks/resource_adapter.go`) MUST be refactored to use `TypedRegistration`/`TypedCRUD` instead of the hand-written `ResourceAdapter` struct.

- FR-016: The synth probes adapter (`internal/providers/synth/probes/resource_adapter.go`) MUST be refactored to use `TypedRegistration`/`TypedCRUD`.

- FR-017: The SLO definitions adapter (`internal/providers/slo/definitions/resource_adapter.go`) MUST be refactored to use `TypedRegistration`/`TypedCRUD`.

- FR-018: The alert rules adapter and alert groups adapter (`internal/providers/alert/resource_adapter.go`) MUST be refactored to use `TypedRegistration`/`TypedCRUD`.

- FR-019: The existing `adapter.go` files containing `ToResource`/`FromResource` helpers in each provider MUST be removed or significantly reduced, with their logic absorbed into `TypedCRUD` function pointers and `StripFields`/`NameFn`/`RestoreNameFn`/`MetadataFn` configuration.

- FR-020: The `TypedCRUD[T]` K8s envelope construction MUST use `resources.MustFromObject` to maintain consistency with existing adapters.

- FR-021: The `TypedCRUD[T]` spec extraction from `*unstructured.Unstructured` MUST use `resources.FromUnstructured` followed by reading the `"spec"` key, matching the existing pattern.

- FR-022: When `MetadataFn` is non-nil, the generated envelope construction MUST call `MetadataFn` with the typed item and merge each key-value pair from the returned map into the `metadata` object of the K8s envelope. `MetadataFn` output MUST NOT overwrite `metadata.name` or `metadata.namespace` (these are always set by the generic).

## Acceptance Criteria

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

- GIVEN the refactored synth checks adapter using `TypedCRUD[CheckSpec]`
  WHEN `grafanactl resources get checks` is executed against a Synthetic Monitoring API
  THEN the output is byte-for-byte identical to the output produced by the pre-refactor adapter

- GIVEN the refactored synth probes adapter using `TypedCRUD[Probe]`
  WHEN `grafanactl resources get probes` is executed
  THEN the output is identical to the pre-refactor adapter output

- GIVEN the refactored SLO definitions adapter using `TypedCRUD[Slo]`
  WHEN `grafanactl resources get slos` is executed
  THEN the output is identical to the pre-refactor adapter output

- GIVEN the refactored alert rules and groups adapters
  WHEN `grafanactl resources get alertrules` and `grafanactl resources get alertrulegroups` are executed
  THEN the output is identical to the pre-refactor adapter output

- GIVEN all refactored adapters
  WHEN `make all` is run (lint + tests + build + docs)
  THEN it completes with exit code 0

- GIVEN the refactored synth checks adapter
  WHEN a check is created via `grafanactl resources push` with a check YAML
  THEN the check is created successfully with probe names resolved to IDs

- GIVEN the refactored synth checks adapter
  WHEN a check is updated via `grafanactl resources push` with a modified check YAML
  THEN the check is updated successfully with the numeric ID recovered from metadata.name

- GIVEN the refactored SLO definitions adapter
  WHEN an SLO is created and then updated via `grafanactl resources push`
  THEN the SLO is created/updated successfully with UUID managed via metadata.name

## Negative Constraints

- NEVER change the `ResourceAdapter` interface signature -- `TypedCRUD[T]` MUST implement the existing interface without modification.
- NEVER change the `Registration` struct, `Register()`, `AllRegistrations()`, or `RegisterAll()` function signatures.
- NEVER change the `Factory` type signature (`func(ctx context.Context) (ResourceAdapter, error)`).
- NEVER modify provider REST client implementations (`checks.Client`, `probes.Client`, `definitions.Client`, `alert.Client`).
- NEVER modify provider type definitions (`Check`, `CheckSpec`, `Slo`, `Probe`, `RuleStatus`, `RuleGroup`).
- DO NOT introduce reflection-based field stripping -- use JSON marshal/unmarshal + map key deletion (the existing pattern).
- DO NOT add new dependencies to the `internal/resources/adapter/` package beyond what is already imported (standard library, `k8s.io/apimachinery`, `github.com/grafana/grafanactl/internal/resources`).
- NEVER expose `TypedCRUD[T]` internals through the `ResourceAdapter` interface -- it MUST remain an implementation detail behind the existing interface.
- DO NOT remove `slugifyJob`, `extractIDFromSlug`, `FileNamer`, or other provider-specific helpers that are used outside the adapter (e.g., by CLI commands or tests).
- `MetadataFn` output MUST NOT be allowed to overwrite `metadata.name` or `metadata.namespace` -- these fields are always controlled by `NameFn` and `Namespace`.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Checks adapter complexity (probe name resolution, tenant ID) does not fit cleanly into TypedCRUD closures | Medium -- checks adapter becomes harder to read than the hand-written version | Design the closure pattern with checks as the primary complex test case. If closures become unwieldy, keep checks-specific helper methods alongside the TypedCRUD wiring. |
| JSON marshal round-trip introduces subtle field ordering or type differences (e.g., int64 -> float64) | High -- output differs from pre-refactor, breaking tests or user workflows | Unit tests MUST verify round-trip fidelity. Run `make all` after each provider refactor. Compare actual CLI output before and after. |
| Alert rules List unwrapping (nested groups[].rules[]) is non-standard and may not fit the simple ListFn signature | Low -- ListFn is `func(ctx) ([]T, error)`, unwrapping can happen inside the closure | The closure can call client.List, iterate groups, flatten rules, and return `[]RuleStatus`. No generic change needed. |
| Go generics constraints may require additional type bounds beyond `any` | Low -- all operations use JSON marshal/unmarshal, not direct field access | The `any` constraint is sufficient since all type-specific logic lives in the function pointers, not in the generic code. |
| Refactoring all five adapters simultaneously risks merge conflicts with concurrent work | Medium -- other contributors may modify these files | Refactor checks first (Task 0.2) as proof of concept, then batch the remaining four (Task 0.3). Keep commits atomic per provider. |

## Open Questions

- [RESOLVED]: Should TypedCRUD use the API struct or spec struct as T? -- Use the API struct for providers where NameFn needs identifier fields (SLO, probes, alert rules/groups). Use the spec struct for checks where ToResource builds a separate CheckSpec. The generic supports both patterns since all type-specific logic lives in closures.
- [RESOLVED]: Should StripFields be a function instead of a string slice? -- String slice is sufficient. All existing providers use simple `delete(specMap, key)`. If a future provider needs conditional stripping, it can do so in a custom marshal step before the generic processes it.
- [RESOLVED]: Should TypedCRUD support custom metadata fields beyond name/namespace? -- Yes. Add an optional `MetadataFn func(T) map[string]any` to `TypedCRUD[T]`. This allows providers like synth checks to set `metadata.uid` or any other custom metadata fields during K8s envelope construction. `MetadataFn` output is merged into metadata but MUST NOT overwrite `metadata.name` or `metadata.namespace`.

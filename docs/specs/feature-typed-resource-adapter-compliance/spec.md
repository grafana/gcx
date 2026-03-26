---
type: feature-spec
title: "Typed Resource Adapter Compliance"
status: done
research: docs/research/2026-03-25-provider-registry-convergence.md
parent: docs/specs/feature-typed-resource-adapter-foundation/spec.md
beads_id: grafanactl-experiments-dvwd
created: 2026-03-26
---

# Typed Resource Adapter Compliance

## Problem Statement

grafanactl's provider/adapter architecture has three structural problems that compound as new providers are added:

1. **No typed access for provider commands.** `TypedCRUD[T any]` exposes no public typed methods -- only function pointers and an unstructured bridge via `AsAdapter()`. Provider CLI commands (e.g., `grafanactl slo definitions list`) call REST clients directly, duplicating CRUD logic that already exists in the adapter layer. Bug fixes must be applied to both code paths independently.

2. **Dual registration with 13 init() functions.** Provider identity (`providers.Register()`) and adapter registration (`adapter.Register()`) are disconnected global registries populated by separate `init()` functions across 8 providers. `Provider.ResourceAdapters()` returns nil in 6 of 8 providers -- it is dead code superseded by direct `adapter.Register()` calls. There is no atomic guarantee that a provider's CLI identity and its adapter registrations are consistent.

3. **Domain types lack self-describing identity.** `NameFn` and `RestoreNameFn` function pointers on `TypedCRUD` replicate what K8s metadata accessors (`GetName()`/`SetName()`) provide natively. Each adapter must configure these function pointers, preventing domain types from being used in generic K8s-aware code without adapter-specific wiring.

Additionally, CONSTITUTION.md and DESIGN.md are stale: they describe the pre-TypedCRUD architecture ("Self-registering providers use init() to register"), do not reflect the Schema/Example convention established in PR #18, and lack documentation for the TypedObject/ResourceIdentity patterns introduced by this work.

The current workaround is manual duplication: every provider CRUD command hand-wires REST client calls, every adapter configures function pointers for name mapping, and every new provider must replicate both patterns. The consolidation plan requires porting 40+ resource types from gcx -- each would perpetuate the dual code path and dual registration if the architecture is not addressed.

## Scope

### In Scope

- **ResourceIdentity interface** in `internal/resources/adapter/` with `GetResourceName() string` and `SetResourceName(string)` methods
- **ResourceIdentity implementation** on ALL existing domain types across ALL 8 providers: SLO (`Slo`), synth (`checkResource`, `Probe`), oncall (all 17 resource types including `Integration`, `Schedule`, `EscalationChain`, etc.), fleet (`Pipeline`, `Collector`), k6 (`Project`, `LoadTest`, `Schedule`, `EnvVar`, `LoadZone`), kg (`Rule`), incidents (`Incident`), alert (`RuleStatus`, `RuleGroup`)
- **TypedObject[T ResourceIdentity] envelope** in `internal/resources/adapter/` implementing `metav1.Object` via embedded `ObjectMeta`
- **TypedCRUD constraint tightening** from `TypedCRUD[T any]` to `TypedCRUD[T ResourceIdentity]`
- **Typed public methods** on TypedCRUD: `List`, `Get`, `Create`, `Update`, `Delete` returning `TypedObject[T]`
- **Removal of `NameFn` and `RestoreNameFn`** from TypedCRUD (replaced by ResourceIdentity)
- **Provider.TypedRegistrations() method** added to Provider interface, returning `[]adapter.Registration`
- **Removal of `Provider.ResourceAdapters()`** from Provider interface (dead code)
- **Unified registration** in `providers.Register()` that auto-registers adapters from `TypedRegistrations()`
- **Collapse of 13 init() functions to 8** (one per provider) by moving `adapter.Register()` calls into `TypedRegistrations()`
- **Provider CRUD command migration** from direct REST client calls to `TypedCRUD[T]` typed methods
- **Shared typed factory** `NewTypedCRUD(ctx) (*adapter.TypedCRUD[T], error)` per provider package
- **Hybrid spec-level serialization** -- TypedObject[T] for typed methods, JSON-map-strip-envelope for AsAdapter bridge
- **CONSTITUTION.md updates** -- new invariants for ResourceIdentity, Provider.TypedRegistrations(), TypedCRUD-based provider commands, Schema/Example convention
- **DESIGN.md updates** -- updated Package Map, architecture descriptions, ADR table entry for new types/patterns including PR #18 Schema/Example convention

### Out of Scope

- **Full registry convergence with ProviderMeta** -- introducing `ProviderMeta` type, moving ConfigKeys/Validate out of Provider interface, rewriting `providers list` from unified registry. This is follow-up work (phases 2-6 of the research report). The `TypedRegistrations()` approach gets 90% of the benefit with 10% of the effort.
- **Removal of Validate() from Provider interface** -- dead at runtime but removing it is a separate cleanup tracked in the research report.
- **Removal of ConfigKeys() from Provider interface** -- requires ProviderMeta type first.
- **StripFields elimination** -- investigating struct tags or separate spec types to replace JSON-map-delete. Tracked as follow-up.
- **Provider CRUD command deprecation/removal** -- commands will migrate to use TypedCRUD internally but will continue to exist as user-facing commands. Deprecation in favor of `grafanactl resources` is a separate decision.
- **SLO Reports adapter** -- Reports is read-only analytics with no CRUD adapter. Adding one is orthogonal.
- **Porting new resource types from gcx** -- depends on this foundation but is separate work.
- **Changes to the ResourceAdapter interface** -- the existing interface (List, Get, Create, Update, Delete, Descriptor, Aliases, Schema, Example) is preserved unchanged.
- **Architecture docs beyond CONSTITUTION.md and DESIGN.md** -- `docs/architecture/*.md` updates are tracked by the existing quality standard ("Architecture docs must stay current with code changes") but are not explicitly spec'd here beyond the two root documents.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| ResourceIdentity uses `GetResourceName()`/`SetResourceName()` method names | Two-method interface, not `GetName()`/`SetName()` | Avoids collision with `metav1.ObjectMeta.GetName()`/`SetName()` which are inherited by any type embedding ObjectMeta. Distinct names prevent ambiguity when TypedObject[T] has both ObjectMeta methods and T's identity methods. | ADR analysis |
| TypedObject[T] uses embedded ObjectMeta (not manual method forwarding) | `metav1.ObjectMeta` embedded field | ObjectMeta implements all ~30 `metav1.Object` methods via pointer receiver. Embedding gives free compliance with no boilerplate. Standard K8s CRD pattern. | ADR section 2 |
| Keep MetadataFn on TypedCRUD | Not removed (unlike NameFn/RestoreNameFn) | MetadataFn provides extra metadata beyond name/namespace (e.g., metadata.uid for synth checks). ResourceIdentity replaces only the name-specific function pointers. | ADR section 3 |
| Keep StripFields on TypedCRUD | Not removed | StripFields operates at the spec-map level during serialization. Eliminating it requires struct tags or separate spec types -- a larger change tracked as follow-up. | ADR section 6 |
| Add TypedRegistrations() to Provider interface (not full ProviderMeta) | Incremental approach | Gets atomic registration (provider + adapters in single call) without the larger refactor of extracting ConfigKeys/Validate into ProviderMeta. ~100-150 LOC change. Research report confirmed 90/10 benefit/effort ratio. | Research report |
| Provider commands migrate to TypedCRUD, not deprecated | Internal migration, external API unchanged | Commands switch from REST client to TypedCRUD internally. Users see no change. Deprecation is a separate future decision. | ADR section 5 |
| SetResourceName swallows parse errors for numeric ID types | Match current RestoreNameFn behavior | K6 (int), synth checks (int64), and probes (int64) have numeric IDs. Current RestoreNameFn silently ignores parse failures. ResourceIdentity formalizes this existing behavior. | ADR consequences |
| Doc updates are part of this spec, not a separate chore | CONSTITUTION.md + DESIGN.md updated in same work | New invariants (ResourceIdentity on all domain types, unified TypedRegistrations(), Schema/Example convention) must be codified before implementation agents assume old patterns. Stale docs cause wrong assumptions. | User requirement |

## Functional Requirements

- **FR-001**: The system MUST define a `ResourceIdentity` interface in `internal/resources/adapter/` with exactly two methods: `GetResourceName() string` and `SetResourceName(string)`.

- **FR-002**: ALL existing provider domain types used in resource adapters across ALL 8 providers MUST implement `ResourceIdentity`. This includes: SLO (`Slo`), synth (`checkResource`, `Probe`), oncall (all 17 resource types: `Integration`, `Schedule`, `EscalationChain`, `EscalationPolicy`, `Route`, `Team`, `User`, `UserGroup`, `OnCallShift`, `PersonalNotificationRule`, `Webhook`, `ResolutionNote`, `AlertGroup`, `Action`, `Heartbeat`, `MaintenanceWindow`, `SlackChannel`), fleet (`Pipeline`, `Collector`), k6 (`Project`, `LoadTest`, `Schedule`, `EnvVar`, `LoadZone`), kg (`Rule`), incidents (`Incident`), alert (`RuleStatus`, `RuleGroup`). Types with string identifiers MUST return the identifier directly. Types with numeric identifiers (K6 int, synth check int64, synth probe int64) MUST convert to/from string representation using `strconv`.

- **FR-003**: The system MUST define `TypedObject[T ResourceIdentity]` in `internal/resources/adapter/` as a struct with `metav1.TypeMeta` (inline), `metav1.ObjectMeta` (field name `metadata`), and `Spec T` (field name `spec`). `TypedObject[T]` MUST implement `metav1.Object` via embedded `ObjectMeta`.

- **FR-004**: `TypedCRUD` MUST change its constraint from `TypedCRUD[T any]` to `TypedCRUD[T ResourceIdentity]`.

- **FR-005**: `TypedCRUD[T]` MUST expose five new public typed methods: `List(ctx) ([]TypedObject[T], error)`, `Get(ctx, name) (*TypedObject[T], error)`, `Create(ctx, *TypedObject[T]) (*TypedObject[T], error)`, `Update(ctx, name, *TypedObject[T]) (*TypedObject[T], error)`, `Delete(ctx, name) error`.

- **FR-006**: `TypedCRUD[T]` MUST remove the `NameFn` and `RestoreNameFn` fields. Name extraction MUST use `T.GetResourceName()` and name restoration MUST use `T.SetResourceName()` (via the `ResourceIdentity` interface on the constraint).

- **FR-007**: `TypedCRUD[T]` MUST retain `MetadataFn` and `StripFields` fields with unchanged semantics.

- **FR-008**: `TypedCRUD[T].AsAdapter()` MUST continue to return a `ResourceAdapter` that works with the unstructured pipeline. The `typedAdapter[T]` bridge MUST use the hybrid serialization approach: JSON-map-strip-envelope for spec serialization to maintain backward compatibility with `StripFields`.

- **FR-009**: The `Provider` interface MUST gain a `TypedRegistrations() []adapter.Registration` method. Providers construct their registrations using `TypedRegistration[T].ToRegistration()` (which wraps a `TypedCRUD[T]` factory into a standard `adapter.Registration`). The full chain is: `TypedRegistration[T]` → `.ToRegistration()` → `Registration` → returned from `TypedRegistrations()` → consumed by `providers.Register()`.

- **FR-010**: The `Provider` interface MUST remove the `ResourceAdapters() []adapter.Factory` method. All 9 existing implementations (6 returning nil, 2 returning factories) MUST be deleted.

- **FR-011**: `providers.Register()` MUST auto-register adapters by iterating `p.TypedRegistrations()` and calling `adapter.Register()` for each. This makes provider registration atomic: a single `providers.Register(p)` call in `init()` populates both the provider registry AND the adapter registry. No separate `adapter.Register()` calls may exist outside `providers.Register()`.

- **FR-012**: Each of the 8 providers (slo, synth, oncall, fleet, k6, kg, incidents, alert) MUST consolidate its adapter registration into the `TypedRegistrations()` method, eliminating separate `init()` functions that call `adapter.Register()` directly. After migration, each provider MUST have exactly one `init()` function containing a single `providers.Register()` call.

- **FR-013**: ALL provider CRUD commands across ALL 8 providers (list, get, create/push, update, delete) MUST migrate from direct REST client calls to `TypedCRUD[T]` typed methods. Each provider package MUST expose a shared typed factory function `NewTypedCRUD(ctx) (*adapter.TypedCRUD[T], error)` (or equivalent per-resource-type variant for multi-resource providers like oncall with 17 types and k6 with 5 types). This factory is used by both CLI commands (typed access via `TypedObject[T].Spec`) and adapter registration (unstructured access via `AsAdapter()`).

- **FR-014**: The `TypedCRUD[T]` typed methods MUST build `TypedObject[T]` with correct `TypeMeta` (apiVersion, kind from Descriptor) and `ObjectMeta` (name from `T.GetResourceName()`, namespace from `TypedCRUD.Namespace`).

- **FR-015**: CONSTITUTION.md MUST be updated to:
  - Replace the "Self-registering providers" invariant with a description of unified `Provider.TypedRegistrations()` pattern (providers own adapter registrations, single `init()` per provider, `providers.Register()` populates both registries atomically)
  - Add an invariant that all provider domain types MUST implement `ResourceIdentity`
  - Add an invariant that provider CRUD commands MUST use `TypedCRUD[T]` for data access, not raw API clients
  - Add an invariant that all `ResourceAdapter` implementations MUST provide `Schema()` and `Example()` (codifying PR #18 convention)
  - Update the "Typed resource trajectory" paragraph to reflect that TypedObject[T] and ResourceIdentity are implemented (not aspirational)

- **FR-016**: DESIGN.md MUST be updated to:
  - Update the Package Map table to include new types (`ResourceIdentity`, `TypedObject`, `TypedCRUD` typed methods) and the `SchemaFromType` helper in `internal/resources/adapter/`
  - Update the Provider System description to reflect `TypedRegistrations()` replacing `ResourceAdapters()`
  - Add or update the ADR table entry for ADR 004 (this work)
  - Include all provider packages currently missing from the Package Map (oncall, fleet, k6, kg, incidents)

## Acceptance Criteria

- GIVEN a provider domain type (e.g., `Slo`)
  WHEN `GetResourceName()` is called
  THEN it returns the type's identity field as a string (e.g., `slo.UUID`)

- GIVEN a provider domain type with a numeric ID (e.g., k6 `Project` with `ID int`)
  WHEN `SetResourceName("42")` is called
  THEN the type's ID field is set to 42

- GIVEN a provider domain type with a numeric ID
  WHEN `SetResourceName("not-a-number")` is called
  THEN the type silently ignores the parse error (matching current `RestoreNameFn` behavior)

- GIVEN a `TypedObject[Slo]` instance
  WHEN `GetName()` is called (via embedded `ObjectMeta`)
  THEN it returns the K8s metadata name

- GIVEN a `TypedCRUD[Slo]` with `ListFn` configured
  WHEN `List(ctx)` is called
  THEN it returns `[]TypedObject[Slo]` where each element has correct `TypeMeta`, `ObjectMeta.Name` matching `slo.GetResourceName()`, and `Spec` containing the domain object

- GIVEN a `TypedCRUD[Slo]` with `GetFn` configured
  WHEN `Get(ctx, "some-uuid")` is called
  THEN it returns `*TypedObject[Slo]` with `ObjectMeta.Name == "some-uuid"` and `Spec` populated from the GetFn result

- GIVEN a `TypedCRUD[T]` with nil `CreateFn`
  WHEN `Create(ctx, obj)` is called
  THEN it returns `errors.ErrUnsupported`

- GIVEN a `TypedCRUD[T]` instance
  WHEN `AsAdapter()` is called and the returned adapter's `List()` is invoked
  THEN the result is identical to the previous `typedAdapter[T]` behavior (K8s envelope with stripped spec fields)

- GIVEN a provider that constructs a `TypedRegistration[T]` with a `TypedCRUD[T]` factory
  WHEN `TypedRegistrations()` is called and `providers.Register(p)` processes the result
  THEN each `TypedRegistration[T].ToRegistration()` is converted to a standard `adapter.Registration` and passed to `adapter.Register()` automatically

- GIVEN the `Provider` interface
  WHEN a provider implements `TypedRegistrations()`
  THEN `providers.Register(p)` auto-registers all returned `adapter.Registration` entries in the adapter registry, and NO separate `adapter.Register()` calls exist outside `providers.Register()`

- GIVEN a provider that previously had two `init()` functions (e.g., alert with `provider.go` and `resource_adapter.go`)
  WHEN the migration is complete
  THEN the provider has exactly one `init()` function containing a single `providers.Register()` call

- GIVEN the total count of `init()` functions across all provider packages
  WHEN all 8 providers (slo, synth, oncall, fleet, k6, kg, incidents, alert) are migrated
  THEN the count is exactly 8 (one per provider)

- GIVEN ALL provider CRUD commands across ALL 8 providers
  WHEN executed after migration
  THEN each uses `TypedCRUD[T]` typed methods internally (not direct REST client calls) and produces identical output to the pre-migration command

- GIVEN the `resources get` command targeting a provider resource type
  WHEN executed after migration
  THEN output is byte-identical to pre-migration output (backward compatibility of AsAdapter bridge)

- GIVEN CONSTITUTION.md after updates
  WHEN inspected
  THEN it contains invariants for: ResourceIdentity on domain types, Provider.TypedRegistrations() pattern, TypedCRUD-based provider commands, Schema()/Example() on all adapters

- GIVEN DESIGN.md after updates
  WHEN inspected
  THEN the Package Map includes ResourceIdentity, TypedObject, SchemaFromType, and all 8 provider packages. The ADR table includes ADR 004.

## Negative Constraints

- NEVER add `NameFn` or `RestoreNameFn` fields to `TypedCRUD` -- these are replaced by `ResourceIdentity` methods on the domain type.
- NEVER change the `ResourceAdapter` interface (List, Get, Create, Update, Delete, Descriptor, Aliases, Schema, Example). The interface is frozen for this spec.
- NEVER introduce a circular import between `internal/providers/` and `internal/resources/adapter/`. The existing import direction (`providers` imports `adapter`) MUST be preserved.
- NEVER change the `Factory` type signature `func(ctx context.Context) (ResourceAdapter, error)`.
- NEVER change the lazy initialization behavior of adapter factories -- factories MUST NOT load config or make HTTP calls at `init()` time.
- NEVER make `Provider.TypedRegistrations()` call `adapter.Register()` directly -- `providers.Register()` is the single entry point that populates both registries.
- NEVER change the external CLI behavior of any provider command. Internal implementation changes MUST NOT alter command output, flags, exit codes, or error messages.
- DO NOT remove `MetadataFn` or `StripFields` from `TypedCRUD` -- these are retained and serve distinct purposes not covered by ResourceIdentity.
- DO NOT add ConfigKeys or Validate logic to `adapter.Registration` -- these are per-provider concerns, not per-resource-type.
- DO NOT introduce `ProviderMeta` type in this spec -- that is tracked as follow-up work in the research report.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Numeric ID parse errors silently swallowed by SetResourceName | Data corruption if an invalid name reaches a Create/Update path | This matches existing RestoreNameFn behavior. Add validation in TypedCRUD typed methods to reject empty GetResourceName() results before API calls. |
| Provider command output divergence after TypedCRUD migration | User-visible behavior change breaks scripts/CI | Require golden-file or snapshot tests comparing pre- and post-migration output for each provider's CRUD commands. |
| TypedCRUD constraint change breaks downstream compilation | All existing TypedCRUD instantiations must be updated simultaneously | Domain types implement ResourceIdentity before the constraint change. Atomic commit per provider with both changes. |
| Hybrid serialization (typed methods vs. AsAdapter bridge) creates maintenance burden | Two conversion paths that can drift | AsAdapter bridge is the backward-compat path and is mechanically tested. Typed methods are the forward path. Document the intent to eliminate the hybrid once StripFields is resolved. |
| checkResource is unexported in synth/checks | Cannot implement ResourceIdentity on unexported type from outside package | ResourceIdentity methods are added within the synth/checks package where checkResource is defined. No export needed. |
| OnCall has 17 resource types with generic newListSubcommand pattern | Migration touches many files in one provider | OnCall's generic pattern means a single TypedCRUD factory template can serve all 17 types. Migration is repetitive but structurally identical per type. |
| CONSTITUTION.md/DESIGN.md updates lag behind code changes | Implementation agents use stale patterns | Doc updates are explicit deliverables in this spec with their own acceptance criteria, not afterthoughts. |

## Open Questions

- [RESOLVED]: Should ResourceIdentity use `GetName()`/`SetName()` or different method names? -- Different names (`GetResourceName()`/`SetResourceName()`) to avoid collision with `metav1.ObjectMeta` methods.
- [RESOLVED]: Should `Provider.Validate()` be removed in this spec? -- No. Removing Validate() is deferred to the full ProviderMeta convergence. It is dead at runtime but removing it is a separate interface change.
- [RESOLVED]: Should SLO Reports get a ResourceAdapter? -- No. Reports is read-only analytics, not a CRUD resource. Out of scope.
- [DEFERRED]: Should `StripFields` be replaced by struct tags? -- Will address in follow-up work. Requires investigation of whether struct tags can express all current strip patterns.
- [DEFERRED]: Full registry convergence with `ProviderMeta` type, `RegisterProvider()` API, and removal of Validate()/ConfigKeys() from Provider interface -- tracked in research report phases 2-6.
- [DEFERRED]: Provider CRUD command deprecation in favor of `grafanactl resources` equivalents -- separate UX decision after migration proves the unified path works.

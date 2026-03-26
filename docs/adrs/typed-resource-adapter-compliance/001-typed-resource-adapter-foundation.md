# TypedResourceAdapter[T] with ResourceIdentity and Provider Command Migration

**Created**: 2026-03-25
**Status**: proposed
**Bead**: grafanactl-experiments-dvwd
**Supersedes**: none

## Context

Every provider in grafanactl has two parallel CRUD code paths:

1. **Provider CLI commands** (`grafanactl slo definitions list`) call REST clients
   directly, working with typed structs (`Slo`, `Check`, etc.).
2. **Resources pipeline** (`grafanactl resources get slos`) goes through
   `ResourceAdapter`, which erases type information via `*unstructured.Unstructured`.

The `TypedCRUD[T any]` generic (introduced in the TypedResourceAdapter Foundation
spec) absorbs boilerplate but immediately erases `T` through `AsAdapter()`. Provider
commands cannot use `TypedCRUD` for typed access because it exposes no typed public
methods — only function pointers and an unstructured bridge.

Additionally:

- `Provider.ResourceAdapters() []adapter.Factory` is **dead code** — no call sites
  exist. All adapter registration flows through `init()` → `adapter.Register()`.
- `NameFn`, `RestoreNameFn`, and `MetadataFn` function pointers on `TypedCRUD`
  replicate what K8s metadata accessors (`GetName()`, `SetName()`) provide natively.
- Two global registries coexist: `providers.Register()` for CLI identity and
  `adapter.Register()` for resource adapters, with **no connection between them**.
  Eight providers use 13 `init()` functions across three different patterns (split,
  combined, sub-package) to populate both registries independently.

The consolidation plan requires porting 40+ resource types from gcx. Each new
provider would perpetuate the dual code path and dual registration if the architecture
is not addressed.

**Prior work**: `docs/specs/feature-typed-resource-adapter-foundation/spec.md`
(TypedCRUD[T] generic), `docs/specs/feature-fold-provider-crud-into-resources/spec.md`
(ResourceAdapter interface and unified CRUD routing).

**Research**: `docs/research/2026-03-25-provider-registry-convergence.md` (analysis of
Provider interface, registry patterns, non-CRUD command inventory, migration path).

## Decision

We will introduce three new abstractions, unify the dual registration pattern, and
migrate all provider CRUD commands to use `TypedCRUD[T]` as their service layer.

### 1. ResourceIdentity interface

Domain types implement this to bridge their identity field to K8s `metadata.name`:

```go
type ResourceIdentity interface {
    GetResourceName() string
    SetResourceName(string)
}
```

This replaces `NameFn` and `RestoreNameFn` on `TypedCRUD`. The contract is: given a
K8s name, `SetResourceName` restores enough identity to make API calls work. The
mapping may be asymmetric (e.g., fleet's `Pipeline` generates a composite slug from
`Name + ID` but only restores `ID` from the slug).

All ~16 existing domain types add these two methods (~50 LOC total).

### 2. TypedObject[T] envelope

Standard K8s CRD pattern — wraps any `T ResourceIdentity` with K8s metadata:

```go
type TypedObject[T ResourceIdentity] struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata"`
    Spec T            `json:"spec"`
}
```

`TypedObject[Slo]` implements `metav1.Object` via embedded `ObjectMeta` — standard
K8s accessors (`GetName()`, `GetNamespace()`, `GetLabels()`, etc.) work natively.

### 3. TypedCRUD constraint change and typed public methods

`TypedCRUD[T any]` becomes `TypedCRUD[T ResourceIdentity]`. New typed methods:

```go
func (c *TypedCRUD[T]) List(ctx context.Context) ([]TypedObject[T], error)
func (c *TypedCRUD[T]) Get(ctx context.Context, name string) (*TypedObject[T], error)
func (c *TypedCRUD[T]) Create(ctx context.Context, obj *TypedObject[T]) (*TypedObject[T], error)
func (c *TypedCRUD[T]) Update(ctx context.Context, name string, obj *TypedObject[T]) (*TypedObject[T], error)
func (c *TypedCRUD[T]) Delete(ctx context.Context, name string) error
```

`AsAdapter()` continues to return `ResourceAdapter` for the unstructured pipeline.

**Removed from TypedCRUD**: `NameFn`, `RestoreNameFn` (replaced by `ResourceIdentity`).
**Kept**: `MetadataFn` (for extra metadata beyond what `ObjectMeta` covers),
`StripFields` (spec-level key removal before serialization).

### 4. Unified registration via Provider.Registrations()

Replace the dual `init()` registration pattern with a single path. The Provider
interface evolves:

```go
type Provider interface {
    Name() string
    ShortDesc() string
    Commands() []*cobra.Command
    Validate(cfg map[string]string) error
    ConfigKeys() []ConfigKey
    Registrations() []adapter.Registration  // NEW — provider owns its adapter registrations
    // ResourceAdapters() REMOVED — dead code, subsumed by Registrations()
}
```

`providers.Register()` auto-registers adapters:

```go
func Register(p Provider) {
    registry = append(registry, p)
    for _, r := range p.Registrations() {
        adapter.Register(r)
    }
}
```

Each provider moves its `adapter.Register()` calls from separate `init()` functions
into the `Registrations()` method. Registration construction logic is unchanged —
just relocated. This collapses 13 `init()` functions down to 8 (one per provider)
and makes Provider the single owner of its adapter registrations.

Example for SLO:

```go
// Before: two init() in two files
//   slo/provider.go:                  providers.Register(&SLOProvider{})
//   slo/definitions/resource_adapter.go: adapter.Register(Registration{...})

// After: one init(), one method
func (p *SLOProvider) Registrations() []adapter.Registration {
    return []adapter.Registration{definitions.StaticRegistration()}
}
// init() only has: providers.Register(&SLOProvider{})
// adapter.Register() call deleted from definitions/resource_adapter.go
```

No circular import risk — `internal/providers/provider.go` already imports
`internal/resources/adapter` for the existing `Factory` type.

### 5. Provider command migration

All provider CRUD commands migrate from direct REST client calls to `TypedCRUD[T]`.
Each provider package exposes a shared typed factory:

```go
func NewTypedCRUD(ctx context.Context) (*adapter.TypedCRUD[Slo], error)
```

Used by both CLI commands (typed access via `.Spec`) and adapter registration
(unstructured access via `AsAdapter()`). This is the single construction point that
eliminates the dual code path.

### 6. Spec-level serialization (hybrid approach)

`TypedObject[T]` provides K8s-compatible metadata, but `StripFields` operates at the
spec map level and requires JSON→map→delete. The approach is hybrid:

- **Typed methods** (`List`, `Get`, etc.): Build `TypedObject[T]` with proper metadata,
  use `runtime.DefaultUnstructuredConverter` for full conversion where possible.
- **AsAdapter bridge**: Keeps the existing JSON→map→strip→envelope approach for spec
  serialization to maintain backward compatibility with `StripFields`.

### Rejected alternatives

- **Interface constraint on T for full `metav1.Object`**: Too heavy — `metav1.Object`
  has ~30 methods. Domain types would need massive boilerplate or `ObjectMeta` embedding
  that changes their JSON serialization.
- **Convertible domain types (types carry own conversion)**: Clean but requires modifying
  all domain types with `ToUnstructured()`/`FromUnstructured()` methods — heavier than
  `ResourceIdentity`'s 2 methods.
- **Keep `NameFn`/`RestoreNameFn` as config**: Pragmatic but misses the opportunity to
  make domain types self-describing. `ResourceIdentity` is minimal (2 methods) and
  enables future K8s machinery use.
- **Keep dual registries, defer unification**: The research report
  (`docs/research/2026-03-25-provider-registry-convergence.md`) confirmed that
  `Provider.Registrations()` is a ~100-150 LOC change that collapses 13 init() into 8
  with zero new logic — just relocation. No reason to defer when the lift is this small.
- **Full registry convergence with ProviderMeta**: The research report proposes a
  `ProviderMeta` type and `RegisterProvider()` API that fully decouples provider
  metadata from the Provider interface. This is the right long-term target but is a
  larger refactor (moving ConfigKeys, Validate, rewriting `providers list`). The
  `Registrations()` approach gets 90% of the benefit with 10% of the effort.

## Consequences

### Positive

- **Single CRUD code path**: Provider commands and resources pipeline share `TypedCRUD[T]`
  as the service layer. Bug fixes apply to both paths.
- **Typed access for provider commands**: Commands get `TypedObject[T]` with `.Spec`
  typed access instead of hand-rolling REST client calls.
- **K8s metadata compatibility**: `TypedObject[T]` implements `metav1.Object` —
  domain objects can participate in K8s tooling that expects standard accessors.
- **Self-describing domain types**: `ResourceIdentity` eliminates per-adapter function
  pointer configuration for name mapping.
- **Unified registration**: Provider owns its adapter registrations via `Registrations()`.
  Single `init()` per provider, single `providers.Register()` call populates both
  registries atomically. Eliminates the dual init() pattern (13 → 8).
- **Dead code removal**: `Provider.ResourceAdapters()` and its 9 implementations
  (mostly `return nil`) are removed, subsumed by `Registrations()`.

### Negative

- **Hybrid serialization**: Two conversion paths (typed methods vs. AsAdapter bridge)
  coexist until `StripFields` is resolved at a higher level.
- **Provider command migration is breadth work**: ~8 providers, each with multiple CRUD
  commands. Mechanical but touches many files. Atomic commits per provider mitigate risk.
- **`SetResourceName` error swallowing**: K6 and synth types with int IDs silently
  discard parse errors, matching current `RestoreNameFn` behavior but baked into a
  formal interface.

### Follow-up work

- **Full registry convergence**: Introduce `ProviderMeta` type, move ConfigKeys/Validate
  out of Provider interface, enable `providers list` from a unified registry. See
  `docs/research/2026-03-25-provider-registry-convergence.md` phases 2-6.
- **StripFields elimination**: Investigate using struct tags or separate spec types to
  avoid the JSON→map→delete pattern entirely.
- **Provider command deprecation**: After migration, deprecate provider-specific CRUD
  commands in favor of `grafanactl resources` equivalents.

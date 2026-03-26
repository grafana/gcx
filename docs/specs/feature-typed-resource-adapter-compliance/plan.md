---
type: feature-plan
title: "Typed Resource Adapter Compliance"
status: draft
spec: docs/specs/feature-typed-resource-adapter-compliance/spec.md
created: 2026-03-26
---

# Architecture and Design Decisions

## Pipeline Architecture

The feature modifies the adapter and provider layers. After this work, the registration and CRUD flow changes from a dual-path system to a single unified path.

### Before (Current State)

```
Provider init()                    Resource Adapter init()
  │                                   │
  ▼                                   ▼
providers.Register(provider)       adapter.Register(registration)
  │                                   │
  ▼                                   ▼
providers.registry ([]Provider)    adapter.registrations ([]Registration)
                                      │
                                      ▼
Provider CLI commands              TypedCRUD[T any]
  │                                   │  NameFn(T) string        ← function pointer
  ▼                                   │  RestoreNameFn(name, *T) ← function pointer
REST Client (direct calls)            ▼
                                   typedAdapter[T] → ResourceAdapter
```

Two separate init() calls per provider (split providers have 2 init files).
Provider CLI commands bypass TypedCRUD entirely, calling REST clients directly.

### After (Target State)

```
Provider init()
  │
  ▼
providers.Register(provider)
  │
  ├─► providers.registry ([]Provider)
  │
  └─► provider.TypedRegistrations() → []Registration
        │
        ▼
      adapter.Register(registration)   ← called atomically by providers.Register()
        │
        ▼
      adapter.registrations ([]Registration)
        │
        ▼
      TypedCRUD[T ResourceIdentity]
        │  T.GetResourceName() string   ← interface method on domain type
        │  T.SetResourceName(string)    ← interface method on domain type
        │
        ├─► typedAdapter[T] → ResourceAdapter  (unstructured path)
        │
        └─► List/Get/Create/Update/Delete      (typed public methods)
              ▲
              │
      Provider CLI commands  ← now call TypedCRUD typed methods
```

Single init() per provider. Provider CLI commands use TypedCRUD typed methods.

### Full Provider Registration Flow

End-to-end flow showing how a provider defines domain types, registers adapters,
and serves both CLI commands and the resources pipeline.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ PROVIDER PACKAGE (e.g., internal/providers/slo/)                           │
│                                                                             │
│  1. Domain type implements ResourceIdentity                                 │
│     ┌──────────────────────────────┐                                        │
│     │ type Slo struct {            │                                        │
│     │     UUID string              │                                        │
│     │     Name string              │                                        │
│     │     ...                      │                                        │
│     │ }                            │                                        │
│     │ func (s Slo) GetResourceName() string    { return s.UUID }            │
│     │ func (s *Slo) SetResourceName(n string)  { s.UUID = n }              │
│     └──────────────────────────────┘                                        │
│                                                                             │
│  2. Shared TypedCRUD factory (used by both paths)                           │
│     ┌──────────────────────────────────────────────────────┐                │
│     │ func NewTypedCRUD(ctx) (*adapter.TypedCRUD[Slo], error) {            │
│     │     client := slo.NewClient(cfg)                                      │
│     │     return &adapter.TypedCRUD[Slo]{                                   │
│     │         Descriptor: sloDescriptor,                                    │
│     │         ListFn:     client.List,                                      │
│     │         GetFn:      client.Get,                                       │
│     │         CreateFn:   client.Create,                                    │
│     │         ...                                                           │
│     │     }, nil                                                            │
│     │ }                                                                     │
│     └──────────────────────────────────────────────────────┘                │
│                                                                             │
│  3. Provider implements TypedRegistrations()                                │
│     ┌──────────────────────────────────────────────────────┐                │
│     │ func (p *SLOProvider) TypedRegistrations()            │                │
│     │     []adapter.Registration {                          │                │
│     │     return []adapter.Registration{                    │                │
│     │         adapter.TypedRegistration[Slo]{               │                │
│     │             Descriptor: sloDescriptor,                │                │
│     │             GVK:        sloGVK,                       │                │
│     │             Aliases:    sloAliases,                   │                │
│     │             Factory:    NewTypedCRUD,  ◄─── same factory               │
│     │         }.ToRegistration(),                           │                │
│     │     }                                                 │                │
│     │ }                                                     │                │
│     └──────────────────────────────────────────────────────┘                │
│                                                                             │
│  4. Single init() per provider                                              │
│     ┌────────────────────────────────────┐                                  │
│     │ func init() {                      │                                  │
│     │     providers.Register(&SLOProvider{})                                │
│     │ }                                  │                                  │
│     └──────────────┬─────────────────────┘                                  │
└─────────────────────┼───────────────────────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ providers.Register(p)  (internal/providers/registry.go)                     │
│                                                                             │
│  Step A: registry = append(registry, p)     ← provider identity registered  │
│                                                                             │
│  Step B: for _, r := range p.TypedRegistrations() {                         │
│              adapter.Register(r)            ← adapter registration          │
│          }                                                                  │
│                                                                             │
│  Result: BOTH registries populated atomically from single init() call       │
└────────────────┬───────────────────────────┬────────────────────────────────┘
                 │                           │
        ┌────────┘                           └────────┐
        ▼                                             ▼
┌───────────────────────┐                 ┌───────────────────────────────────┐
│ providers.registry    │                 │ adapter.registrations             │
│ ([]Provider)          │                 │ ([]Registration)                  │
│                       │                 │                                   │
│ Used by:              │                 │ Used by:                          │
│ • providers list cmd  │                 │ • ResourceClientRouter            │
│ • config view         │                 │ • discovery.RegistryIndex         │
│ • provider CLI cmds   │                 │ • resources get/push/pull/delete  │
└───────────────────────┘                 └─────────────┬─────────────────────┘
                                                        │
                                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ RUNTIME: Two access paths, one TypedCRUD instance                           │
│                                                                             │
│  Path A: Provider CLI command (typed)                                       │
│  ┌─────────────────────────────────────────────────────┐                    │
│  │ // slo definitions list                             │                    │
│  │ crud, _ := NewTypedCRUD(ctx)                        │                    │
│  │ objects, _ := crud.List(ctx)                        │                    │
│  │ // objects is []TypedObject[Slo]                    │                    │
│  │ // objects[i].Spec is Slo (fully typed)             │                    │
│  │ // objects[i].GetName() is K8s metadata name        │                    │
│  └─────────────────────────────────────────────────────┘                    │
│                                                                             │
│  Path B: Resources pipeline (unstructured)                                  │
│  ┌─────────────────────────────────────────────────────┐                    │
│  │ // resources get slos.v1alpha1.slo.ext.grafana.app  │                    │
│  │ router.getAdapter(gvk)   ← lazy init via sync.Once  │                    │
│  │   → factory(ctx)         ← calls same NewTypedCRUD   │                    │
│  │   → crud.AsAdapter()     ← returns typedAdapter[Slo] │                    │
│  │   → adapter.List()       ← returns []Unstructured    │                    │
│  └─────────────────────────────────────────────────────┘                    │
│                                                                             │
│  Both paths share the same TypedCRUD[Slo] factory and REST client.          │
│  Bug fixes to ListFn/GetFn/etc. apply to both paths automatically.          │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| `ResourceIdentity` uses `GetResourceName()`/`SetResourceName()` not `GetName()`/`SetName()` | Avoids collision with `ObjectMeta.GetName()` if a domain type ever embeds ObjectMeta. Explicit naming makes the purpose clear. (FR-001) |
| `TypedObject[T]` uses embedded `metav1.ObjectMeta` | Standard K8s CRD pattern. Enables domain types to participate in K8s-aware generic code without custom metadata wiring. (FR-003) |
| Keep `MetadataFn` and `StripFields` on `TypedCRUD` | These serve different purposes than identity. `MetadataFn` adds labels/annotations; `StripFields` removes server-managed spec fields. Both are adapter-specific concerns, not domain-type concerns. Removing them is a separate refactor (out of scope). (FR-007) |
| `TypedRegistrations()` on `Provider` interface, not a full `ProviderMeta` struct | Minimal interface change. `ProviderMeta` is a larger convergence effort marked out of scope. Adding one method is backward-compatible with a default nil return. (FR-009) |
| `SetResourceName` swallows parse errors for numeric IDs | K6 and synth providers use `strconv.Atoi`/`ParseInt` with `_` for errors today. Changing this would alter behavior for existing round-trip flows. Matches existing `RestoreNameFn` semantics. (FR-006) |
| OnCall keeps its `registerOnCallResource` helper but replaces `nameFn` with `ResourceIdentity` constraint | OnCall has 17 types all using the same `ID string` pattern. The helper reduces duplication. The constraint change propagates naturally — `nameFn` parameter is removed, `T.GetResourceName()` is used instead. (FR-004, FR-013) |
| Provider CRUD commands share a typed factory per provider | Each provider creates a `TypedCRUD[T]` factory once. CLI commands and adapter registration both use the same factory, eliminating dual code paths. (FR-013) |
| Hybrid spec-level serialization in `TypedObject[T]` | `TypedObject.Spec` holds the typed `T`. JSON serialization produces the standard `{apiVersion, kind, metadata, spec}` envelope. `toUnstructured`/`fromUnstructured` continue to work via JSON round-trip but now use `T.GetResourceName()` instead of `NameFn`. (FR-014) |
| Doc updates are deliverables in the final wave | CONSTITUTION.md and DESIGN.md updates capture invariants established by this feature. They MUST be done as part of this feature, not deferred. (FR-015, FR-016) |

## Compatibility

**Continues working unchanged:**
- All external CLI behavior (command names, flags, output formats, exit codes)
- `ResourceAdapter` interface (no method additions or removals)
- `adapter.Factory` type signature (`func(ctx context.Context) (ResourceAdapter, error)`)
- `adapter.Register()` function (still callable, still accepts `Registration`)
- `adapter.AllTypedRegistrations()` function
- Lazy initialization behavior (adapters created on first use, not at startup)
- All existing tests (unit and integration)

**Deprecated (removed):**
- `Provider.ResourceAdapters()` method (replaced by `Provider.TypedRegistrations()`)
- `NameFn` and `RestoreNameFn` fields on `TypedCRUD`
- Separate `init()` functions for adapter registration (merged into provider `init()`)
- Direct REST client calls in provider CLI commands (replaced by `TypedCRUD` typed methods)

**Newly available:**
- `ResourceIdentity` interface for domain types
- `TypedObject[T ResourceIdentity]` envelope
- Typed public methods on `TypedCRUD`: `List()`, `Get()`, `Create()`, `Update()`, `Delete()`
- `Provider.TypedRegistrations()` method returning `[]adapter.Registration`
- Atomic provider + adapter registration via `providers.Register()`

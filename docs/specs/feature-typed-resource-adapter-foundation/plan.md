---
type: feature-plan
title: "TypedResourceAdapter[T] Foundation"
status: draft
spec: spec/feature-typed-resource-adapter-foundation/spec.md
created: 2026-03-20
---

# Architecture and Design Decisions

## Pipeline Architecture

The `TypedCRUD[T]` generic sits between the provider's REST client and the existing `ResourceAdapter` interface. It absorbs the marshal/strip/envelope boilerplate that every provider currently duplicates.

```
Provider REST Client (unchanged)
    │
    │  ListFn / GetFn / CreateFn / UpdateFn / DeleteFn
    │  (closures capture client + extra deps)
    ▼
┌──────────────────────────────────────────────────┐
│  TypedCRUD[T]  (new, internal/resources/adapter) │
│                                                  │
│  Config:                                         │
│    NameFn(T) string          → metadata.name     │
│    StripFields []string      → delete from spec  │
│    RestoreNameFn(name, *T)   → round-trip ident  │
│    MetadataFn(T) map[s]any   → extra metadata    │
│    Namespace string          → metadata.namespace│
│                                                  │
│  Internal pipeline (auto-generated):             │
│    T → json.Marshal → map[s]any → strip fields   │
│    → build K8s envelope → MustFromObject         │
│    → *unstructured.Unstructured                  │
│                                                  │
│  AsAdapter(desc, aliases) → ResourceAdapter      │
└──────────────────────────────────────────────────┘
    │
    │  implements ResourceAdapter interface (unchanged)
    ▼
┌──────────────────────────────────────────────────┐
│  adapter.Registration / adapter.Register()       │
│  (unchanged — TypedRegistration[T].ToRegistration│
│   bridges into the existing system)              │
└──────────────────────────────────────────────────┘
    │
    ▼
ResourceClientRouter → Discovery Registry → CLI commands
```

**Registration flow** (before vs after):

```
BEFORE (per provider, ~30 LOC each):
  init() → build static descriptor/aliases globals
         → NewAdapterFactory(loader) returns Factory
         → adapter.Register(Registration{Factory, Descriptor, Aliases, GVK})

AFTER (per provider, ~5 LOC each):
  init() → TypedRegistration[T]{Descriptor, Aliases, GVK, Factory}.ToRegistration()
         → adapter.Register(registration)  // same call
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Place `typed.go` + `typed_test.go` in `internal/resources/adapter/` | The generic implements `ResourceAdapter` (defined in same package), avoiding circular imports. All existing adapters already import this package. (FR-001, FR-004) |
| `TypedCRUD[T]` holds function pointers, not an interface | Function pointers + closures are more flexible than requiring providers to implement a typed interface. Checks adapter needs probe resolution in closures; SLO needs re-fetch after create. Each closure captures its own dependencies. (FR-001, Key Decision: closures) |
| `AsAdapter()` returns a thin wrapper struct (not TypedCRUD itself) | The wrapper struct holds `desc` and `aliases` in addition to `*TypedCRUD[T]`. This keeps TypedCRUD focused on CRUD logic while the wrapper satisfies `Descriptor()` and `Aliases()`. (FR-004, FR-013, FR-014) |
| `TypedRegistration[T].ToRegistration()` wraps the typed factory in a standard `Factory` | The typed factory returns `*TypedCRUD[T]`, but `adapter.Factory` returns `ResourceAdapter`. `ToRegistration()` bridges by calling `AsAdapter()` on the result. (FR-003) |
| Checks uses `CheckSpec` as T with complex ListFn closure | The ListFn closure calls `client.List()` (returns `[]Check`), fetches probe names, converts each `Check` to `CheckSpec` with resolved probe names, and returns `[]CheckSpec`. This keeps the generic simple while handling the most complex adapter. (Key Decision: checks) |
| MetadataFn merge happens after name/namespace are set | The envelope construction sets `metadata.name` and `metadata.namespace` first, then merges MetadataFn output. The merge explicitly skips `"name"` and `"namespace"` keys to enforce the negative constraint. (FR-022) |
| Provider `adapter.go` files (ToResource/FromResource) are removed or reduced | The generic absorbs marshal/strip/envelope logic. Provider-specific helpers like `slugifyJob`, `extractIDFromSlug`, `FileNamer`, `SpecToCheck` remain since they are used by CLI commands or the closures themselves. (FR-019) |
| Refactor checks first as proof-of-concept, then batch remaining four | Checks is the most complex adapter (probe resolution, tenant ID, custom metadata.uid). If the generic handles checks cleanly, the simpler adapters will work. (Risk mitigation) |

## Compatibility

- **Unchanged**: `ResourceAdapter` interface, `Registration` struct, `Register()`, `AllRegistrations()`, `RegisterAll()`, `Factory` type, all provider REST clients, all provider type definitions, CLI commands, `FileNamer` helpers, `slugifyJob`/`extractIDFromSlug` helpers.
- **Reduced**: Per-provider `adapter.go` files (`ToResource`/`FromResource`) -- logic moves into TypedCRUD closures and configuration. Functions still used outside the adapter (e.g., by tests or commands) remain.
- **Reduced**: Per-provider `resource_adapter.go` files -- the hand-written `ResourceAdapter` struct, `Descriptor()`, `Aliases()`, `List`, `Get`, `Create`, `Update`, `Delete` methods are replaced by `TypedCRUD[T]` + `AsAdapter()`.
- **New**: `typed.go` in `internal/resources/adapter/` -- the `TypedCRUD[T]` and `TypedRegistration[T]` generics.
- **New**: `typed_test.go` -- unit tests for the generic with a mock `TestWidget` struct.

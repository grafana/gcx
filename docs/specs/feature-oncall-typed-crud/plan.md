---
type: feature-plan
title: "Table-driven TypedCRUD[T] for OnCall Adapter"
status: approved
spec: spec/feature-oncall-typed-crud/spec.md
created: 2026-03-24
---

# Architecture and Design Decisions

## Pipeline Architecture

Current state (switch dispatch):

```
init()
  └─ RegisterAdapters(loader)
       └─ for _, rd := range allResources() {      ← 17 resourceDefs
            adapter.Register(Registration{
              Factory: newSubResourceFactory(loader, rd)
            })
          }
              └─ Factory returns &subResourceAdapter{...}
                   ├─ listRaw()   → switch 17 cases → toAnySlice → []any
                   ├─ getRaw()    → switch 15 cases → any
                   ├─ createRaw() → switch 7 cases  → fromResource[T] → any
                   ├─ updateRaw() → switch 7 cases  → fromResource[T] → any
                   └─ deleteRaw() → switch 8 cases  → error
```

Target state (table-driven TypedCRUD):

```
init()
  ├─ providers.Register(&OnCallProvider{})
  └─ registerAllAdapters(&configLoader{})
       ├─ registerOnCallResource[Integration](loader, meta, nameFn, listFn, getFn,
       │      withCreate(...), withUpdate(...), withDelete(...))
       ├─ registerOnCallResource[EscalationChain](...)
       ├─ registerOnCallResource[Shift](...)              ← custom closures for ShiftRequest
       ├─ registerOnCallResource[ResolutionNote](...)     ← custom closures for Input types
       ├─ registerOnCallResource[ShiftSwap](...)          ← custom closures for Input types
       ├─ registerOnCallResource[UserGroup](loader, meta, nameFn, listFn, nil)  ← nil GetFn
       ├─ registerOnCallResource[SlackChannel](loader, meta, nameFn, listFn, nil)
       └─ ... (17 total)
            └─ adapter.Register(Registration{
                 Factory: func(ctx) {
                   client, ns, _ := loader.LoadOnCallClient(ctx)
                   crud := &TypedCRUD[T]{NameFn, ListFn, GetFn, ...}
                   return crud.AsAdapter()
                 },
                 Descriptor, Aliases, Schema, Example
               })
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Single `registerOnCallResource[T]` generic function in `resource_adapter.go` | Eliminates the `subResourceAdapter` struct and all 5 switch blocks. Each of the 17 registrations is self-contained (~5-15 LOC). Traces to FR-003. |
| Functional options pattern: `crudOption[T]` as `func(*TypedCRUD[T])` | Natural Go idiom for optional configuration. 10/17 types support create, 10 update, 11 delete — options express the CRUD matrix declaratively. Traces to FR-004. |
| `withCreate[T]`, `withUpdate[T]`, `withDelete[T]` helper constructors | Each returns a `crudOption[T]` that sets the corresponding `Fn` field. Special-case types (Shift, ResolutionNote, ShiftSwap) pass custom closures that do the type conversion. Traces to FR-004, FR-007, FR-008, FR-009. |
| `resourceMeta` struct replacing `resourceDef` | Carries `Descriptor`, `Aliases`, `Schema`, `Example` — drops `idField` and `kind/singular/plural` fields since those live in the Descriptor. Lighter and matches what `adapter.Register` needs directly. Traces to Key Decision "resourceMeta replaces resourceDef". |
| `nil` GetFn for UserGroup/SlackChannel | TypedCRUD's `typedAdapter.Get` calls GetFn directly. For these two types, pass a closure that returns `errors.ErrUnsupported`. Traces to FR-014. |
| NameFn uses `item.ID` directly | All 17 OnCall types have an `ID string` field tagged `json:"id"`. Each registration passes a concrete closure `func(t T) string { return t.ID }`. Traces to FR-018. |
| `registerAllAdapters` replaces `RegisterAdapters` | Same signature (`loader OnCallConfigLoader`), called from `init()`. Internal to the package. |
| Schema and Example passed via `resourceMeta` | Only Integration has schema/example today. The meta struct carries them as `json.RawMessage` (zero value = nil for others). Traces to FR-017. |
| Single-file rewrite of `resource_adapter.go` | All registration code, the generic helper, and the functional options live in one file. `adapter.go` loses `fromResource[T]` only. Traces to FR-016. |

## Compatibility

**Unchanged:**
- All 17 resource types remain discoverable with identical GVK, singular/plural names, and aliases
- `OnCallProvider` struct, its `Commands()`, `ResourceAdapters()`, `Validate()`, `ConfigKeys()` — untouched
- `client.go`, `types.go` — untouched (read-only)
- `resource_adapter_test.go` — passes without modification
- All provider CLI commands (`list`, `get`, `alert-groups`, `schedules`, `users`, `escalate`) — untouched
- Integration schema and example JSON — identical content

**Newly available:**
- Create and Update for ResolutionNote (previously returned "not supported")
- Create and Update for ShiftSwap (previously returned "not supported")
- Create, Update, and Delete for PersonalNotificationRule (previously returned "not supported")
- Delete for ResolutionNote and ShiftSwap (previously returned "not supported")

**Removed (dead code):**
- `subResourceAdapter` struct and all methods
- `toAnySlice[T]` generic function
- `fromResource[T]` generic function (in `adapter.go`)
- `itemToResource` method
- `resourceDef` struct
- `allResources()` function
- `newSubResourceFactory` function

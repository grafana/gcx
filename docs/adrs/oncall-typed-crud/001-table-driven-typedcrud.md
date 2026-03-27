# ADR-001: Table-driven TypedCRUD[T] for OnCall Adapter

**Created**: 2026-03-24
**Status**: proposed
**Bead**: gcx-experiments-ghh
**Supersedes**: none

## Context

The OnCall provider was ported from the cloud CLI (PR #44) using a single
`subResourceAdapter` struct that dispatches all 17 resource types through 5
switch blocks (`listRaw`, `getRaw`, `createRaw`, `updateRaw`, `deleteRaw`) —
totalling 569 LOC in `internal/providers/oncall/resource_adapter.go`.

Meanwhile, the `TypedCRUD[T]` generic was implemented (commit b2c42e5) and
applied to incidents, fleet, and knowledge-graph adapters. OnCall is the last
provider using the old switch-dispatch pattern.

The current implementation has these problems:
- **Type erasure**: Uses `any` + `toAnySlice` helper, losing compile-time safety
- **Switch bloat**: Every CRUD operation is a 17-case switch on `a.def.kind`
- **Inconsistency**: All other providers use `TypedCRUD[T]`

Constraints:
- 17 distinct Go types sharing one `*Client`
- Only 7 support create, 7 update, 8 delete (rest are read-only)
- `Shift` uses `ShiftRequest` (not `Shift`) for create/update API calls
- `ResolutionNote` uses `Create/UpdateResolutionNoteInput` for write ops
- `ShiftSwap` uses `Create/UpdateShiftSwapInput` for write ops
- Existing tests (`resource_adapter_test.go`) must pass unchanged

## Decision

We will use a **table-driven registration helper** pattern: a generic
`registerOnCallResource[T]` function called 17 times in `init()`, with
functional options for optional write operations.

### 1. Registration helper

```go
type crudOption[T any] func(client *Client, crud *adapter.TypedCRUD[T])

func withCreate[T any](
    fn func(*Client, context.Context, *T) (*T, error),
) crudOption[T] { ... }

func withUpdate[T any](
    fn func(*Client, context.Context, string, *T) (*T, error),
) crudOption[T] { ... }

func withDelete[T any](
    fn func(*Client, context.Context, string) error,
) crudOption[T] { ... }

func registerOnCallResource[T any](
    loader OnCallConfigLoader,
    meta   resourceMeta,
    nameFn func(T) string,
    listFn func(*Client, context.Context) ([]T, error),
    getFn  func(*Client, context.Context, string) (*T, error),
    opts   ...crudOption[T],
) {
    adapter.Register(adapter.Registration{
        Factory: func(ctx context.Context) (adapter.ResourceAdapter, error) {
            client, ns, err := loader.LoadOnCallClient(ctx)
            if err != nil {
                return nil, err
            }
            crud := &adapter.TypedCRUD[T]{
                NameFn: nameFn,
                ListFn: func(ctx context.Context) ([]T, error) {
                    return listFn(client, ctx)
                },
                GetFn: func(ctx context.Context, name string) (*T, error) {
                    return getFn(client, ctx, name)
                },
                StripFields: []string{"id"},
                Namespace:   ns,
                Descriptor:  meta.Descriptor,
                Aliases:     meta.Aliases,
            }
            for _, opt := range opts {
                opt(client, crud)
            }
            return crud.AsAdapter(), nil
        },
        Descriptor: meta.Descriptor,
        Aliases:    meta.Aliases,
        GVK:        meta.Descriptor.GroupVersionKind(),
        Schema:     meta.Schema,
        Example:    meta.Example,
    })
}
```

### 2. Registration calls (~10 LOC each)

```go
func init() {
    loader := &providers.ConfigLoader{}

    // Full CRUD resource
    registerOnCallResource[Integration](
        loader, integrationMeta,
        func(i Integration) string { return i.ID },
        (*Client).ListIntegrations,
        (*Client).GetIntegration,
        withCreate((*Client).CreateIntegration),
        withUpdate((*Client).UpdateIntegration),
        withDelete((*Client).DeleteIntegration),
    )

    // Read-only + delete resource
    registerOnCallResource[AlertGroup](
        loader, alertGroupMeta,
        func(ag AlertGroup) string { return ag.ID },
        (*Client).ListAlertGroups,
        (*Client).GetAlertGroup,
        withDelete((*Client).DeleteAlertGroup),
    )

    // ... 15 more registrations
}
```

### 3. Special cases

**Shift** — API takes `ShiftRequest`, not `Shift`, for create/update:
```go
registerOnCallResource[Shift](
    loader, shiftMeta,
    func(s Shift) string { return s.ID },
    (*Client).ListShifts,
    (*Client).GetShift,
    func(client *Client, crud *adapter.TypedCRUD[Shift]) {
        crud.CreateFn = func(ctx context.Context, s *Shift) (*Shift, error) {
            sr := shiftToRequest(s)
            return client.CreateShift(ctx, sr)
        }
        crud.UpdateFn = func(ctx context.Context, name string, s *Shift) (*Shift, error) {
            sr := shiftToRequest(s)
            return client.UpdateShift(ctx, name, sr)
        }
    },
    withDelete((*Client).DeleteShift),
)
```

**ResolutionNote** and **ShiftSwap** follow the same pattern — custom closures
that convert between the read type and write-specific input types.

### 4. What gets deleted

- `subResourceAdapter` struct and all 5 switch methods (~350 LOC)
- `toAnySlice` helper
- `fromResource[T]` in `adapter.go` (replaced by `TypedCRUD.fromUnstructured`)
- `itemToResource` method (replaced by `TypedCRUD.toUnstructured`)
- `resourceDef` struct and `allResources()` (replaced by `resourceMeta` vars)

## Rejected Alternatives

### A: One `TypedCRUD[T]` per resource (17 explicit factories)

17 separate `newXAdapter()` functions, each ~25 LOC. Maximally clear and
consistent with fleet/incidents pattern, but produces ~425 LOC of
near-identical adapter code. The repetition is mechanical — the helper
captures it without losing clarity.

### C: `TypedRegistration[T]` bridge

Uses existing `TypedRegistration[T].ToRegistration()`. Adds indirection
without reducing verbosity — the factory closure still constructs a full
`TypedCRUD`. No advantage over A or B for this use case.

## Consequences

### Positive
- Eliminates `subResourceAdapter`, all 5 switch blocks, `toAnySlice`,
  `fromResource`, and `itemToResource` (~400 LOC removed)
- Compile-time type safety via Go generics
- Consistent with all other provider adapters in the codebase
- Each resource registration is self-documenting (~10 LOC)

### Negative
- Introduces `registerOnCallResource[T]` + `crudOption[T]` — an abstraction
  unique to OnCall (other providers have ≤4 resources and don't need it)
- Method value syntax (`(*Client).ListIntegrations`) may be unfamiliar to
  some Go developers

### Follow-up
- **Verb metadata**: Consider adding `Verbs []string` to `Registration` so
  provider adapters can declare supported operations statically (matching k8s
  `APIResource.Verbs` convention). Currently, unsupported ops return
  `errors.ErrUnsupported` at runtime. The `registerOnCallResource` helper
  already knows which ops are present based on which options were passed —
  emitting `Verbs` would be trivial. This is a separate enhancement, not
  blocked on this refactor.
- If other providers grow to >4 resources, the helper pattern can be
  extracted to a shared utility in `internal/resources/adapter/`.

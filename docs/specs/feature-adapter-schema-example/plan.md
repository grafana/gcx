---
type: feature-plan
title: "Add Schema() and Example() methods to ResourceAdapter interface"
status: approved
spec: docs/specs/feature-adapter-schema-example/spec.md
created: 2026-03-25
---

# Architecture and Design Decisions

## Pipeline Architecture

Current state: schema/example accessible only via global lookup.

```
Provider init()
    │
    ▼
TypedRegistration[T].ToRegistration()
    │
    ▼
Registration{Schema, Example}  ──►  global []registrations
    │                                        │
    ▼                                        ▼
adapter.Register(reg)             SchemaForGVK(gvk) / ExampleForGVK(gvk)
                                         │
                                         ▼
                                  schemas.go / examples.go commands
```

Target state: schema/example accessible both via global lookup AND via adapter instance.

```
Provider init()
    │
    ▼
TypedRegistration[T].ToRegistration()
    │
    ├──► Registration{Schema, Example}  ──►  global []registrations
    │                                                │
    │                                                ▼
    │                                      SchemaForGVK() / ExampleForGVK()
    │                                      (unchanged, backward compatible)
    │
    └──► Factory closure captures Schema/Example
              │
              ▼
         typedAdapter[T]{schema, example}  ← set by factory, NOT on TypedCRUD
              │
              ▼
         typedAdapter[T].Schema() / .Example()
              │
              ▼
         ResourceAdapter interface  ◄── used by commands, future CRUD gen
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Add `Schema() json.RawMessage` and `Example() json.RawMessage` to `ResourceAdapter` interface directly (no optional interface pattern) | Keeps the interface cohesive; blast radius is exactly 2 test mock structs. Avoids type-assertion ceremony at every call site. Traces to FR-001, FR-002. |
| Store `schema`/`example` as fields on `typedAdapter[T]`, NOT on `TypedCRUD[T]` | Schema/example are static registration metadata; `TypedCRUD[T]` is runtime CRUD behavior. Keeping them separate avoids entangling schema wiring in the in-flight `TypedCRUD[T ResourceIdentity]` refactor. Traces to FR-003, FR-004. |
| `ToRegistration()` factory closure sets schema/example on `typedAdapter[T]` after wrapping `TypedCRUD` | Single propagation point; no provider code changes required since providers already set these fields on `TypedRegistration[T]`. Traces to FR-004. |
| Round-trip test in `typed_test.go` anchors the schema/example wiring | Proves schema/example survive `TypedRegistration → ToRegistration → Factory → adapter.Schema()`. This test will serve as the anchor when the `TypedCRUD` type constraint changes. Traces to FR-005. |
| Global `SchemaForGVK`/`ExampleForGVK` remain unchanged | Backward compatibility for any callers outside the adapter package. Traces to FR-010. |
| Mock stubs return `nil` | Matches the zero-value behavior; tests that do not set schema/example get nil, consistent with adapters whose providers lack schema data. Traces to FR-006, FR-007. |

## Compatibility

**Continues working unchanged:**
- All provider registration code (`TypedRegistration[T]` already has `Schema`/`Example` fields)
- Global `SchemaForGVK()` and `ExampleForGVK()` functions and their call sites
- All existing CLI commands
- All provider packages (zero code changes required)

**Newly available:**
- `ResourceAdapter.Schema()` and `ResourceAdapter.Example()` methods on any adapter instance
- Commands that hold an adapter instance can call these methods directly instead of global lookup

**Deprecated:** Nothing.

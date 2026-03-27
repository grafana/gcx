---
type: feature-plan
title: "Fold Provider CRUD into Resources Subcommand"
status: draft
spec: spec/feature-fold-provider-crud-into-resources/spec.md
created: 2026-03-10
---

# Architecture and Design Decisions

## Pipeline Architecture

```
                          ┌─────────────────────────────────────────────────┐
                          │              CLI Layer (cmd/resources/)         │
                          │  get / list / push / pull / delete / edit       │
                          └─────────────────────┬───────────────────────────┘
                                                │
                                    ParseSelectors(args)
                                                │
                                                ▼
                          ┌─────────────────────────────────────────────────┐
                          │          Discovery Registry (hybrid)            │
                          │                                                 │
                          │  ┌──────────────────┐  ┌─────────────────────┐ │
                          │  │ Dynamic discovery │  │ Static provider     │ │
                          │  │ (K8s /apis)       │  │ descriptors         │ │
                          │  └────────┬─────────┘  └────────┬────────────┘ │
                          │           └─────────┬───────────┘              │
                          │                     ▼                          │
                          │         Unified RegistryIndex                   │
                          │   (LookupPartialGVK, aliases, etc.)            │
                          └─────────────────────┬───────────────────────────┘
                                                │
                                   MakeFilters() → Filters
                                                │
                                                ▼
                          ┌─────────────────────────────────────────────────┐
                          │           Resource Client Router                │
                          │                                                 │
                          │  IF filter.Descriptor is provider-backed:       │
                          │    → ResourceAdapter (lazy-init)                │
                          │  ELSE:                                          │
                          │    → dynamic.NamespacedClient (K8s)             │
                          │                                                 │
                          └──────────┬──────────────────────┬───────────────┘
                                     │                      │
                          ┌──────────▼──────────┐ ┌────────▼────────────────┐
                          │  ResourceAdapter     │ │ dynamic.NamespacedClient│
                          │  (per provider)      │ │ (existing K8s path)     │
                          │                      │ │                         │
                          │  SLOAdapter          │ │ Create/Update/Get/List  │
                          │  ChecksAdapter       │ │ Delete                  │
                          │  ProbesAdapter       │ │                         │
                          │  RulesAdapter        │ │                         │
                          │  GroupsAdapter       │ │                         │
                          │                      │ │                         │
                          │  wraps existing       │ │                         │
                          │  REST clients         │ │                         │
                          └──────────────────────┘ └────────────────────────┘
```

### Key Integration Points

1. **RegistryIndex** gains a `RegisterStatic()` method that accepts provider descriptors with short aliases. These entries merge into the existing `kindNames`, `singularNames`, `pluralNames`, `shortGroups`, `preferredVersions`, and `descriptors` maps. No change to `LookupPartialGVK` or `MakeFilters` logic is needed -- the index already handles lookup by kind, singular, plural, and group.

2. **Pusher, Puller, Deleter** gain awareness of provider-backed descriptors via a `ResourceClientRouter` that implements `PushClient`, `PullClient`, and `DeleteClient`. The router checks whether a descriptor belongs to a provider; if so, it delegates to the corresponding `ResourceAdapter`. Otherwise, it delegates to the existing `dynamic` client.

3. **ResourceAdapter** is a new interface in `internal/resources/adapter/` that matches the existing `PushClient`/`PullClient`/`DeleteClient` signatures but also carries its own descriptor for identification. Each provider implements this interface by wrapping its existing REST client and adapter functions (`ToResource`/`FromResource`).

6. **Provider interface extension**: The `Provider` interface gains a `ResourceAdapters() []adapter.Factory` method. Providers that support resource adapters return their factories; others return `nil`. The root registration loop in `cmd/gcx/root/command.go` (which already iterates all registered providers) calls `Registry.RegisterAdapter()` for each factory. This keeps provider self-description centralized — a provider declares both its commands tree AND its resource adapters.

4. **Lazy initialization**: The `ResourceClientRouter` holds adapter factories (closures), not adapter instances. Adapters are constructed on first use, ensuring provider config is only loaded when a provider resource type is actually selected.

5. **Deprecation warnings**: Provider top-level commands (`slo`, `synth`, `alert`) gain a `PersistentPreRun` hook that prints a stderr warning. The warning is suppressed in agent mode and when `--json` is active.

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| New `ResourceAdapter` interface mirrors existing `PushClient + PullClient + DeleteClient` combined (FR-001) | Avoids creating a separate abstraction; adapters can be directly used as the delegation target from the router. The combined interface is small (5 methods) and matches the CRUD verbs. |
| `ResourceClientRouter` wraps dynamic client + adapters behind existing client interfaces (FR-005 through FR-009) | Pusher, Puller, and Deleter already accept interfaces (`PushClient`, `PullClient`, `DeleteClient`). The router implements all three, keeping the existing pipeline code unchanged. |
| `RegistryIndex.RegisterStatic()` instead of modifying `Update()` (FR-003) | Static descriptors are known at compile time and do not come from the `/apis` endpoint. A separate registration path avoids mixing server-discovered and hard-coded resources in the same code path. |
| Short aliases registered via the same `kindNames`/`singularNames`/`pluralNames` maps (FR-004) | The existing `LookupPartialGVK` already searches these maps. Adding provider entries makes provider types resolvable without any parser changes. |
| Adapter factory pattern with lazy init (FR-016) | Synth requires `LoadSMConfig` which can fail if SM is not configured. Eager init would break `gcx resources get dashboards` for users without SM config. |
| Adapter encapsulates provider-specific auth (FR-015) | SLO and Alert use `NamespacedRESTConfig`; Synth uses `LoadSMConfig`. The adapter factory closure captures the config loading strategy, keeping the router auth-agnostic. |
| Provider descriptors hard-coded in each provider package (FR-003) | Provider REST APIs lack K8s-style discovery. The descriptors are small and stable: group, version, kind, singular, plural, aliases. Registering them from the provider package keeps ownership clear. |
| Extend `Provider` interface with `ResourceAdapters()` method | Single registration point: providers already declare `Commands()` and `ConfigKeys()`. Adding `ResourceAdapters()` keeps all provider contributions in one interface. The root command loop already iterates providers, so wiring is trivial. Providers without adapters return `nil`. |
| Auto-detect provider types from YAML files during push (FR-017) | The router already knows all registered descriptors (native + provider). During push, `supportedDescriptors()` returns the merged set. Resources read from disk are matched by GVK against this set -- no additional detection logic needed. |

## Compatibility

**Unchanged:**
- All existing `gcx resources` commands continue to work for native K8s-backed resource types
- `gcx slo`, `gcx synth`, `gcx alert` commands continue to work (with deprecation warnings)
- `gcx providers list` continues to work
- Existing tests for native resource flows pass without modification
- Processor pipeline (`NamespaceOverrider`, `ManagerFieldsAppender`, `ServerFieldsStripper`) works unchanged on provider-backed resources because they operate on `*resources.Resource` which wraps `unstructured.Unstructured` -- the same envelope format used by provider adapters

**New:**
- Provider resource types appear in `gcx resources list` output
- Provider resources are addressable via `gcx resources get/push/pull/delete/edit`
- Short aliases (`slo`, `checks`, `probes`, `rules`, `groups`) resolve in the selector parser

**Deprecated:**
- Top-level provider commands print stderr deprecation notices

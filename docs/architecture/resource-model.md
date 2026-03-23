# Core Abstractions and Resource Model

## Overview

grafanactl's resource model is built on a Kubernetes-style representation borrowed directly from `k8s.io/apimachinery`. Every Grafana resource — dashboard, folder, alert rule — is represented as an `unstructured.Unstructured` object carrying `apiVersion`, `kind`, `metadata`, and `spec` fields. This design choice unlocks use of the full Kubernetes client-go ecosystem, including dynamic clients, paginators, and server-side apply semantics.

The central pipeline that enables user-facing commands like `grafanactl resources get dashboards/my-dash` is:

```
User input string
      |
      v
  [Selector]        -- partial spec from user input
  (selector.go)
      |
      v  (via discovery.Registry.MakeFilters)
  [Filter]          -- fully-resolved spec with complete GVK
  (filter.go)
      |
      v  (via dynamic client)
  [Resource]        -- concrete fetched/read object
  (resources.go)
      |
      v  (via Processors)
  [Transformed Resource]  -- ready for write/push
  (process/)
```

---

## 1. The Resource Type

**File:** `internal/resources/resources.go`

```
┌──────────────────────────────────────────────────────────┐
│  Resource                                                │
│                                                          │
│  Raw    utils.GrafanaMetaAccessor  ←─ typed Grafana API │
│  Object unstructured.Unstructured  ←─ raw K8s object    │
│  Source SourceInfo                 ←─ origin tracking   │
└──────────────────────────────────────────────────────────┘
```

`Resource` (line 28) wraps two complementary representations:

- **`Object`**: the raw `unstructured.Unstructured` map (`map[string]any`) from `k8s.io/apimachinery`. This is what gets serialized to JSON/YAML and sent to the API.
- **`Raw`**: a `GrafanaMetaAccessor` — Grafana's typed accessor layer over the unstructured object. It provides methods like `GetManagerProperties()`, `SetManagerProperties()`, `GetSourceProperties()`, `GetFolder()` that would otherwise require manual map traversal.

### SourceInfo

```go
type SourceInfo struct {
    Path   string        // absolute file path on disk
    Format format.Format // JSON or YAML
}
```

Every `Resource` carries a `SourceInfo` (line 374) recording where it came from. This enables:
- Round-trip fidelity: pulled YAML stays YAML on push
- Error messages: "error in file://./resources/dashboards.yaml"
- The `ServerFieldsStripper` processor to preserve the path annotation

### Manager Metadata

Resources carry manager metadata in annotations (via `GrafanaMetaAccessor`):
- `grafana.app/manager-kind` — which tool manages the resource (grafanactl uses `utils.ManagerKindKubectl` as placeholder, line 19)
- `grafana.app/manager-identity` — identity string ("grafanactl")
- `grafana.app/source-path` — original file path

`IsManaged()` (line 161) returns true when the manager kind matches `ResourceManagerKind`. Resources managed by the UI (with `grafana.app/saved-from-ui` annotation) or other tools are protected from accidental overwrites unless `--include-managed` is passed.

### ResourceRef — the Collection Key

```go
type ResourceRef string
// Format: "group/version/kind/namespace-name"
```

`Ref()` (line 89) generates a unique stable key used as the map key in `Resources`.

### The Resources Collection

```go
type Resources struct {
    collection    map[ResourceRef]*Resource  // deduplicates by ref
    onChangeFuncs []func(resource *Resource)
}
```

Key operations:
- `Add()` — deduplicates: adding the same resource twice overwrites (line 235)
- `ForEach()` — sequential iteration with error propagation
- `ForEachConcurrently(ctx, maxInflight, fn)` — bounded-concurrency iteration via `errgroup.SetLimit` (line 283)
- `GroupByKind()` — returns `map[string]*Resources` for writer grouping
- `Merge()` — merge two collections (used by serve command for live reload)
- `OnChange(cb)` — event hook called on every `Add()` (used by serve for live updates)

---

## 2. The Descriptor Type

**File:** `internal/resources/descriptor.go`

A `Descriptor` is the complete, unambiguous identity of a resource type:

```go
type Descriptor struct {
    GroupVersion schema.GroupVersion  // e.g. {Group: "dashboard.grafana.app", Version: "v1alpha1"}
    Kind         string               // e.g. "Dashboard"
    Singular     string               // e.g. "dashboard"
    Plural       string               // e.g. "dashboards"
}
```

It provides both `GroupVersionKind()` (for API calls) and `GroupVersionResource()` (for k8s client routing, which uses the plural form). The `Matches(gvk)` method (line 64) is used by `Filter.Matches()` to check if a resource belongs to a filter.

String representation: `dashboards.v1alpha1.dashboard.grafana.app`

---

## 3. The Selector → Filter Resolution Pipeline

### Selectors (user input layer)

**File:** `internal/resources/selector.go`

A `Selector` is an unvalidated user specification parsed from CLI arguments:

```go
type Selector struct {
    Type             FilterType   // All | Single | Multiple
    GroupVersionKind PartialGVK   // partial — may lack group/version
    ResourceUIDs     []string     // resource names, if specified
}
```

`PartialGVK` (line 140) accepts any level of specificity:

```
Input string format:  <resource>[.<version>.<group>][/<uid1>[,<uid2>...]]

Parsing rules (SplitN on "."):
  1 part:  "dashboards"               → Resource="dashboards"
  2 parts: "dashboards.folder"        → Resource="dashboards", Group="folder"
  3 parts: "dashboards.v1alpha1.dashboard.grafana.app"
                                      → Resource="dashboards", Version="v1alpha1",
                                        Group="dashboard.grafana.app"
```

FilterType is assigned during parsing (line 102-125):
- No UID → `FilterTypeAll`
- One UID → `FilterTypeSingle`
- Multiple UIDs (comma-separated) → `FilterTypeMultiple`

### Concrete examples from selector_test.go

```
"dashboards"                              → FilterTypeAll,    Resource="dashboards"
"dashboards/foo"                          → FilterTypeSingle,  Resource="dashboards", UIDs=["foo"]
"dashboards/foo,bar"                      → FilterTypeMultiple, Resource="dashboards", UIDs=["foo","bar"]
"dashboards.v1alpha1.dashboard.grafana.app/foo,bar"
                                          → FilterTypeMultiple, Version="v1alpha1",
                                            Group="dashboard.grafana.app", UIDs=["foo","bar"]
```

### Filters (resolved layer)

**File:** `internal/resources/filter.go`

A `Filter` is a Selector that has been resolved against the discovery registry. It replaces `PartialGVK` with a concrete `Descriptor`:

```go
type Filter struct {
    Type         FilterType
    Descriptor   Descriptor   // complete GVK + plural/singular — fully resolved
    ResourceUIDs []string
}
```

`Filter.Matches(res Resource)` (line 65) checks both the descriptor (GVK equality) and the UIDs list. `Filters.Matches(res)` (line 89) returns true if any filter in the list matches — empty filters match all resources.

### The Resolution Step: Registry.MakeFilters

**File:** `internal/resources/discovery/registry.go`, line 80

```
Selector (PartialGVK)
      |
      v  registry.MakeFilters(opts)
      |
      ├── version specified? ──── LookupPartialGVK ─────────→ single Descriptor → Filter
      |
      ├── preferredVersionOnly? ─ LookupPartialGVK ─────────→ single Descriptor → Filter
      |
      └── all versions? ───────── LookupAllVersionsForPartialGVK → []Descriptor → []Filters
```

`MakeFiltersOptions.PreferredVersionOnly` controls whether to resolve to one filter per type (pull uses all versions; push uses preferred).

---

## 4. The Discovery System

**Files:** `internal/resources/discovery/registry.go`, `registry_index.go`

### Architecture

```
Grafana API (/apis endpoint)
      |
      v  k8s discovery.Client.ServerGroupsAndResources()
      |
[APIGroup list]   [APIResourceList list]
      |
      v  FilterDiscoveryResults()  ← strips ignoredResourceGroups + non-namespaced + subresources
      |
      v  RegistryIndex.Update()
      |
      ├── shortGroups:       {"dashboard": "dashboard.grafana.app", ...}
      ├── longGroups:        {"dashboard.grafana.app": {}, ...}
      ├── preferredVersions: {"dashboard.grafana.app": {Group:..., Version:"v1"}, ...}
      ├── descriptors:       {GroupVersion → []Descriptor}
      ├── kindNames:         {"Dashboard": [{Group:"dashboard.grafana.app", Kind:"Dashboard"}]}
      ├── singularNames:     {"dashboard": [...]}
      └── pluralNames:       {"dashboards": [...]}
```

### RegistryIndex — the lookup core

**File:** `internal/resources/discovery/registry_index.go`

The index resolves a partial name string to candidates via `getKindCandidates()` (line 258), which checks three maps in order: `kindNames` → `singularNames` → `pluralNames`. This means `"Dashboard"`, `"dashboard"`, and `"dashboards"` all resolve to the same candidates.

`filterCandidates()` (line 271) then narrows by group and version, falling back to `preferredVersions` when version is omitted.

Short group names work: `"folders.folder"` resolves `"folder"` via `shortGroups` to `"folder.grafana.app"` (line 280-283). The short name is the first DNS label: `makeShortName("folder.grafana.app") → "folder"`.

### Ignored Resource Groups

The `ignoredResourceGroups` global (line 19) excludes these groups from discovery:

```
apiregistration.k8s.io          — internal K8s
featuretoggle.grafana.app       — read-only feature flags
service.grafana.app             — internal service registry
userstorage.grafana.app         — internal user storage
notifications.alerting.grafana.app — pending decision
iam.grafana.app                 — identity/access management
```

Additionally, `FilterDiscoveryResults()` (line 181) excludes:
- Non-namespaced resources (line 207) — all Grafana resources are namespaced
- Subresources (containing `/` in name, line 212) — e.g. `dashboards/status`

### Preferred Versions

Grafana follows standard Kubernetes API versioning: each group advertises a `preferredVersion` that clients should use by default. The registry tracks this in `preferredVersions` map. When a user specifies `"dashboards"` without a version, the preferred version (e.g. `v1`) is selected automatically.

---

## 5. The Processor Pattern

**File:** `internal/resources/remote/remote.go` (interface), `internal/resources/process/` (implementations)

```go
// Defined in remote/remote.go
type Processor interface {
    Process(res *resources.Resource) error
}
```

Processors transform resources in-place before push or after pull. They are passed as `[]Processor` in `PushRequest` and `PullRequest` and applied sequentially per resource.

### ManagerFieldsAppender (push pipeline)

**File:** `internal/resources/process/managerfields.go`

Applied during push (wired in `cmd/grafanactl/resources/push.go` line 148). Writes manager metadata into annotations on resources that are managed by grafanactl:

```
r.Raw.SetManagerProperties({Kind: ResourceManagerKind, Identity: "grafanactl"})
r.Raw.SetSourceProperties({Path: "file:///path/to/resource.yaml"})
```

Skipped if `r.IsManaged()` returns false — protects externally-managed resources.
Skipped entirely when `--omit-manager-fields` CLI flag is set.

### ServerFieldsStripper (pull pipeline)

**File:** `internal/resources/process/serverfields.go`

Applied during pull (wired in `cmd/grafanactl/resources/pull.go` line 121). Removes server-generated ephemeral fields to produce clean, round-trippable files:

Annotations removed:
- `grafana.app/createdBy`, `grafana.app/updatedBy`, `grafana.app/updatedTimestamp` — always
- `grafana.app/manager-*`, `grafana.app/source-*` — only for grafanactl-managed resources (re-added on push)

Labels removed:
- `grafana.app/deprecatedInternalID`

Also reconstructs the object as a clean minimal structure (`apiVersion`, `kind`, `metadata`, `spec`) stripping any other server-injected top-level fields.

### NamespaceOverrider (push pipeline)

**File:** `internal/resources/process/namespace.go`

Always applied first in the push pipeline (line 145 in push.go). Overwrites the `metadata.namespace` of every resource with the target context's namespace. This enables pulling from one org/stack and pushing to another without manually editing files.

### Pull Output Directory Structure

Files written by the pull pipeline use a versioned path that includes Kind, API version, and group:

```
{OutputDir}/{Kind}.{Version}.{Group}/{Name}.{ext}
Example:  ./resources/Dashboard.v1alpha1.dashboard.grafana.app/my-dash.yaml
```

This replaces the old format (`{Kind}/{Name}.{ext}`) to make the API version
and group unambiguous, which is important when multiple versions of the same
resource type are pulled simultaneously.

### Pipeline Wiring

```
PUSH pipeline (cmd/grafanactl/resources/push.go):
  procs = [NamespaceOverrider(cfg.Namespace), ManagerFieldsAppender{}]
  PushRequest{Resources, Processors: procs, ...}
  → pusher.Push() calls Process() on each resource before Create/Update

PULL pipeline (cmd/grafanactl/resources/pull.go):
  PullRequest{Processors: [ServerFieldsStripper{}], ...}
  → puller.Pull() calls Process() on each resource after fetching from API
```

---

## 5b. Provider-Backed Resources: ResourceAdapter and Router

**Files:** `internal/resources/adapter/adapter.go`, `internal/resources/adapter/router.go`

Some resource types are backed by provider REST APIs (SLO, Synthetic Monitoring, Alert)
rather than by the Grafana k8s-compatible `/apis` endpoint. These types plug into the
unified `resources` pipeline via the `ResourceAdapter` interface:

```
ResourceAdapter interface
  +-- List(ctx, ListOptions)    → (*UnstructuredList, error)
  +-- Get(ctx, name, GetOptions) → (*Unstructured, error)
  +-- Create(ctx, obj, CreateOptions) → (*Unstructured, error)
  +-- Update(ctx, obj, UpdateOptions) → (*Unstructured, error)
  +-- Delete(ctx, name, DeleteOptions) → error
  +-- Descriptor() Descriptor
  +-- Aliases() []string
```

`adapter.Factory` is `func(ctx context.Context) (ResourceAdapter, error)` — a lazy
constructor that is only called on first use and its result cached for the router's
lifetime.

### Self-Registration Pattern

Provider packages call `adapter.Register()` in their `init()` function with a
`Registration{Factory, Descriptor, Aliases, GVK}`. This is the database/sql driver
pattern — importing the provider package is sufficient to register its adapters.

### ResourceClientRouter

`ResourceClientRouter` wraps both a `DynamicClient` (k8s path) and a
`map[GVK]Factory` (adapter path). For every CRUD call:
1. Look up GVK in the factory map
2. If found: lazily initialize the adapter (first call only), then delegate
3. If not found: fall through to the k8s dynamic client

This makes provider-backed types transparent to the rest of the pipeline — the
Pusher, Puller, and Deleter call the router without knowing whether the underlying
client is a REST adapter or the k8s dynamic client.

### Discovery Integration

Provider descriptors are injected into the `RegistryIndex` via `RegisterStatic(desc, aliases)`
so that provider types appear in `resources schemas` output and resolve correctly from
selector strings like `"slos"` or `"rules"`.

---

## 6. Why the Kubernetes Resource Model

Grafana 12+ exposes its API as a Kubernetes-style API server (using `grafana/grafana/pkg/apimachinery`). The same `apiVersion/kind/metadata/spec` structure used by Kubernetes is used by Grafana's API. This was not a grafanactl design choice — it is a direct consequence of Grafana's server architecture.

Given that reality, using `k8s.io/client-go` and `k8s.io/apimachinery` directly provides:

1. **Dynamic discovery** — `ServerGroupsAndResources()` returns all supported types without needing hardcoded lists; new resource types in Grafana are automatically available
2. **Pagination** — `k8s.io/client-go`'s pager handles continuation tokens transparently
3. **Dry-run semantics** — the K8s `dryRun: All` option maps directly to Grafana's API
4. **Unstructured representation** — `map[string]any` accommodates any resource shape without pre-generated Go types for each Grafana resource kind
5. **Familiar UX** — the kubectl-style CLI patterns (`resources get dashboards/foo`, context switching) are immediately recognizable to Grafana users who work with Kubernetes

---

## 7. Type Relationship Summary

```
PartialGVK                         Descriptor
┌─────────────────────┐            ┌──────────────────────────────┐
│ Group   string       │  ──via──→  │ GroupVersion  schema.GV      │
│ Version string       │  registry  │ Kind          string          │
│ Resource string      │            │ Singular      string          │
└─────────────────────┘            │ Plural        string          │
                                   └──────────────────────────────┘
         │                                       │
         │                                       │
         v                                       v
      Selector                               Filter
┌─────────────────────┐            ┌──────────────────────────────┐
│ Type   FilterType    │  ──via──→  │ Type         FilterType      │
│ GVK    PartialGVK    │  registry  │ Descriptor   Descriptor      │
│ UIDs   []string      │            │ ResourceUIDs []string        │
└─────────────────────┘            └──────────────────────────────┘
                                                │
                                                │ used by
                                                v
                                           Resource
                                   ┌──────────────────────────────┐
                                   │ Raw    GrafanaMetaAccessor    │
                                   │ Object unstructured.Unstruct  │
                                   │ Source SourceInfo             │
                                   └──────────────────────────────┘
                                                │
                                                │ collected into
                                                v
                                           Resources
                                   ┌──────────────────────────────┐
                                   │ collection map[ResourceRef]   │
                                   │ ForEachConcurrently(...)      │
                                   │ GroupByKind() → map[string]   │
                                   └──────────────────────────────┘
```

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `internal/resources/resources.go` | `Resource`, `Resources`, `SourceInfo`, `ResourceRef` types |
| `internal/resources/descriptor.go` | `Descriptor`, `Descriptors` types |
| `internal/resources/selector.go` | `Selector`, `PartialGVK`, `ParseSelectors()` |
| `internal/resources/filter.go` | `Filter`, `Filters`, `FilterType` |
| `internal/resources/discovery/registry.go` | `Registry`, `MakeFilters()`, `FilterDiscoveryResults()` |
| `internal/resources/discovery/registry_index.go` | `RegistryIndex`, lookup/resolution logic |
| `internal/resources/remote/remote.go` | `Processor` interface |
| `internal/resources/process/managerfields.go` | `ManagerFieldsAppender` |
| `internal/resources/process/serverfields.go` | `ServerFieldsStripper` |
| `internal/resources/process/namespace.go` | `NamespaceOverrider` |
| `cmd/grafanactl/resources/push.go` | Push pipeline wiring (processors, registry, filters) |
| `cmd/grafanactl/resources/pull.go` | Pull pipeline wiring (processors, registry, filters) |
| `internal/resources/adapter/adapter.go` | `ResourceAdapter` interface and `Factory` type |
| `internal/resources/adapter/register.go` | Global `Register()`, `AllRegistrations()` for self-registration |
| `internal/resources/adapter/router.go` | `ResourceClientRouter` — routes CRUD to adapter or dynamic client |
| `internal/resources/discovery/openapi.go` | `SchemaFetcher` — fetches OpenAPI v3 schemas with disk caching; used by `resources schemas` |
| `internal/config/context.go` | `ContextWithName` / `ContextNameFromCtx` — threads config context name through `context.Context` to adapter factories |

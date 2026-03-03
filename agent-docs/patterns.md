# Pattern Analysis and Contradiction Resolutions

## Architectural Patterns Identified

### 1. Kubernetes Resource Model Adoption (High Confidence: 97%)

grafanactl does not merely borrow Kubernetes conventions -- it directly uses
`k8s.io/apimachinery` and `k8s.io/client-go` because Grafana 12+ exposes a
Kubernetes-compatible `/apis` endpoint. The choice is dictated by the server
architecture, not by preference.

**Consequences throughout the codebase:**
- Resources are `unstructured.Unstructured` (map-based, no pre-generated Go types)
- Discovery uses `ServerGroupsAndResources()` to learn available types at runtime
- Pagination, dry-run, and error semantics all follow Kubernetes conventions
- The Descriptor/Filter/Selector abstraction mirrors how kubectl resolves GVK

**Evidence across domains:**
- Resource Model domain: `Resource` wraps `unstructured.Unstructured` + `GrafanaMetaAccessor`
- Client/API domain: `NamespacedClient` wraps `k8s.io/client-go/dynamic.Interface`
- Config domain: `NamespacedRESTConfig` bridges grafanactl config to `rest.Config`
- Data Flows domain: push/pull use k8s `metav1.CreateOptions`, `ListOptions`, etc.

---

### 2. Options Pattern for CLI Commands (High Confidence: 96%)

Every `resources` subcommand follows a strict four-part structure:
1. An `opts` struct holding all command-specific state
2. A `setup(flags)` method that binds CLI flags to struct fields
3. A `Validate()` method that checks semantic constraints before any I/O
4. A constructor function that wires opts into a `cobra.Command`

Shared cross-cutting concerns (`OnErrorMode`, `io.Options`, `configOpts`) are
composed into the opts struct via embedding or pointer injection, not inherited.

**Evidence across domains:**
- CLI Layer domain: push, pull, get, delete, edit, validate, list, serve all follow this
- Config domain: `config.Options` is created once per command group and passed by pointer
- Data Flows domain: `MaxConcurrent`, `DryRun`, `OnError` are all opts fields that
  flow into `PushRequest`/`PullRequest` structs

---

### 3. Processor Pipeline (High Confidence: 94%)

Resource transformations are modeled as a `Processor` interface with a single method:
```
Process(res *Resource) error
```

Processors are composed into ordered slices and applied per-resource at well-defined
points in the push and pull pipelines. This keeps transformation logic decoupled
from I/O logic.

**Current processors:**
- `NamespaceOverrider` -- rewrites namespace to target context (push, always first)
- `ManagerFieldsAppender` -- stamps manager/source annotations (push)
- `ServerFieldsStripper` -- removes server-generated fields for clean files (pull)

**Extension pattern:** New processors can be added without modifying the push/pull
pipeline code -- just append to the `[]Processor` slice in the command wiring.

---

### 4. Selector-to-Filter Resolution Pipeline (High Confidence: 95%)

User input flows through a two-stage resolution process:

```
CLI argument  -->  Selector (partial, unvalidated)
                       |
                   Discovery Registry
                       |
                   Filter (fully resolved, complete GVK)
```

This separation keeps the CLI layer ignorant of API details. Selectors are pure
parsing; Filters require a live connection to Grafana for GVK resolution.

The discovery registry maintains indexes by kind name, singular name, and plural
name, plus a short-group-name shortcut (e.g., `"folder"` resolves to
`"folder.grafana.app"`). This enables ergonomic short-form input like
`"dashboards/my-dash"`.

---

### 5. Dual-Client Architecture (High Confidence: 93%)

Two distinct client paths serve different purposes:

| Path | Target endpoint | Library | Use case |
|------|----------------|---------|----------|
| Dynamic client | `/apis` (K8s-compatible) | `k8s.io/client-go` | All resource CRUD |
| OpenAPI client | `/api` (Grafana REST) | `grafana-openapi-client-go` | Health checks, version checks |

Within the dynamic client path, there are two specializations:
- `NamespacedClient` -- used for push operations (Create/Update/Delete)
- `VersionedClient` -- used for pull operations (List/Get, handles version re-fetch)

**Evidence across domains:**
- Client/API domain: documented both paths and their distinct transports
- Config domain: `NewNamespacedRESTConfig` builds the k8s REST config; `ClientFromContext` builds the OpenAPI client
- Data Flows domain: Pusher uses `NamespacedClient`; Puller uses `VersionedClient`

---

### 6. Context-Based Configuration (High Confidence: 96%)

Directly modeled after kubectl's kubeconfig pattern. Key design decisions:

- Named contexts in a single YAML file, one "current" at a time
- Simplified model: grafanactl merges cluster+auth+user into a single context
  (kubectl separates them into three lists for reuse)
- Environment variables override the current context only, never mutate the file
- XDG Base Directory specification for file location
- Reflection-based editor: `SetValue`/`UnsetValue` use YAML struct tags for
  path traversal, so adding a new config field requires zero registration code

**Loading priority chain:**
```
--config flag  >  $GRAFANACTL_CONFIG  >  $XDG_CONFIG_HOME  >  ~/.config  >  $XDG_CONFIG_DIRS
```

---

### 7. Concurrency via errgroup (High Confidence: 95%)

All concurrent operations use `golang.org/x/sync/errgroup`, with two patterns:

1. **Bounded concurrency** (`errgroup.SetLimit`): FSReader file reads,
   `ForEachConcurrently` for push/pull/delete operations
2. **Unbounded concurrency**: Puller fetch goroutines (one per filter),
   `GetMultiple` in NamespacedClient

`ForEachConcurrently` on `Resources` is the primary concurrency primitive for
batch operations. Default limit is 10. Error propagation behavior depends on
`StopOnError`: when true, first error cancels the context; when false, errors
are recorded in `OperationSummary` and processing continues.

---

### 8. Two-Phase Push with Folder Dependency Ordering (High Confidence: 94%)

Folders must exist before resources that reference them. The push pipeline
implements this via:

1. **Phase 1:** Topological sort of folders by parent-child relationships
   (`SortFoldersByDependency`), then push level-by-level (concurrent within
   each level, sequential between levels)
2. **Phase 2:** All non-folder resources pushed concurrently

This is a hard invariant. Any modification to push must preserve the two-phase
approach or nested folder creation will break.

---

### 9. Structured Error Handling (High Confidence: 91%)

Errors flow through a multi-layer translation chain:

```
k8s StatusError  -->  APIError (formatted)  -->  DetailedError (rich rendering)
```

- `ParseStatusError` in the dynamic client layer normalizes k8s errors into `APIError`
- `ErrorToDetailedError` in the CLI layer converts any error into `DetailedError`
  with a summary, details, suggestions, and optional docs link
- Commands never call `os.Exit` -- they return errors from `RunE`, and `main.go`
  handles the exit code

The conversion pipeline is extensible: new error types are handled by adding a
converter function to the `errorConverters` slice.

---

### 10. Source Tracking for Round-Trip Fidelity (High Confidence: 92%)

Every `Resource` carries a `SourceInfo{Path, Format}` recording where it was read
from and in what format. This enables:

- Round-trip format preservation: YAML stays YAML, JSON stays JSON
- Meaningful error messages with file paths
- The serve command's save-back feature (write modified dashboard to the original file)

---

---

### 11. Provider Plugin System (High Confidence: 93%)

Providers are first-class extension points that contribute Cobra commands and
configuration to grafanactl. The pattern separates the plugin contract from
command registration:

```
Provider interface
  +-- Name()       string               -- unique identifier
  +-- ShortDesc()  string               -- one-line description
  +-- Commands()   []*cobra.Command     -- contributed commands
  +-- Validate()   func(map[string]string) error
  +-- ConfigKeys() []ConfigKey          -- config metadata (name + secret flag)
```

**Registry:** `providers.All()` returns all compile-time registered providers
as a `[]Provider` slice. The root command iterates this slice to mount each
provider's commands and to pass the list to `RedactSecrets`.

**Secret redaction:** `providers.RedactSecrets(providerConfigs, registered)`
applies a secure-by-default model:
- Known provider + `Secret=false` key → left as-is
- Everything else (undeclared keys, unknown providers, `Secret=true`) → redacted

**Config storage:** Provider configs live in
`Context.Grafana.Providers map[string]map[string]string`, indexed by provider
name. Reflection-based editor picks them up via the `yaml:"providers"` tag.

**Evidence:**
- `internal/providers/provider.go`: `Provider` interface and `ConfigKey` type
- `internal/providers/registry.go`: `All()` function
- `internal/providers/redact.go`: `RedactSecrets` implementation
- `cmd/grafanactl/providers/command.go`: `providers list` command
- `internal/config/types.go`: `Providers` field on `GrafanaConfig`

---

### 12. Direct HTTP Client for Datasource APIs (High Confidence: 91%)

Query clients for Prometheus and Loki bypass the k8s dynamic client entirely.
They use `rest.HTTPClientFor` to create a plain `*http.Client` from the same
`rest.Config` used by the dynamic client, then call Grafana's datasource-specific
sub-resource endpoints directly:

```
NamespacedRESTConfig
       |
   rest.HTTPClientFor(&cfg.Config)
       |
   *http.Client
       |
   POST /apis/query.grafana.app/v0alpha1/namespaces/{ns}/query
   GET  /apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/api/v1/...
   GET  /apis/loki.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/...
```

**Why not the dynamic client?** These endpoints do not follow the standard
K8s resource CRUD model (no GVK, no `List`/`Get`/`Create`/`Update`). They are
query/stream endpoints that return Grafana-native response formats, not
`unstructured.Unstructured` objects.

**Auth reuse:** `rest.HTTPClientFor` respects `BearerToken` and
`Username+Password` on the `rest.Config`, so the same auth config flows to
all three client paths without duplication.

**Output rendering:** Query results can be rendered as tables, JSON/YAML, or
terminal charts (`internal/graph`). The `query` command registers custom codecs
(`queryTableCodec`, `queryGraphCodec`) into the `io.Options` codec registry.

**Evidence:**
- `internal/query/prometheus/client.go`: `NewClient` calls `rest.HTTPClientFor`
- `internal/query/loki/client.go`: same pattern
- `cmd/grafanactl/query/command.go`: codec registration and datasource-type dispatch
- `internal/graph/chart.go`: `RenderChart` auto-selects line vs bar chart

---

## Contradiction Resolutions

### 1. DiscoverStackID Called Twice

**Observed in:** Config System domain and Client/API Layer domain.

The config loading chain calls `DiscoverStackID` during validation (in
`GrafanaConfig.validateNamespace`) and again in `NewNamespacedRESTConfig`. Both
domains note this duplication. The Config System domain explicitly identifies it
as "a known inefficiency (no caching between the two calls)."

**Resolution:** This is a confirmed minor inefficiency, not a contradiction. Both
calls are real. The second call is necessary because `NewNamespacedRESTConfig`
operates on the already-validated config and needs the resolved namespace.
Caching would require threading state between the validation and REST config
construction steps.

### 2. GetMultiple Concurrency Limit

**Observed in:** Client/API domain says `GetMultiple` has "no SetLimit call,"
while Data Flows domain says push operations use `errgroup.SetLimit(maxConcurrent)`.

**Resolution:** Both are correct at different layers. `GetMultiple` in
`NamespacedClient` runs fully concurrent Gets (bounded only by QPS/Burst at the
HTTP transport level). Push concurrency is bounded by `ForEachConcurrently` in
the Pusher, which wraps the per-resource push logic (including the Get-then-
Create/Update upsert). The concurrency limit applies to the outer loop, not to
the inner `GetMultiple`.

### 3. Manager Metadata Check in Delete vs Push

**Observed in:** Data Flows domain notes that Deleter does NOT check
`IsManaged()`, while Push always checks it.

**Resolution:** Intentional design difference, not a contradiction. The Deleter
trusts the caller (the `delete` command) to have already filtered the resource
list via `ExcludeManaged` in `fetchRequest`. The Pusher checks `IsManaged()`
per-resource because the resource list comes from local files, not from a
pre-filtered fetch.

### 4. httputils Usage Scope

**Observed in:** Client/API domain states that `internal/httputils` is used by
the local development server, not by the dynamic client path.

**Resolution:** Confirmed. Despite being named "httputils," this package is NOT
part of the primary API client chain. The k8s dynamic client has its own
transport stack. `httputils` provides transport for the serve command's reverse
proxy and for any direct HTTP calls (like `DiscoverStackID`). This naming could
be confusing to newcomers.

### 5. CI Drift Check Coverage

**Observed in:** Project Structure domain notes that the CI `docs` job only
checks `cli-reference-drift`, not all three reference generators. The Makefile
has `reference-drift` targeting all three.

**Resolution:** The Makefile now has all three drift check targets
(`cli-reference-drift`, `env-var-reference-drift`, `config-reference-drift`)
plus a combined `reference-drift` target. The CI workflow may not invoke the
combined target. This is a coverage gap in CI, not a code contradiction.

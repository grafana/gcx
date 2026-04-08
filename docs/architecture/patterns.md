# Pattern Analysis and Contradiction Resolutions

## Status Marker Vocabulary

Patterns are tagged with status markers using [ThoughtWorks Technology Radar](https://www.thoughtworks.com/radar) terminology (plus `[CURRENT]`, which has no radar equivalent):

| Marker | Meaning |
|--------|---------|
| `[CURRENT]` | Implemented and enforced. All existing code already follows this. |
| `[ADOPT]` | New code must follow this. Not yet consistently applied to existing code. |
| `[TRIAL]` | In active use with known limitations; likely to be replaced or promoted. Prefer for new code unless a better option exists. |
| `[ASSESS]` | Not yet implemented. Documented for context and future direction. |

## Architectural Patterns Identified

### 1. Kubernetes Resource Model Adoption

gcx does not merely borrow Kubernetes conventions -- it directly uses
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
- Config domain: `NamespacedRESTConfig` bridges gcx config to `rest.Config`
- Data Flows domain: push/pull use k8s `metav1.CreateOptions`, `ListOptions`, etc.

---

### 2. Options Pattern for CLI Commands

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

### 3. Processor Pipeline

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

### 4. Selector-to-Filter Resolution Pipeline

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

### 5. Dual-Client Architecture

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

### 6. Context-Based Configuration

Directly modeled after kubectl's kubeconfig pattern. Key design decisions:

- Named contexts in a single YAML file, one "current" at a time
- Simplified model: gcx merges cluster+auth+user into a single context
  (kubectl separates them into three lists for reuse)
- Environment variables override the current context only, never mutate the file
- XDG Base Directory specification for file location
- Reflection-based editor: `SetValue`/`UnsetValue` use YAML struct tags for
  path traversal, so adding a new config field requires zero registration code

**Loading priority chain:**
```
--config flag  >  $GCX_CONFIG  >  $XDG_CONFIG_HOME  >  ~/.config  >  $XDG_CONFIG_DIRS
```

---

### 7. Concurrency via errgroup

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

### 8. Two-Phase Push with Folder Dependency Ordering

Folders must exist before resources that reference them. The push pipeline
implements this via:

1. **Phase 1:** Topological sort of folders by parent-child relationships
   (`SortFoldersByDependency`), then push level-by-level (concurrent within
   each level, sequential between levels)
2. **Phase 2:** All non-folder resources pushed concurrently

This is a hard invariant. Any modification to push must preserve the two-phase
approach or nested folder creation will break.

---

### 9. Structured Error Handling

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

### 10. Source Tracking for Round-Trip Fidelity

Every `Resource` carries a `SourceInfo{Path, Format}` recording where it was read
from and in what format. This enables:

- Round-trip format preservation: YAML stays YAML, JSON stays JSON
- Meaningful error messages with file paths
- The serve command's save-back feature (write modified dashboard to the original file)

---

---

### 11. Provider Plugin System

Providers are first-class extension points that contribute Cobra commands and
configuration to gcx. The pattern separates the plugin contract from
command registration:

```
Provider interface
  +-- Name()       string               -- unique identifier
  +-- ShortDesc()  string               -- one-line description
  +-- Commands()   []*cobra.Command     -- contributed commands
  +-- Validate()   func(map[string]string) error
  +-- ConfigKeys() []ConfigKey          -- config metadata (name + secret flag)
  +-- TypedRegistrations() []adapter.Registration -- adapter registrations for provider-backed resource types
```

**Registry:** `providers.All()` returns all compile-time registered providers
as a `[]Provider` slice. The root command iterates this slice to mount each
provider's commands and to pass the list to `RedactSecrets`.

**Secret redaction:** `providers.RedactSecrets(providerConfigs, registered)`
applies a secure-by-default model:
- Known provider + `Secret=false` key → left as-is
- Everything else (undeclared keys, unknown providers, `Secret=true`) → redacted

**Config storage:** Provider configs live in
`Context.Providers map[string]map[string]string`, indexed by provider
name. Reflection-based editor picks them up via the `yaml:"providers"` tag.

**Evidence:**
- `internal/providers/provider.go`: `Provider` interface and `ConfigKey` type
- `internal/providers/registry.go`: `All()` function
- `internal/providers/redact.go`: `RedactSecrets` implementation
- `internal/providers/configloader.go`: Shared `ConfigLoader` struct — all providers use this instead of duplicating config loading logic. Provides `LoadGrafanaConfig`, `LoadCloudConfig`, `LoadProviderConfig` (provider-specific `map[string]string`), `SaveProviderConfig` (write-back), and `LoadFullConfig` (full `*config.Config`)
- `internal/providers/alert/provider.go`: Second provider implementation (alert rules and groups)
- `cmd/gcx/providers/command.go`: `providers list` command
- `internal/config/types.go`: `Providers` field on `Context`
- `internal/resources/adapter/register.go`: Global adapter registration pattern (self-registration via `Register()` and `AllRegistrations()`)

---

### 12. Direct HTTP Client for Datasource APIs

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

**Contrast with external APIs:** Provider clients calling **external** APIs
(K6 Cloud, OnCall, Synth, Fleet — domains outside the Grafana server) must
**not** use `rest.HTTPClientFor`. The k8s transport round-tripper injects the
Grafana bearer token on every outgoing request, which conflicts with the
product's own auth mechanism (e.g. OnCall raw token, K6 X-Grafana-Key).
These providers use `providers.ExternalHTTPClient()` — a shared, well-tuned
`*http.Client` singleton with no auth injection — and set their own auth
headers per request.

**Output rendering:** Query results can be rendered as tables, JSON/YAML, or
terminal charts (`internal/graph`). The `query` command registers custom codecs
(`queryTableCodec`, `queryGraphCodec`) into the `io.Options` codec registry.

**Evidence:**
- `internal/query/prometheus/client.go`: `NewClient` calls `rest.HTTPClientFor`
- `internal/query/loki/client.go`: same pattern
- `cmd/gcx/datasources/query/codecs.go`: `queryTableCodec`, `queryGraphCodec` registration — shared by all per-kind query subcommands
- `cmd/gcx/datasources/query/{prometheus,loki,pyroscope,tempo,generic}.go`: per-kind constructors wired under `datasources {kind} query`
- `internal/graph/chart.go`: `RenderChart` auto-selects line vs bar chart

---

### 13. Format-Agnostic Data Fetching

Commands fetch **all** available data in `RunE`, regardless of the `--output`
format. The output format (`-o table`, `-o wide`, `-o json`, etc.) controls
**display**, not **data acquisition**. Custom table codecs select which columns
to render; the built-in JSON/YAML codecs serialize the full data structure.

This separation ensures that JSON/YAML always contain complete data, and adding
new table columns never requires changes to the fetch logic.

**Anti-pattern:** Gating data fetches on `opts.IO.OutputFormat == "wide"` or
similar sentinel checks. This causes JSON/YAML to silently omit fields that
only the wide table codec was expected to display.

**Implementation rule:**
- `RunE` calls fetch functions with no format awareness
- The result struct contains all fields (SLI, Budget, BurnRate, SLI1h, SLI1d…)
- Table codecs choose which subset of fields to render
- JSON/YAML codecs serialize the full struct via standard `encoding/json` tags

**Evidence:**
- `internal/providers/slo/definitions/status.go`: `fetchMetrics` fetches all metrics unconditionally
- `cmd/gcx/datasources/query/query.go`: query response passed to all codecs unchanged
- `internal/output/format.go`: built-in JSON/YAML codecs fall through when no custom codec is registered

**See also:** [output.md](../design/output.md) — codec requirements by command type and mutation command output spec.

---

### 14. PromQL Construction with promql-builder

PromQL expressions are built programmatically using `github.com/grafana/promql-builder/go/promql`
rather than string formatting. This eliminates string injection risks and makes
complex expressions (aggregations, binary operations, function calls) composable
and readable.

**Key API surface:**

| Builder | Purpose | Example |
|---------|---------|---------|
| `promql.Vector(name)` | Metric selector | `promql.Vector("grafana_slo_sli_window")` |
| `.LabelMatchRegexp(k, v)` | `=~` matcher | `.LabelMatchRegexp("grafana_slo_uuid", "uuid1\|uuid2")` |
| `.Range("1h")` | Range vector | `.Range("1h")` → `metric[1h]` |
| `promql.Sum(expr).By(labels)` | Aggregation | `promql.Sum(expr).By([]string{"grafana_slo_uuid"})` |
| `promql.Div(a, b).On(labels)` | Binary with matching | Division with `on(label)` clause |
| `promql.ClampMax(expr, max)` | Function call | `promql.ClampMax(expr, 1)` |
| `promql.AvgOverTime(expr)` | Range function | Wraps a range vector |
| `promql.N(value)` | Number literal | `promql.N(1)` → scalar `1` |
| `.Build()` then `.String()` | Render to string | Final step to get PromQL text |

**Batch-querying pattern:** Join multiple resource UUIDs with `|` and pass as a
regex matcher via `.LabelMatchRegexp()`. Group results back to individual
resources using `sum by (uuid_label)(...)`.

Cross-reference: Pattern 12 (Direct HTTP Client for Datasource APIs).

**Evidence:**
- `internal/providers/slo/definitions/status.go`: `buildBurnRateQuery`, `buildMetricQuery`
- Dependency: `github.com/grafana/promql-builder/go` in `go.mod`

---

### 15. Agent Mode Detection and Pipe-Aware Output

gcx detects at startup whether it is running inside an AI agent
environment (Claude Code, Cursor, GitHub Copilot, Amazon Q) and adjusts
its behavior accordingly. Detection happens at `init()` time by reading
well-known environment variables; the `--agent` CLI flag overrides env
detection when explicitly set.

**Detection priority:**

| Priority | Mechanism | Notes |
|----------|-----------|-------|
| 1 | `GCX_AGENT_MODE` env var | Explicit override — falsy value disables agent mode even if other vars are set |
| 2 | `CLAUDE_CODE`, `CURSOR_AGENT`, `GITHUB_COPILOT`, `AMAZON_Q` env vars | Any truthy value enables agent mode |
| 3 | `--agent` CLI flag | Applied after env detection; always takes precedence when explicitly passed |

**Behavioral effects when agent mode is active:**
- Color output disabled globally (`color.NoColor = true`)
- Default output format overridden to `json` (machine-parseable by default)
- Pipe-aware behaviors forced: `IsPiped=true`, `NoTruncate=true` regardless of TTY state
- In-band error JSON written to stdout on failure (see `cmd/gcx/fail/json.go`)

**Pipe detection** is also independent of agent mode. Root `PersistentPreRun` calls
`terminal.Detect()` which checks `term.IsTerminal(os.Stdout.Fd())`. When piped:
- Color disabled automatically
- Table column truncation suppressed automatically

The `--no-truncate` persistent flag provides explicit control for non-TTY use cases
(e.g., wide terminal output without truncation). Agent mode sets all pipe-aware
behaviors regardless of actual TTY state.

**Key files:**
- `internal/agent/agent.go` — `IsAgentMode()`, `SetFlag()`, `DetectedFromEnv()`
- `internal/terminal/terminal.go` — `Detect()`, `IsPiped()`, `NoTruncate()`, setters
- `cmd/gcx/root/command.go` — orchestrates detection order in `PersistentPreRun`
- `internal/output/format.go` — `io.Options` fields `IsPiped`, `NoTruncate`, `JSONFields`
- `cmd/gcx/fail/json.go` — `DetailedError.WriteJSON` for in-band error reporting

**Evidence:**
- `internal/agent/` package with `init()`-time env-var detection
- `internal/terminal/` package with TTY detection and package-level state
- Root command `PersistentPreRun` coordinates detection in a defined order
- `io.Options.BindFlags` reads `terminal.IsPiped()` / `terminal.NoTruncate()` at flag-bind time

**PersistentPreRun chaining convention:** In Cobra, a child command's `PersistentPreRun` replaces (not chains) the nearest ancestor's hook. Any command that defines its own `PersistentPreRun` must explicitly call the root hook first to preserve logger setup, TTY detection, and agent mode:

```go
PersistentPreRun: func(cmd *cobra.Command, args []string) {
    if root := cmd.Root(); root.PersistentPreRun != nil {
        root.PersistentPreRun(cmd, args)
    }
    // command-specific setup...
},
```

This applies to provider commands (`slo`, `synth`, `alert`) which each define a `PersistentPreRun` for provider-specific setup (e.g. config loading, root command propagation).

---

### 16. ResourceAdapter and Provider CRUD Routing

Provider-backed resource types (SLO, Synthetic Monitoring, Alert) implement the
`adapter.ResourceAdapter` interface to bridge their REST clients to the unified
`resources` pipeline. Adapters self-register at `init()` time using
`adapter.Register()` — the same database/sql driver pattern. At runtime a
`ResourceClientRouter` routes each CRUD call to the correct adapter by GVK,
falling back to the k8s dynamic client for non-provider resource types.

**Key components:**
- `adapter.ResourceAdapter` interface: `List`, `Get`, `Create`, `Update`, `Delete`, `Descriptor`, `Aliases`
- `adapter.Factory`: lazy constructor `func(ctx context.Context) (ResourceAdapter, error)` — invoked only on first use, then cached
- `adapter.Register()` / `adapter.AllRegistrations()`: global self-registration called from provider `init()` functions
- `ResourceClientRouter`: routes CRUD operations by GVK; lazily initializes adapter instances; falls back to dynamic client for unregistered GVKs
- `RegistryIndex.RegisterStatic()`: injects provider descriptors into the discovery lookup indexes so provider types appear in `resources schemas` and resolve from `resources get slos`

**Evidence:**
- `internal/resources/adapter/adapter.go`: `ResourceAdapter` interface definition
- `internal/resources/adapter/register.go`: `Register()`, `AllRegistrations()`, and global registration machinery
- `internal/resources/adapter/router.go`: `ResourceClientRouter` implementation
- `internal/providers/slo/definitions/resource_adapter.go`: SLO provider implementation
- `internal/providers/synth/checks/resource_adapter.go`: Synthetic Monitoring implementation
- `internal/providers/alert/resource_adapter.go`: Alert rules implementation

**Slug-ID naming convention (Fleet, Synth checks):** Providers whose APIs use
numeric IDs but whose users want human-readable names use a composite
`metadata.name = slug-id` format (e.g. `grafana-instance-health-5594`).
Shared helpers in `internal/resources/adapter/slug.go` — `SlugifyName`,
`ExtractIDFromSlug`, `ExtractInt64IDFromSlug`, `ComposeName` — implement this
pattern. `GetResourceName()` produces the composite name; `SetResourceName()`
extracts and restores the numeric ID so that CRUD operations work after a K8s
round-trip. Provider table output shows `NAME` (the slug-id) so users can
copy-paste it directly into `get`, `update`, and `delete` commands.

**Usage:** When a provider resource type needs CRUD via `gcx resources`, implement `ResourceAdapter`, call `adapter.Register()` in `init()`, and call `RegistryIndex.RegisterStatic()` in `discovery.NewDefaultRegistry`.

### Provider / Resources Output Consistency `[ADOPT]`

Provider CRUD commands must use their registered `ResourceAdapter` (via
TypedCRUD) for data access, not raw REST clients. This ensures:

- JSON/YAML output is identical to the `resources` pipeline by construction.
- Table/wide codecs may access domain types `T` for richer columns (e.g.
  SLI%, burn rate, budget remaining).
- The `resources` pipeline uses generic resource columns (name, namespace,
  age) for its table codec.

Provider commands that bypass the adapter for CRUD operations are
non-compliant. Extension commands (status, timeline, etc.) may use raw
clients since they have no `resources` pipeline equivalent.

### TypedCRUD Pattern `[ADOPT → TRIAL]`

TypedCRUD is the current required pattern for new providers implementing
ResourceAdapter. It bridges typed domain objects to Kubernetes-style
unstructured envelopes.

**Current requirement:** New providers must use TypedCRUD for adapter
registration.

**Trajectory:** Domain types should be designed with eventual K8s metadata
interface compliance in mind (metadata.name, metadata.namespace,
apiVersion/kind). The long-term goal is typed resources that satisfy K8s
interfaces directly, eliminating the TypedCRUD bridge.

Do not introduce new serialization bridges, dispatch patterns, or
type-erasure mechanisms. If TypedCRUD does not fit your use case, raise
the issue for architectural discussion.

### Provider ConfigLoader `[ADOPT]`

All provider commands must use `providers.ConfigLoader` for flag binding
(`--config`, `--context`) and config resolution (YAML + env var precedence).

| Method | Purpose | Used by |
|--------|---------|---------|
| `LoadGrafanaConfig(ctx)` | REST config for Grafana API calls | alert, fleet, incidents, kg, oncall, slo, synth |
| `LoadCloudConfig(ctx)` | Cloud token + GCOM stack info | k6, fleet |
| `LoadProviderConfig(ctx, name)` | Provider-specific `map[string]string` + namespace | synth, oncall, k6 |
| `SaveProviderConfig(ctx, name, key, val)` | Write-back a single provider config key | synth (datasource UID) |
| `LoadFullConfig(ctx)` | Full `*config.Config` (for cross-cutting lookups) | synth (datasource discovery) |

Do not:
- Import `cmd/gcx/config` from provider code (import cycle)
- Roll custom flag binding for `--config`/`--context`
- Construct HTTP clients or load credentials outside ConfigLoader
- Hardcode env var names — ConfigLoader handles `GRAFANA_PROVIDER_*` resolution
- Use `os.Getenv` for provider-specific env vars — use `LoadProviderConfig`

See [provider-checklist.md](../design/provider-checklist.md) for the UX compliance checklist.

**Context threading for `--context` flag:** The selected config context name is
threaded into adapter factories via Go's `context.Context` using helpers in
`internal/config/context.go`:

```go
// Writer side (threaded in before factory is called):
ctx = config.ContextWithName(ctx, contextName)

// Reader side (inside adapter Factory):
contextName := config.ContextNameFromCtx(ctx)
```

This lets adapters load the correct provider config for the active context
without requiring an extra parameter on the `Factory` type.

---

### 17. K8s Envelope Wrapping for Provider List/Get

Provider list/get commands that output CRUD resources (resources the user can
create, update, and delete via the CLI) wrap JSON/YAML output in K8s envelope
manifests (`apiVersion`/`kind`/`metadata`/`spec`) for round-trip compatibility
with push/pull. Table/wide codecs continue to receive raw domain types for
direct field access, since they need to pick specific fields for column rendering.

This is a companion to Pattern 13 (Format-Agnostic Data Fetching): data is
fetched unconditionally, but the _presentation_ layer converts to K8s envelopes
for structured formats while keeping raw types for tabular formats.

**Implementation rule:**

```go
// Table/wide → raw domain types for direct field access.
if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
    return opts.IO.Encode(cmd.OutOrStdout(), items)
}

// JSON/YAML → K8s envelope via ToResource().
var objs []unstructured.Unstructured
for _, item := range items {
    res, err := ItemToResource(item, namespace)
    if err != nil { return err }
    objs = append(objs, res.ToUnstructured())
}
return opts.IO.Encode(cmd.OutOrStdout(), objs)
```

**Exempt command categories** (output raw API types without wrapping):

| Category | Examples | Rationale |
|----------|----------|-----------|
| Query/search results | `insights query`, `search entities` | Time-series and aggregation results, not storable resources |
| Operational views | `status`, `health`, `inspect` | Composite or derived data, not individual resources |
| Read-only reference data | `vendors list`, `scopes list`, `entity-types list` | Discoverable metadata, not user-managed resources |
| Singleton config | `env get`, `graph-config` | Single config objects, not collections of resources |

**Evidence:**
- `internal/providers/slo/definitions/commands.go`: `newListCommand` — SLO list wraps via `ToResource`
- `internal/providers/fleet/provider.go`: `newPipelineListCommand`, `newCollectorListCommand`
- `internal/providers/kg/commands.go`: `newRulesCommand` — rules list/get wrap via `RuleToResource`

---

### 18. Table-Driven TypedCRUD Registration for Providers

Providers with many resource types (e.g., OnCall with 17 types) use a generic
`registerXResource[T]` function with functional options to register each type
in a single, self-contained call. This replaces the earlier switch-dispatch
pattern where a single adapter struct dispatched all types through runtime
kind-string matching.

**Pattern structure:**

```go
// 1. resourceMeta holds static registration metadata.
type resourceMeta struct {
    Descriptor resources.Descriptor
    Aliases    []string
    Schema, Example json.RawMessage
}

// 2. crudOption[T] configures optional CRUD operations.
type crudOption[T any] func(client *Client, crud *adapter.TypedCRUD[T])

// 3. withCreate/withUpdate/withDelete set the corresponding Fn fields.
func withCreate[T any](fn func(ctx context.Context, c *Client, item *T) (*T, error)) crudOption[T]

// 4. registerOnCallResource[T] wires everything and calls adapter.Register.
func registerOnCallResource[T any](
    loader OnCallConfigLoader,
    meta   resourceMeta,
    nameFn func(T) string,
    listFn func(ctx context.Context, client *Client) ([]T, error),
    getFn  func(ctx context.Context, client *Client, name string) (*T, error), // nil for list-only
    opts   ...crudOption[T],
)
```

**When to use:** When a provider has 4+ resource types sharing the same
API group/version and client initialization pattern. The generic helper
eliminates per-type boilerplate while keeping each registration self-documenting.

**Key properties:**
- No `any` type erasure — all 17 types use concrete generics
- No switch/case dispatch — CRUD behavior determined at registration time
- Functional options express the CRUD matrix declaratively (only 10/17 types support create, etc.)
- Special-case type conversions (e.g., Shift→ShiftRequest) are closures in the option, not if/else branches

**Evidence:**
- `internal/providers/oncall/resource_adapter.go`: `registerOnCallResource[T]`, 17 registrations
- ADR: `docs/adrs/oncall-typed-crud/001-table-driven-typedcrud.md`

### 19. Singleton Adapter Pattern (Adopt)

**Observation:** Some provider resources are singletons — exactly one instance
exists per stack with no meaningful name, no list endpoint, and no create/delete
lifecycle. Rather than falling back to provider-only commands with alternative
verbs, these can be registered as TypedCRUD adapters with a hardcoded name
`"default"` and nil `ListFn`/`CreateFn`/`DeleteFn`.

**Rationale:** Enables the full generic resource pipeline (`resources get`,
`resources push`, `resources schemas`, `resources examples`) without requiring
a separate verb vocabulary. Bulk `resources pull` silently skips singletons
(nil ListFn → `ErrUnsupported`), which is correct behavior.

**Key files:**
- `internal/providers/appo11y/overrides/resource_adapter.go`: TypedCRUD with only GetFn + UpdateFn
- `internal/providers/appo11y/settings/resource_adapter.go`: Same pattern without ETag
- ADR: `docs/adrs/appo11y-provider/001-cli-ux-and-resource-adapter-design.md`

### 20. ETag-as-Annotation Pattern (Adopt)

**Observation:** When an API uses HTTP ETags for optimistic concurrency, the
ETag value can be stored as a K8s annotation on the resource envelope (e.g.
`appo11y.ext.grafana.app/etag`). This preserves the ETag through
get→edit→push round-trips, including file-based workflows.

**Rationale:** Analogous to how `resourceVersion` works in K8s. The annotation
survives JSON/YAML serialization, is visible in `get -o yaml` output, and
is automatically included when the user pushes a previously-pulled file.
Unexported fields on domain types would be lost during marshal/unmarshal.

**Key files:**
- `internal/providers/appo11y/overrides/resource_adapter.go`: MetadataFn injects annotation, UpdateFn extracts it
- `internal/providers/appo11y/overrides/adapter.go`: ToResource preserves annotation
- ADR: `docs/adrs/appo11y-provider/001-cli-ux-and-resource-adapter-design.md`

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

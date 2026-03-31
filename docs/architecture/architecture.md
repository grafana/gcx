# Codebase Architecture Analysis: gcx

*Generated: 2026-03-02 | Domains Analyzed: 6 | Overall Confidence: 92% (High)*

---

## Executive Summary

- **gcx is a unified CLI for managing Grafana resources** operating in two tiers: (1) a K8s resource tier using Grafana 12+'s Kubernetes-compatible API via `k8s.io/client-go` for dashboards, folders, and other K8s-native resources; (2) a Cloud provider tier with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring, OnCall, Fleet Management, K6 Cloud, Knowledge Graph, IRM Incidents, Alerting) that use product-specific REST APIs.
- **The architecture is a clean layered monolith** with strict separation: CLI wiring (`cmd/`) holds no business logic; all domain logic lives in `internal/` organized by feature (config, resources, server, providers).
- **Context-based multi-environment configuration** follows the kubectl kubeconfig pattern, enabling management of multiple Grafana instances (dev, staging, prod, cloud) from a single config file.
- **A composable processor pipeline** transforms resources during push and pull, keeping I/O and transformation concerns decoupled.
- **Pluggable provider system** enables extending the CLI with new Grafana Cloud products via a self-registering `Provider` interface, each contributing CLI commands, resource adapters, and product-specific configuration.
- **Test coverage is moderate** (~40-50%) with no automated integration tests, despite a docker-compose environment being available. This is the most significant quality gap.

---

## Confidence Assessment

| Section | Score | Level | Rationale |
|---------|-------|-------|-----------|
| Project Structure | 96% | High | Exhaustive analysis of directory layout, build system, CI/CD, and toolchain |
| Resource Model | 95% | High | Core abstractions thoroughly documented with type relationships |
| CLI Layer | 94% | High | Complete command tree, options pattern, and error handling chain |
| Client/API Layer | 93% | High | Both client paths documented; minor gaps in retry/timeout behavior |
| Config System | 95% | High | Full loading chain, env overrides, namespace resolution covered |
| Data Flows | 94% | High | All four pipelines documented with concurrency models |
| Testing | 70% | Medium | No dedicated analyzer; coverage known to be moderate |
| Infrastructure | 88% | Medium | CI/CD and build well-covered; deployment beyond GitHub Releases less clear |
| **Overall** | **92%** | **High** | |

---

## 1. Architecture Overview

### Layered Structure

```
+-------------------------------------------------------------+
|  CLI Layer (cmd/gcx/)                                 |
|  - Cobra commands, flag parsing, output formatting           |
|  - No business logic; only wiring and user interaction       |
|  - internal/agent: agent-mode detection (env vars + --agent) |
+-------------------------------------------------------------+
         |                    |                    |
         v                    v                    v
+------------------+  +----------------+  +----------------+
| Config System    |  | Resource Layer |  | Server Layer   |
| (internal/       |  | (internal/     |  | (internal/     |
|  config/)        |  |  resources/)   |  |  server/)      |
| - Context mgmt   |  | - Resource     |  | - Reverse proxy|
| - Auth/TLS       |  |   abstraction  |  | - Live reload  |
| - Env overrides  |  | - Discovery    |  | - File watcher |
+------------------+  | - Local I/O    |  +----------------+
         |            | - Remote ops   |
         |            | - Processing   |
         |            +----------------+
         |
         |            +----------------+  +----------------+  +-------------------------+
         |            | Provider Layer |  | Query Layer    |  | Provider Implementations |
         |            | (internal/     |  | (internal/     |  | (internal/providers/*/)   |
         |            |  providers/)   |  |  query/)       |  | - Pluggable Cloud        |
         |            | - Provider     |  | - Prometheus   |  |   product providers      |
         |            |   interface    |  |   client       |  | - Self-registering via   |
         |            | - Registry     |  | - Loki client  |  |   init()                 |
         |            | - Secret       |  | - Direct HTTP  |  +----------------+
         |            |   redaction    |  |   (no k8s      |
         |            +----------------+  |    machinery)  |  +------------------+
         |                               +----------------+  | Linter Layer     |
         |                                                    | (internal/       |
         |                                                    |  linter/)        |
         |                                                    | - Linter engine  |
         |                                                    | - Rule interface |
         |                                                    | - Rego bundle    |
         |                                                    +------------------+
         |
         |            +----------------+  +----------------+
         |            | Graph Layer    |  | Test Utilities |
         |            | (internal/     |  | (internal/     |
         |            |  graph/)       |  |  testutils/)   |
         |            | - Terminal     |  | - Command test |
         |            |   charts       |  |   helpers      |
         |            | - Line/bar     |  | - FS helpers   |
         |            |   rendering    |  +----------------+
         |            +----------------+
         |                    |
         v                    v
+-------------------------------------------------------------+
|  Transport Layer                                             |
|  - k8s.io/client-go dynamic client (primary: /apis)         |
|  - grafana-openapi-client-go (secondary: /api)              |
|  - internal/httputils (serve command reverse proxy)          |
|  - net/http direct client (query layer: datasource APIs)     |
+-------------------------------------------------------------+
         |
         v
+-------------------------------------------------------------+
|  Grafana REST API (Kubernetes-compatible /apis endpoint)     |
+-------------------------------------------------------------+
```

### Key Architectural Decisions

1. **Kubernetes client libraries as foundation.** Grafana 12+ exposes a K8s-compatible
   API. Using `k8s.io/client-go` directly gives gcx pagination, discovery,
   dry-run, error handling, and unstructured object support for free. The trade-off
   is a large vendor directory, but the implementation savings are substantial.

2. **No public Go API.** Everything is under `internal/`. gcx is a CLI tool,
   not a library. This gives the team freedom to refactor without worrying about
   external API stability.

3. **Dynamic resource types.** Resources are discovered at runtime via the Grafana
   API's discovery endpoint, not hardcoded. This means new resource types added to
   Grafana are automatically available without gcx code changes.

4. **Vendored dependencies.** All Go dependencies are committed to `vendor/`. This
   ensures reproducible builds without network access and makes the full dependency
   graph auditable in code review.

---

## 2. Core Abstractions

### The Resource Model

The central data type is `Resource`, which wraps a Kubernetes `unstructured.Unstructured`
object (a `map[string]any`) plus Grafana-specific typed accessors and source tracking.

```
Resource
  +-- Object  (unstructured.Unstructured)   -- raw K8s-style object
  +-- Raw     (GrafanaMetaAccessor)         -- typed Grafana metadata API
  +-- Source  (SourceInfo)                  -- origin file path + format
```

Resources are collected into `Resources` (a deduplicated map keyed by `ResourceRef`),
which provides concurrent iteration, grouping, merging, and change notification.

### The Resolution Pipeline

User input ("dashboards/my-dash") must be resolved to a fully-qualified API call.
This happens in two stages:

```
User input string
      |  ParseSelectors()
      v
Selector (PartialGVK + resource UIDs)
      |  registry.MakeFilters()  [requires live API connection]
      v
Filter (complete Descriptor + UIDs)
      |  dynamic client
      v
Resource (concrete fetched/read object)
```

**Selectors** are pure parsing -- no network required. They accept short forms
(`"dashboards"`, `"dashboards/foo"`) and long forms
(`"dashboards.v1alpha1.dashboard.grafana.app/foo"`).

**Filters** contain a fully-resolved `Descriptor` with GroupVersionKind, singular/plural
names, and are used by the dynamic client for API calls.

The **Discovery Registry** bridges the two. It calls Grafana's
`ServerGroupsAndResources` endpoint and builds lookup indexes by kind name,
singular name, plural name, and short group name. This enables ergonomic input
resolution (e.g., `"dashboards"` -> `dashboards.v1.dashboard.grafana.app`).

### The Descriptor Type

A `Descriptor` is the fully-qualified identity of a resource type:

```
Descriptor
  +-- GroupVersion  (e.g., dashboard.grafana.app/v1alpha1)
  +-- Kind          (e.g., Dashboard)
  +-- Singular      (e.g., dashboard)
  +-- Plural        (e.g., dashboards)
```

It provides both `GroupVersionKind()` (for business logic) and
`GroupVersionResource()` (for k8s client routing, which needs the plural form).

---

## 3. Data Flow Pipelines

### Push (Local -> Grafana)

```
Local files  --(FSReader)--> Resources  --(Processors)--> Pusher  --> Grafana API
                                 |
                            [Dedup by GVK+name]
```

Pipeline stages:
1. Parse CLI selectors and resolve to Filters via Discovery
2. FSReader reads files concurrently (3-goroutine pipeline: walker, readers, collector)
3. Filter: skip resources not matching selectors
4. Process: `NamespaceOverrider` (rewrite namespace) then `ManagerFieldsAppender` (stamp ownership)
5. Two-phase push: folders first (topologically sorted by hierarchy), then all other resources
6. Per-resource upsert: Get -> if exists: Update with resourceVersion; if 404: Create

### Pull (Grafana -> Local)

```
Grafana API  --(Puller)--> Resources  --(Processors)--> FSWriter  --> Local files
```

Pipeline stages:
1. Parse CLI selectors; if none, expand to ALL preferred resource types
2. Concurrent fetch via `VersionedClient` (handles API version re-fetch when stored version differs)
3. Process: `ServerFieldsStripper` removes server-generated annotations and rebuilds clean objects
4. FSWriter writes files organized as `{Kind}.{Version}.{Group}/{Name}.{ext}`
5. 404/405 responses during fetch are silently skipped (not counted as errors)

### Delete

```
CLI args  --(fetch from Grafana)--> Resources  --(Deleter)--> Grafana API
```

Simpler than push/pull. No `IsManaged()` check in the Deleter itself -- callers
are expected to filter beforehand. Concurrent deletion via `ForEachConcurrently`.

### Serve (Local Development)

```
Local files  --(FSReader + file watcher)--> In-memory Resources
                                                   |
Browser  <--(Chi router + reverse proxy)----------+
   ^                                               |
   +-------(WebSocket live reload)----------------+
```

The `dev serve` command (formerly `resources serve`) starts a local HTTP server that:
- Reverse-proxies most requests to the real Grafana instance
- Intercepts dashboard/folder API calls and serves from in-memory resources
- Watches local files for changes via fsnotify
- Triggers browser reload via WebSocket (LiveReload protocol v7)

---

## 4. Configuration System

### Data Model

```
Config
  +-- CurrentContext: string
  +-- Contexts: map[string]*Context
        +-- Grafana: *GrafanaConfig
              +-- Server, User, Password, APIToken
              +-- OrgID (on-prem) / StackID (cloud)
              +-- TLS (cert, key, CA, insecure flag)
        +-- DefaultPrometheusDatasource (UID for query command default)
        +-- DefaultLokiDatasource       (UID for query command default)
        +-- Providers: map[string]map[string]string
              (per-provider config, indexed by provider name)
```

This is a simplified kubeconfig: where kubectl separates clusters, users, and
contexts into three reusable lists, gcx collapses everything into a single
context entry. Simpler but means auth and server are always paired.

### Loading Chain

```
--config flag  >  $GCX_CONFIG  >  $XDG_CONFIG_HOME  >  ~/.config  >  $XDG_CONFIG_DIRS
     |
     v
YAML file read + decode
     |
     v
Apply overrides (in order):
  1. env.Parse(currentContext.Grafana)  -- GRAFANA_SERVER, GRAFANA_TOKEN, etc.
  2. --context flag override            -- switch current context
  3. Validator                          -- enforce server, namespace, auth present
     |
     v
Config ready
```

Two loading modes:
- **Tolerant** (`loadConfigTolerant`): used by `config view`, `config set` -- no
  validation beyond YAML parsing, allows working with partial configs
- **Strict** (`LoadConfig`/`LoadGrafanaConfig`): used by `resources` commands --
  validates server URL, namespace, and credentials

### Namespace Semantics

"Namespace" maps to the Kubernetes namespace for all API calls:
- On-prem: `org-{OrgID}` (e.g., `org-1`)
- Cloud: `stacks-{StackID}` (e.g., `stacks-12345`)

Stack ID can be auto-discovered from Grafana Cloud's `/bootdata` endpoint. If
discovery succeeds, it overrides even an explicitly-configured `org-id`.

### Adding a New Config Field

Add a struct field in `types.go` with `yaml`, `env`, and optionally
`datapolicy:"secret"` tags. The editor (`SetValue`/`UnsetValue`), env parser,
and secret redactor are all reflection-driven and require zero additional
registration code.

---

## 5. Client Architecture

### Four Client Paths

The codebase has four distinct communication paths to Grafana:

**Primary (dynamic client):** `k8s.io/client-go` -> `/apis` endpoint
- Used for all resource CRUD operations
- Rate-limited at QPS=50, Burst=100 (hardcoded)
- Two specializations:
  - `NamespacedClient` for push (Create/Update/Delete)
  - `VersionedClient` for pull (List/Get with version re-fetch)

**Secondary (OpenAPI client):** `grafana-openapi-client-go` -> `/api` endpoint
- Used for health checks, version detection
- Completely separate connection setup from the dynamic client
- Not used for resource operations

**Tertiary (direct HTTP client):** `net/http` via `rest.HTTPClientFor` -> `/apis/{datasource}.grafana.app/...`
- Used by `internal/query/prometheus` and `internal/query/loki`
- Bypasses k8s API machinery entirely (no GVK, no dynamic.Interface)
- Uses the same auth config as the dynamic client (`rest.Config` -> `rest.HTTPClientFor`)
- Hits datasource-specific sub-resource endpoints (`/apis/prometheus.datasource.grafana.app/...`)

**Quaternary (provider adapter client):** `adapter.ResourceAdapter` implementations -> provider REST APIs
- Used for provider-backed resource types (SLO, Synthetic Monitoring, OnCall, Fleet, KG, IRM Incidents, Alert)
- Each adapter wraps a provider-specific REST client targeting the product's API
- Routed via `ResourceClientRouter`: calls to Pusher/Puller/Deleter are transparently dispatched to the adapter for registered GVKs, falling back to the primary dynamic client for all others
- Read-only adapters return `errors.ErrUnsupported` for Create/Update/Delete

### Auth Flow

API token takes priority over basic auth in both paths:

```
APIToken set?  --> rest.Config.BearerToken (dynamic) / TransportConfig.APIKey (OpenAPI)
User set?      --> rest.Config.Username+Password (dynamic) / TransportConfig.BasicAuth (OpenAPI)
```

### Error Translation

Kubernetes `StatusError` objects are translated through two layers:
1. `ParseStatusError` (dynamic client layer) -> `APIError` with formatted code/reason/message
2. `ErrorToDetailedError` (CLI layer) -> `DetailedError` with summary, details, suggestions

---

## 6. CLI Conventions

### Command Structure

```
gcx
  +-- config             (--config, --context as persistent flags)
  |     +-- check, current-context, list-contexts, set, unset, use-context, view
  +-- resources          (--config, --context as persistent flags)
  |     +-- get, schemas, pull, push, delete, edit, validate
  +-- datasources        (--config, --context as persistent flags)
  |     +-- get, list, prometheus, loki, pyroscope, tempo, generic
  |     (each kind subgroup exposes its own `query` subcommand)
  +-- providers
  |     (single command: list registered providers)
  +-- dev
        (import, scaffold, generate, lint, serve subcommands for code scaffolding/dev workflows)
```

### The Options Pattern

Every command follows a consistent structure:

```
1. opts struct          -- all state for the command
2. setup(flags)         -- bind CLI flags to struct fields
3. Validate()           -- check constraints BEFORE any I/O
4. constructor(configOpts) -> *cobra.Command  -- wire RunE closure
```

`config.Options` (holding `--config` and `--context`) is created once per command
group and injected into every subcommand constructor by pointer. Subcommands call
`configOpts.LoadGrafanaConfig(ctx)` at execution time (in `RunE`), not at construction
time, ensuring flags are already parsed.

### Shared Helpers

- **`fetchResources`**: centralizes the Grafana fetch + filter + process flow for
  `get`, `edit`, and `delete` commands
- **`OnErrorMode`**: shared `--on-error` flag with `ignore`/`fail`/`abort` semantics
- **`io.Options`**: shared `--output/-o` flag with pluggable codec registration

### Adding a New Command

1. Create `cmd/gcx/resources/mycommand.go` following the options pattern
2. Register in `resources/command.go` with `cmd.AddCommand(myCmd(configOpts))`
3. No other wiring needed -- error handling, config loading, and logging are automatic

---

## 7. Concurrency Model

| Operation | Mechanism | Limit | Configurable? |
|-----------|-----------|-------|---------------|
| File reads (FSReader) | errgroup + SetLimit | MaxConcurrentReads | Yes (--max-concurrent) |
| Pull API fetches | errgroup (one per filter) | = number of filters | No |
| Push (folders) | ForEachConcurrently per level | MaxConcurrency | Yes (--max-concurrent) |
| Push (non-folders) | ForEachConcurrently | MaxConcurrency | Yes (--max-concurrent) |
| Delete | ForEachConcurrently | MaxConcurrency | Yes (--max-concurrent) |
| `NamespacedClient.GetMultiple` | errgroup (no SetLimit) | Unbounded (QPS/Burst only) | No |
| `ResourceClientRouter.GetMultiple` (adapter path) | errgroup + SetLimit(10) | 10 | No |
| HTTP rate limiting | k8s token bucket | QPS=50, Burst=100 | No (hardcoded) |

Default `MaxConcurrency` is 10 for all operations.

Error propagation: `StopOnError=true` cancels the errgroup context on first error.
`StopOnError=false` records failures in `OperationSummary` and continues processing.

---

## 8. Build and Development

### Toolchain

Devbox pins exact tool versions (`go@1.26`, `golangci-lint@2.9`, `goreleaser@2.13.3`,
`python@3.12.12`). The Makefile uses a `$(RUN_DEVBOX)` prefix pattern so all
commands work identically inside and outside `devbox shell`.

### Key Makefile Targets

| Target | Purpose |
|--------|---------|
| `make all` | Full gate: lint + tests + build + docs |
| `make build` | Compile to `bin/gcx` with version injection |
| `make tests` | Run all unit tests |
| `make lint` | golangci-lint with project config |
| `make docs` | Generate reference docs + build mkdocs site |
| `make reference-drift` | Fail if generated docs are stale |
| `make test-env-up` | Start Grafana 12 + MySQL 9 via docker-compose |

### CI/CD

Three GitHub Actions workflows:
- **ci.yaml**: PR/push gate -- lint, tests, doc drift check (parallel jobs)
- **release.yaml**: Tag-triggered -- goreleaser cross-platform builds + GitHub Pages docs
- **publish-docs.yaml**: Manual doc deployment without a release

### Code Generation

Three standalone Go programs under `scripts/` generate reference documentation
from Cobra command trees and config struct reflection. Generated docs are committed
and checked for drift in CI.

---

## 9. Strengths

1. **Principled architecture.** Clean layered design with strict separation of
   concerns. CLI holds no business logic. Internal packages are organized by
   feature, not by technical layer.

2. **Kubernetes ecosystem leverage.** Using k8s client-go directly avoids
   reimplementing discovery, pagination, dry-run, error handling, and unstructured
   object representation. Dynamic resource types mean gcx stays compatible
   as Grafana adds new resource kinds.

3. **Consistent command patterns.** The options pattern, shared helpers, and
   error handling chain make it straightforward to add new commands. A newcomer
   can follow the pattern mechanically.

4. **Configuration ergonomics.** Context-based multi-environment support, env var
   overrides, auto-discovery of cloud namespace, and reflection-driven config
   editing create a polished user experience.

5. **Composable processor pipeline.** The Processor interface cleanly separates
   resource transformation from I/O, making it easy to add new transformations
   without touching pipeline code.

6. **Reproducible builds.** Vendored dependencies, devbox, and CI caching ensure
   identical builds across environments.

7. **Serve command.** The local development server with live reload, reverse proxy,
   and dashboard interception is a genuinely differentiating feature for
   dashboards-as-code workflows.

---

## 10. Concerns and Technical Debt

### High Priority

1. **No automated integration tests.** A docker-compose environment exists but is
   only used for manual testing. The most impactful quality investment would be
   adding integration tests for push/pull/delete/serve workflows.

2. **Test coverage at ~40-50%.** Unit tests focus on parsing and filtering logic.
   Critical paths like push upsert, pull processing, error scenarios, and
   concurrency edge cases are undertested.

3. **Resource versioning in updates.** `pusher.go` copies `resourceVersion` from
   the existing object before Update, but there is no conflict detection or retry
   logic. Concurrent updates to the same resource could produce unexpected results.

### Medium Priority

4. **DiscoverStackID called twice.** During config validation and again during
   REST config construction. No caching between calls means two network round-trips
   to `/bootdata` on every command.

5. **Manager kind placeholder.** `ResourceManagerKind` uses `utils.ManagerKindKubectl`
   (a kubectl constant) as a placeholder. Should be changed to a gcx-specific
   value.

6. **Hardcoded rate limits.** QPS=50 and Burst=100 are not configurable. This
   could be limiting for large deployments or too aggressive for rate-limited
   environments.

7. **GetMultiple concurrency unbounded.** `NamespacedClient.GetMultiple` runs all
   Gets concurrently without `SetLimit`. For large resource lists, this could
   overwhelm the HTTP transport despite QPS limiting.

8. **CI drift check incomplete.** Only CLI reference drift is checked in CI; env-var
   and config reference drift checks exist in the Makefile but may not be wired
   into the CI workflow.

### Low Priority

9. **UserAgent not applied to dynamic client.** `httputils.UserAgent` is defined
   but not set on the k8s REST config (noted as TODO).

10. **httputils naming confusion.** This package is used by the serve command's
    reverse proxy, not by the primary API client. The name could mislead newcomers
    into thinking it is part of the main client chain.

11. **Three-way merge not implemented.** Push uses simple Get-then-Create/Update
    upsert. Proper server-side apply with field manager semantics (like kubectl)
    would prevent conflicts in multi-tool scenarios.

---

## 11. Critical Files Reference

Files most important for understanding the codebase. Organized by architectural layer.

### Entry Points and Wiring

| File | Purpose |
|------|---------|
| `cmd/gcx/main.go` | Binary entry point, error handling, version formatting |
| `cmd/gcx/root/command.go` | Root Cobra command, logging setup, PersistentPreRun |
| `cmd/gcx/resources/command.go` | Resources command group, configOpts injection |
| `cmd/gcx/config/command.go` | Config commands + Options.LoadConfig/LoadGrafanaConfig |

### Core Resource Abstractions

| File | Purpose |
|------|---------|
| `internal/resources/resources.go` | Resource, Resources, SourceInfo, ResourceRef types |
| `internal/resources/descriptor.go` | Descriptor type (fully-qualified resource identity) |
| `internal/resources/selector.go` | Selector, PartialGVK, ParseSelectors |
| `internal/resources/filter.go` | Filter, Filters, FilterType |

### Discovery and Resolution

| File | Purpose |
|------|---------|
| `internal/resources/discovery/registry.go` | Registry, MakeFilters, FilterDiscoveryResults |
| `internal/resources/discovery/registry_index.go` | RegistryIndex, GVK lookup/resolution logic |

### Remote Operations

| File | Purpose |
|------|---------|
| `internal/resources/remote/pusher.go` | Pusher, PushClient interface, upsert logic |
| `internal/resources/remote/puller.go` | Puller, PullClient interface, concurrent fetch |
| `internal/resources/remote/deleter.go` | Deleter, concurrent delete |
| `internal/resources/remote/remote.go` | Processor interface definition |
| `internal/resources/remote/folder_hierarchy.go` | SortFoldersByDependency (topological sort) |
| `internal/resources/remote/summary.go` | OperationSummary (thread-safe result tracking) |

### Adapter Layer

| File | Purpose |
|------|---------|
| `internal/resources/adapter/adapter.go` | `ResourceAdapter` interface and `Factory` type |
| `internal/resources/adapter/register.go` | Global adapter registration — `Register()`, `AllRegistrations()` |
| `internal/resources/adapter/router.go` | `ResourceClientRouter` — GVK-based routing to adapter or dynamic client |

### Processors

| File | Purpose |
|------|---------|
| `internal/resources/process/namespace.go` | NamespaceOverrider (push) |
| `internal/resources/process/managerfields.go` | ManagerFieldsAppender (push) |
| `internal/resources/process/serverfields.go` | ServerFieldsStripper (pull) |

### Local I/O

| File | Purpose |
|------|---------|
| `internal/resources/local/reader.go` | FSReader (3-goroutine concurrent file reader) |
| `internal/resources/local/writer.go` | FSWriter (sequential file writer) |
| `internal/format/codec.go` | JSON/YAML codecs, format detection |

### Configuration

| File | Purpose |
|------|---------|
| `internal/config/types.go` | Config, Context, GrafanaConfig, TLS struct definitions |
| `internal/config/loader.go` | Load, Write, StandardLocation, file path resolution |
| `internal/config/rest.go` | NewNamespacedRESTConfig (config -> k8s REST config bridge) |
| `internal/config/stack_id.go` | DiscoverStackID (Grafana Cloud auto-discovery) |
| `internal/config/editor.go` | SetValue/UnsetValue (reflection-based config editing) |
| `internal/config/errors.go` | ValidationError, UnmarshalError, ContextNotFound |
| `internal/secrets/redactor.go` | Reflection-based secret redaction |

### Cloud Integration

| File | Purpose |
|------|---------|
| `internal/cloud/gcom.go` | GCOMClient for Grafana Cloud stack discovery via GCOM API |

### Agent Mode

| File | Purpose |
|------|---------|
| `internal/agent/agent.go` | `IsAgentMode()`, `SetFlag()` — env-var detection at init time |
| `internal/terminal/terminal.go` | `Detect()`, `IsPiped()`, `NoTruncate()` — TTY/pipe state for output suppression |

### Client Layer

| File | Purpose |
|------|---------|
| `internal/resources/dynamic/namespaced_client.go` | Primary CRUD client (k8s dynamic) |
| `internal/resources/dynamic/versioned_client.go` | Version-aware client for pull |
| `internal/resources/dynamic/errors.go` | ParseStatusError, APIError |
| `internal/grafana/client.go` | OpenAPI client factory for /api operations |

### Serve Command

| File | Purpose |
|------|---------|
| `internal/server/server.go` | Server.Start, Chi router setup, reverse proxy |
| `internal/server/handlers/` | HTTP handlers for dashboard/folder interception |
| `internal/server/livereload/` | WebSocket live reload hub and protocol |
| `internal/server/watch/` | fsnotify file watcher integration |

### Error Handling

| File | Purpose |
|------|---------|
| `cmd/gcx/fail/detailed.go` | DetailedError type (rich error rendering) |
| `cmd/gcx/fail/convert.go` | ErrorToDetailedError (error type dispatch) |

### Provider System

| File | Purpose |
|------|---------|
| `internal/providers/provider.go` | `Provider` interface (incl. TypedRegistrations()), `ConfigKey` metadata type |
| `internal/providers/registry.go` | `All()` — compile-time provider registry |
| `internal/providers/redact.go` | `RedactSecrets()` — secure-by-default secret redaction |
| `cmd/gcx/providers/command.go` | `providers` command (list registered providers) |
| `internal/providers/configloader.go` | Shared `ConfigLoader` — binds `--config`/`--context` flags and loads REST config for all providers |

### Adaptive Telemetry Provider

| File | Purpose |
|------|---------|
| `internal/providers/adaptive/provider.go` | `AdaptiveProvider` implementing the `providers.Provider` interface |
| `internal/providers/adaptive/auth/` | Shared Basic auth helper and GCOM caching |
| `internal/providers/adaptive/metrics/` | Metrics rules and recommendations (provider-only, read-only) |
| `internal/providers/adaptive/logs/` | Logs patterns (provider-only, read-only) and exemptions (TypedCRUD adapter) |
| `internal/providers/adaptive/traces/` | Traces recommendations (provider-only, read-only) and policies (TypedCRUD adapter) |

### App Observability Provider

| File | Purpose |
|------|---------|
| `internal/providers/appo11y/provider.go` | `AppO11yProvider` implementing the `providers.Provider` interface |
| `internal/providers/appo11y/client.go` | Plugin proxy HTTP client (shared by both subpackages for testing) |
| `internal/providers/appo11y/overrides/` | Overrides (MetricsGeneratorConfig) — singleton TypedCRUD with ETag concurrency |
| `internal/providers/appo11y/settings/` | Settings (PluginSettings) — singleton TypedCRUD without ETag |

### Alert Provider

| File | Purpose |
|------|---------|
| `internal/providers/alert/provider.go` | `AlertProvider` implementing the `providers.Provider` interface |
| `internal/providers/alert/rules/` | Alert rules management (read-only via the Prometheus-compatible alerting API) |
| `internal/providers/alert/groups/` | Alert groups management |

### SLO Provider

| File | Purpose |
|------|---------|
| `internal/providers/slo/provider.go` | `SLOProvider` implementing the `providers.Provider` interface |
| `internal/providers/slo/definitions/` | SLO definitions management (status, metrics via PromQL) |
| `internal/providers/slo/reports/` | SLO reports management |

### Synthetic Monitoring Provider

| File | Purpose |
|------|---------|
| `internal/providers/synth/provider.go` | `SynthProvider` implementing the `providers.Provider` interface |
| `internal/providers/synth/checks/` | Check management (list, get, push, pull, delete, status, timeline) |
| `internal/providers/synth/probes/` | Probe listing and management |

### Fleet Management Provider

| File | Purpose |
|------|---------|
| `internal/providers/fleet/provider.go` | `FleetProvider` implementing the `providers.Provider` interface |
| `internal/providers/fleet/client.go` | Fleet Management REST client |

### K6 Cloud Provider

| File | Purpose |
|------|---------|
| `internal/providers/k6/provider.go` | `K6Provider` implementing the `providers.Provider` interface |
| `internal/providers/k6/client.go` | K6 Cloud REST client (token exchange auth, projects, tests, runs, envvars) |
| `internal/providers/k6/commands.go` | K6 CLI commands (projects, tests, runs, envvars, token) |
| `internal/providers/k6/resource_adapter.go` | Resource adapter for k6 projects |

### IRM Incidents Provider

| File | Purpose |
|------|---------|
| `internal/providers/incidents/provider.go` | `IncidentsProvider` implementing the `providers.Provider` interface |
| `internal/providers/incidents/commands.go` | IRM Incidents CLI commands |
| `internal/providers/incidents/resource_adapter.go` | Resource adapter for incidents |

### OnCall Provider

| File | Purpose |
|------|---------|
| `internal/providers/oncall/provider.go` | `OnCallProvider` implementing the `providers.Provider` interface |
| `internal/providers/oncall/client.go` | OnCall REST client |
| `internal/providers/oncall/commands.go` | OnCall CLI commands (schedules, integrations, escalation chains) |
| `internal/providers/oncall/resource_adapter.go` | Resource adapter for OnCall resources |

### Knowledge Graph (Asserts) Provider

| File | Purpose |
|------|---------|
| `internal/providers/kg/provider.go` | `KGProvider` implementing the `providers.Provider` interface |
| `internal/providers/kg/client.go` | Knowledge Graph REST client |
| `internal/providers/kg/commands.go` | KG CLI commands |
| `internal/providers/kg/resource_adapter.go` | Resource adapter for KG resources |

### Linter System

| File | Purpose |
|------|---------|
| `internal/linter/linter.go` | Linter engine — rule execution, report aggregation |
| `internal/linter/rules.go` | Rule interface and rule management |
| `internal/linter/report.go` | Report and Violation types for linting results |
| `internal/linter/reporter.go` | Reporter — formats and outputs linting results |
| `internal/linter/tests.go` | Test runner for `.rego` test files |
| `internal/linter/bundle/` | Embedded Rego bundle with built-in linting rules |
| `internal/linter/builtins/` | Built-in rule validators (PromQL, LogQL) |
| `cmd/gcx/linter/command.go` | `dev lint` subgroup (run, new, rules, test subcommands; formerly top-level `linter`) |
| `scripts/linter-rules-reference/` | Code generator for linter rule reference documentation |

### Dev Command

| File | Purpose |
|------|---------|
| `cmd/gcx/dev/command.go` | `dev` command group (import, scaffold, generate, lint, serve subcommands) |

### Datasource Query Clients

| File | Purpose |
|------|---------|
| `internal/query/prometheus/client.go` | Prometheus query client (Query, Labels, LabelValues, Metadata, Targets) |
| `internal/query/prometheus/types.go` | Request/response types for Prometheus |
| `internal/query/prometheus/formatter.go` | Table/text formatting for Prometheus responses |
| `internal/query/loki/client.go` | Loki query client (Query, Labels, LabelValues, Series) |
| `internal/query/loki/types.go` | Request/response types for Loki |
| `internal/query/loki/formatter.go` | Table/text formatting for Loki responses |
| `cmd/gcx/datasources/command.go` | `datasources` command group (list, get, prometheus, loki, pyroscope, tempo, generic subcommands) |
| `cmd/gcx/datasources/query/` | Per-kind `query` subcommand constructors and shared infrastructure (codecs, time parsing) |

### Dashboard Image Renderer

| File | Purpose |
|------|---------|
| `internal/dashboards/renderer.go` | HTTP client for Grafana Image Renderer API (`/render/d/`, `/render/d-solo/`) |
| `internal/dashboards/types.go` | `SnapshotResult` struct for JSON/table output |
| `cmd/gcx/dashboards/command.go` | `dashboards` command group |
| `cmd/gcx/dashboards/snapshot.go` | `dashboards snapshot` — renders PNG images with kiosk mode, template variable overrides |

### Terminal Chart Rendering

| File | Purpose |
|------|---------|
| `internal/graph/chart.go` | `RenderChart`, `RenderLineChart`, `RenderBarChart` — auto-selects chart type |
| `internal/graph/types.go` | `ChartData`, `Series`, `Point` types |
| `internal/graph/colors.go` | Color palette for multi-series charts |
| `internal/graph/convert.go` | Conversion helpers from query responses to `ChartData` |

### Test Utilities

| File | Purpose |
|------|---------|
| `internal/testutils/command.go` | Cobra command test helpers |
| `internal/testutils/fs.go` | Filesystem test helpers |

### Build and Tooling

| File | Purpose |
|------|---------|
| `Makefile` | Build, test, lint, docs, integration env orchestration |
| `devbox.json` | Reproducible toolchain pins |
| `.golangci.yaml` | Linter configuration (opt-out model) |
| `.goreleaser.yaml` | Cross-platform release builds |
| `docker-compose.yml` | Grafana 12 + MySQL 9 integration test env |

---

## 12. Key Invariants for Code Modification

These invariants are enforced by convention. Violating them will cause subtle bugs.

1. **Folder ordering is mandatory.** Push must create folders before resources that
   reference them. The two-phase approach (folders level-by-level, then non-folders)
   must be preserved.

2. **FSReader deduplicates by {GVK, name}.** First-seen wins. Code that creates
   Resources outside FSReader must set `SourceInfo` if round-tripping is needed.

3. **ServerFieldsStripper rebuilds the entire object.** It is not a patch -- it
   constructs a new object with only `{apiVersion, kind, metadata, spec}`. Fields
   outside those will be lost.

4. **resourceVersion must be copied before Update.** `upsertResource` reads the
   existing resourceVersion via Get before calling Update. Skipping this causes
   API rejection.

5. **OperationSummary is not an error.** Failures in the summary do not cause
   Push/Pull/Delete to return an error (unless StopOnError=true). Callers must
   check `summary.FailedCount()` separately.

6. **opts.Validate() must be the first call in RunE.** No I/O before validation.

7. **configOpts.LoadGrafanaConfig is called in RunE, not at construction time.**
   Flags are not yet parsed when the command is constructed.

---

## 13. Recommendations for Newcomers

### Getting Started

1. Run `devbox shell` to get the full toolchain
2. Run `make build` to verify the build works
3. Read `cmd/gcx/main.go` to see the entry point
4. Read `cmd/gcx/resources/push.go` as the canonical command example
5. Read `internal/resources/resources.go` to understand the central data type
6. Read `internal/resources/discovery/registry.go` to understand how user input
   resolves to API calls

### Understanding a Request Flow

Trace `gcx resources push dashboards/my-dash -p ./resources`:

```
main.go             -> root.Command().Execute()
root/command.go     -> PersistentPreRun: configure logging
resources/push.go   -> RunE:
                       1. Validate flags
                       2. Load config + build REST client
                       3. Parse "dashboards/my-dash" into Selector
                       4. Discover API resources from Grafana
                       5. Resolve Selector to Filter
                       6. FSReader reads ./resources (concurrent)
                       7. Pusher pushes matched resources:
                          - NamespaceOverrider rewrites namespace
                          - ManagerFieldsAppender stamps ownership
                          - Folders first (by level), then other resources
                          - Per resource: Get -> Create or Update
                       8. Print summary
```

### Common Tasks

**Adding a new resource command:** Follow the options pattern in any existing command
(e.g., `push.go`). Create opts struct, setup/validate/constructor, register in
`command.go`.

**Adding a new processor:** Implement `Processor.Process(*Resource) error`, then
add it to the processor slice in the relevant command's wiring (push.go or pull.go).

**Adding a new config field:** Add the struct field in `types.go` with `yaml`,
`env`, and optionally `datapolicy:"secret"` tags. The reflection-based editor,
env parser, and redactor pick it up automatically.

**Running locally against a test Grafana:** `make test-env-up` starts Grafana 12
+ MySQL 9. Use `--config testdata/integration-test-config.yaml` to point
gcx at it.

---

## 14. Areas of Uncertainty

**Sections with Lower Confidence:**

- **Testing strategy** (70%): No dedicated analysis of test files. Coverage
  estimated from cross-references in other domains. Test patterns (table-driven,
  testdata fixtures) noted but not deeply validated.

- **Retry and timeout behavior** (limited): The k8s client-go transport handles
  retries internally, but the analysis did not trace specific retry policies,
  timeout configurations, or backoff behavior.

- **Production deployment patterns**: The codebase is a CLI tool, so "deployment"
  means distributing binaries. How users actually integrate it into CI/CD
  pipelines, GitOps workflows, or automated systems is outside the code analysis.

**Recommended for Manual Review:**

- `internal/resources/remote/pusher.go`: The upsert logic and its interaction
  with resourceVersion conflicts deserves careful review if concurrent pushes
  are a concern.

- `internal/server/`: The serve command has significant complexity (reverse proxy,
  WebSocket, file watching, dashboard interception) that warrants deeper review
  for correctness in edge cases.

---

## 15. Synthesis Notes

### Analysis Coverage

- Domains analyzed: 6 (project structure, resource model, CLI layer, client/API, config, data flows)
- Patterns identified: 10 (see patterns.md)
- Contradictions resolved: 5 (see patterns.md)
- Key files referenced: 35+

### Confidence Rationale

Overall confidence of 92% (High) is based on:
- Six parallel analyzers covered all major code paths and architectural layers
- Strong agreement between domains on core patterns and design decisions
- Five minor contradictions resolved with evidence from multiple sources
- Clear documentation in the codebase (CLAUDE.md, inline comments, code structure)
- Primary gap: no dedicated test analysis; testing strategy reconstructed from
  cross-references

### Analysis Limitations

- **Runtime behavior not analyzed.** Static code analysis only; actual API responses,
  error rates, and performance characteristics are not measured.
- **Git history not analyzed.** Architectural evolution and decision rationale
  require examining commit history and PR discussions.
- **External service integrations not fully traced.** The Grafana API contract is
  assumed from the k8s-compatible endpoint pattern; actual API behavior may differ.
- **Serve command edge cases.** The reverse proxy, dashboard interception, and
  live reload have complex interaction patterns that may have subtle issues not
  visible in static analysis.

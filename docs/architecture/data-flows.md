# Resource Processing Pipelines

Domain: Data flows — push, pull, delete, and serve pipelines in gcx.

---

## 1. Overview

gcx has five primary data flow pipelines:

```
PUSH:   Local disk → FSReader → filter → process → Pusher → Grafana API
PULL:   Grafana API → Puller → process → FSWriter → Local disk
DELETE: Local disk → FSReader → filter → Deleter → Grafana API
SERVE:  Local disk → watch → FSReader → HTTP proxy → live reload → Browser
QUERY:  Flags → query client → Grafana datasource API → parse → render
```

The first four share the same `Resource`/`Resources` abstraction as the central in-memory
representation. The `Processor` interface (`remote/remote.go:11`) provides a composable
transformation stage in push and pull. The QUERY pipeline is independent — it operates
on time series data and does not use the resource model.

---

## 2. PUSH Pipeline

Entry point: `cmd/gcx/resources/push.go:95` (`RunE` closure in `pushCmd`).

```
User invocation:
  gcx resources push dashboards/foo

  ┌──────────────────────────────────────────────────────────────────────┐
  │ 1. Parse selectors                                                    │
  │    resources.ParseSelectors(args) → []Selector                       │
  │    e.g. "dashboards/foo" → {kind:"dashboards", uid:"foo"}            │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 2. Resolve selectors → Filters (via Discovery Registry)              │
  │    discovery.NewDefaultRegistry(ctx, cfg)                            │
  │    reg.MakeFilters(selectors) → resources.Filters                    │
  │    Maps partial selector to fully-qualified GVK + filter type         │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 3. Read local files (FSReader)                                        │
  │    local.FSReader{Decoders: format.Codecs(), MaxConcurrentReads: N}  │
  │    reader.Read(ctx, resourcesList, filters, paths)                   │
  │                                                                       │
  │    Internal goroutine pipeline (3 concurrent goroutines):            │
  │                                                                       │
  │    [goroutine 1: path walker]                                         │
  │       filepath.WalkDir → emit file paths to pathCh channel           │
  │       Files sent individually; directories recursively walked        │
  │                                                                       │
  │    [goroutine 2: file readers (errgroup with SetLimit)]               │
  │       For each path from pathCh:                                      │
  │         - Detect format from file extension (.json / .yaml / .yml)   │
  │         - Decode file → unstructured.Unstructured                    │
  │         - Check if matches filters (skip if not)                     │
  │         - Send readResult{Object, Path} to resCh                     │
  │                                                                       │
  │    [goroutine 3: deduplication collector]                             │
  │       For each result from resCh:                                     │
  │         - Check idx map[{gvk,name}] for duplicates                   │
  │         - Add to *resources.Resources collection                      │
  │         - Each Resource has SourceInfo{Path, Format} attached        │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 4. Push via Pusher                                                    │
  │    remote.NewDefaultPusher(ctx, cfg) → Pusher                        │
  │    (internally builds ResourceClientRouter: adapter path for          │
  │     provider-backed GVKs, k8s dynamic client for all others)         │
  │    pusher.Push(ctx, PushRequest{...})                                 │
  │                                                                       │
  │    Processors applied (in order) per resource:                        │
  │      a. NamespaceOverrider — override namespace with target context  │
  │      b. ManagerFieldsAppender — set manager/source annotations       │
  │         (skipped with --omit-manager-fields)                         │
  │                                                                       │
  │    Two-phase push:                                                    │
  │                                                                       │
  │    PHASE 1: Folders (ordered by hierarchy)                           │
  │      SortFoldersByDependency(folders) → [][]*Resource (levels)       │
  │      For each level (sequential between levels):                     │
  │        levelResources.ForEachConcurrently(ctx, N, pushSingleResource)│
  │                                                                       │
  │    PHASE 2: Non-folder resources                                      │
  │      request.Resources.ForEachConcurrently(ctx, N, pushSingleResource│
  │        skip folders — they were already handled in phase 1           │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 5. Per-resource push logic (pushSingleResource)                       │
  │                                                                       │
  │    a. Validate GVK is in supported descriptors (from registry)       │
  │    b. Run each Processor.Process(res)                                │
  │    c. Check res.IsManaged() — skip if managed by another tool        │
  │       (unless IncludeManaged=true)                                   │
  │    d. upsertResource:                                                 │
  │         client.Get(ctx, desc, name) — check if exists                │
  │         if exists: copy resourceVersion, client.Update(...)          │
  │         if 404:    client.Create(...)                                 │
  │         DryRun: pass DryRun: []string{"All"} to K8s options          │
  │    e. summary.RecordSuccess() or summary.RecordFailure(res, err)     │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 6. Report summary to user                                             │
  │    "%d resources pushed, %d errors"                                  │
  │    Exit non-zero if --on-error=fail and failures > 0                 │
  └──────────────────────────────────────────────────────────────────────┘
```

Key files:
- `cmd/gcx/resources/push.go` — CLI wiring
- `internal/resources/local/reader.go` — FSReader
- `internal/resources/remote/pusher.go` — Pusher, upsertResource
- `internal/resources/process/managerfields.go` — ManagerFieldsAppender
- `internal/resources/process/namespace.go` — NamespaceOverrider
- `internal/resources/adapter/router.go` — ResourceClientRouter (routes CRUD to adapter or dynamic client)

---

## 3. PULL Pipeline

Entry point: `cmd/gcx/resources/pull.go` (mirrors push structure).

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │ 1. Parse selectors + resolve to Filters (same as push)               │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 2. Pull via Puller                                                    │
  │    remote.NewDefaultPuller(ctx, cfg) → Puller                        │
  │    (uses ResourceClientRouter — adapter path for                      │
  │     provider-backed GVKs)                                             │
  │    Uses VersionedClient (not NamespacedClient) for preferred versions│
  │                                                                       │
  │    If no filters: expand to ALL preferred resources                   │
  │      registry.PreferredResources() → one FilterTypeAll per resource  │
  │                                                                       │
  │    Concurrent fetch (errgroup, one goroutine per filter):            │
  │      partialRes := make([][]unstructured.Unstructured, len(filters)) │
  │                                                                       │
  │      FilterTypeAll    → client.List(ctx, desc, ListOptions{})        │
  │      FilterTypeMultiple → client.GetMultiple(ctx, desc, names, ...)  │
  │      FilterTypeSingle   → client.Get(ctx, desc, name, ...)           │
  │                                                                       │
  │    404 / 405 responses: silently skipped (not counted as errors).    │
  │    Some resource types discovered via the API may not support List   │
  │    or Get; the pull pipeline ignores them rather than failing.       │
  │                                                                       │
  │    errg.Wait() — collect all results                                  │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 3. Post-fetch processing (sequential, after all fetches complete)     │
  │                                                                       │
  │    For each fetched unstructured item:                                │
  │      resources.FromUnstructured(&item) → *Resource                   │
  │      if ExcludeManaged && !res.IsManaged(): skip                     │
  │      Apply Processors in order:                                       │
  │        ServerFieldsStripper — remove server-only annotations:        │
  │          AnnoKeyCreatedBy, AnnoKeyUpdatedBy, AnnoKeyUpdatedTimestamp  │
  │          Manager annotations (if managed by gcx)               │
  │          Source path/checksum/timestamp annotations                   │
  │          LabelKeyDeprecatedInternalID                                 │
  │          Rebuilds clean object: {apiVersion, kind, metadata, spec}   │
  │      req.Resources.Add(res)                                           │
  │      summary.RecordSuccess()                                          │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 4. Write to disk (FSWriter)                                           │
  │    local.FSWriter{Path, Namer, Encoder}                              │
  │    Namer strategy: GroupResourcesByKind(ext)                         │
  │      → {Kind}.{Version}.{Group}/{Name}.{ext}                         │
  │        e.g. Dashboard.v1alpha1.dashboard.grafana.app/my-dash.yaml    │
  │                                                                       │
  │    Sequential write (no concurrency in FSWriter):                    │
  │      For each resource: writer.writeSingle(resource)                 │
  │        Namer(resource) → relative path                               │
  │        ensureDirectoryExists(dir)                                    │
  │        Encoder.Encode(file, resource.ToUnstructured())               │
  └──────────────────────────────────────────────────────────────────────┘
```

Key files:
- `internal/resources/remote/puller.go` — Puller, concurrent fetch
- `internal/resources/process/serverfields.go` — ServerFieldsStripper
- `internal/resources/local/writer.go` — FSWriter
- `internal/resources/adapter/router.go` — ResourceClientRouter (routes CRUD to adapter or dynamic client)

---

## 4. DELETE Pipeline

Simpler than push/pull — no local file reading phase (resources are parsed from
CLI args or read from disk by the caller before passing to `Deleter`).

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │ 1. Build supported descriptor map from registry                       │
  │    deleter.supportedDescriptors() → map[GVK]Descriptor               │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 2. Concurrent delete                                                  │
  │    request.Resources.ForEachConcurrently(ctx, MaxConcurrency, ...)   │
  │                                                                       │
  │    Per resource:                                                      │
  │      a. Look up GVK in supported map; skip/error if not found        │
  │      b. deleter.deleteResource(ctx, desc, res, dryRun)               │
  │           client.Delete(ctx, desc, res.Name(), DeleteOptions{        │
  │             DryRun: ["All"] if dryRun,                               │
  │           })                                                          │
  │      c. summary.RecordSuccess() or summary.RecordFailure(res, err)   │
  │                                                                       │
  │    NOTE: No manager check — deleter does NOT verify IsManaged().     │
  │    Callers are expected to filter the resource list before deleting.  │
  └──────────────────────────────────────────────────────────────────────┘
```

Key files:
- `internal/resources/remote/deleter.go` — Deleter, delete operations
- `internal/resources/adapter/router.go` — ResourceClientRouter (routes CRUD to adapter or dynamic client)

Difference from push: Deleter does NOT check `res.IsManaged()`. It trusts the caller
to have already resolved which resources should be deleted. The `NewDeleter` constructor
builds a `ResourceClientRouter` to route delete calls to provider adapters or the k8s
dynamic client depending on resource type.

---

## 4b. Provider-Backed Resource Routing

For resource types backed by provider REST APIs (SLO, Synthetic Monitoring, Alert),
the Pusher/Puller/Deleter's underlying client is a `ResourceClientRouter`. The router
transparently routes each CRUD call:

```
Client call (Get/List/Create/Update/Delete)
      |
      v  ResourceClientRouter.getAdapter(ctx, gvk)
      |
 GVK registered?
      |
 YES  |  NO
  ↓       ↓
ResourceAdapter    k8s DynamicClient
(provider REST)    (/apis endpoint)
```

Adapters are lazily initialized (factory called once, result cached). For read-only
provider types (Alert rules/groups), Create/Update/Delete return `errors.ErrUnsupported`.

The `--context` flag (Grafana config context name) is threaded into adapter
factories via `context.Context` using `config.ContextWithName` / `config.ContextNameFromCtx`
helpers (`internal/config/context.go`). This lets adapter factories look up the
correct provider config for the selected context without requiring a separate
parameter.

This routing is transparent to processors, selectors, and the CLI layer — they
interact with the same Pusher/Puller/Deleter interface regardless of whether the
backing client is a REST adapter or the k8s dynamic client.

---

## 5. QUERY Pipeline

Entry point: `cmd/gcx/datasources/query/` package — per-kind constructors wired under each kind's subgroup (`datasources prometheus query`, `datasources loki query`, etc.).

```
User invocation:
  gcx datasources prometheus query <uid> 'rate(http_requests_total[5m])' --from now-1h --to now --step 1m

  ┌──────────────────────────────────────────────────────────────────────┐
  │ 1. Parse args and flags                                               │
  │    [DATASOURCE_UID] EXPR   positional args (UID optional for typed   │
  │                            subcommands when config default is set)   │
  │    --from / --to    time bounds (RFC3339, Unix epoch, or relative    │
  │                     e.g. "now-1h", "now")                            │
  │    --since          convenience: sets --from=now-{since} --to=now    │
  │                     (mutually exclusive with --from/--to)            │
  │    --step           query step / interval (e.g. "15s", "1m")         │
  │    --limit          max log lines returned (loki and generic only;   │
  │                     default 1000; 0 = no limit)                      │
  │    --profile-type   required for pyroscope; also on generic          │
  │    -o               output format: table (default), graph, json, yaml│
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 2. Resolve datasource UID                                             │
  │    Typed subcommands (prometheus/loki/pyroscope):                    │
  │      if UID positional arg provided → use directly                   │
  │      else → config.DefaultDatasourceUID(ctx, kind):                  │
  │        (1) ctx.Datasources[kind]         ← new config section        │
  │        (2) ctx.DefaultPrometheusDatasource / DefaultLokiDatasource   │
  │            / DefaultPyroscopeDatasource  ← legacy fallback           │
  │      error if still empty                                             │
  │    generic subcommand:                                               │
  │      UID positional arg required (no default resolution)             │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 3. Parse time range                                                   │
  │    ParseTime(opts.From, now) → time.Time (zero if empty)             │
  │    ParseTime(opts.To, now)   → time.Time (zero if empty)             │
  │    ParseDuration(opts.Step)  → time.Duration (zero if empty)         │
  │    --since already resolved to From/To by Validate() before RunE    │
  │                                                                       │
  │    IsRange() = From != zero && To != zero                            │
  │    Instant query: no --from/--to flags → uses "now-1m" to "now"     │
  │    Range query: explicit time bounds + optional step                 │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 4. Build query client and execute                                     │
  │                                                                       │
  │    PROMETHEUS path:                                                   │
  │      prometheus.NewClient(cfg) — wraps rest.HTTPClientFor(&cfg)      │
  │      client.Query(ctx, datasourceUID, QueryRequest{...})             │
  │                                                                       │
  │        POST /apis/query.grafana.app/v0alpha1/namespaces/{ns}/query   │
  │        Body: {                                                        │
  │          "queries": [{                                                │
  │            "refId": "A",                                             │
  │            "datasource": {"type":"prometheus","uid":<uid>},          │
  │            "expr": <PromQL>,                                          │
  │            "intervalMs": <step_ms>,                                  │
  │            "instant": true    ← only for instant queries             │
  │          }],                                                          │
  │          "from": <start_ms>,  "to": <end_ms>                         │
  │        }                                                              │
  │                                                                       │
  │    LOKI path:                                                         │
  │      loki.NewClient(cfg) — same HTTP client construction             │
  │      client.Query(ctx, datasourceUID, QueryRequest{...})             │
  │                                                                       │
  │        POST /apis/query.grafana.app/v0alpha1/namespaces/{ns}/query   │
  │        Body: same structure with "type":"loki", "maxLines":1000      │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 5. Parse Grafana datasource response                                  │
  │                                                                       │
  │    Both clients receive the same Grafana Data Frame format:          │
  │      GrafanaQueryResponse.Results["A"].Frames[]                      │
  │      Each frame: {schema: {fields: [{type,labels,...}]},             │
  │                   data:   {values: [[timestamps...],[values...]]}}   │
  │                                                                       │
  │    PROMETHEUS conversion (convertGrafanaResponse):                   │
  │      Locate time field (type=="time") and value field (type=="number")│
  │      Single value per series   → ResultType="vector", Sample.Value   │
  │      Multiple values per series → ResultType="matrix", Sample.Values │
  │      Timestamps converted: milliseconds → seconds (÷1000)           │
  │                                                                       │
  │    LOKI conversion (convertGrafanaResponse):                         │
  │      Locate time field and string/number value field                 │
  │      Labels extracted from field.Labels                              │
  │      Result: []StreamEntry{Stream: labels, Values: [][timestamp,val]}│
  │      Timestamps in nanoseconds (×1e6 from ms float)                 │
  └───────────────────────┬──────────────────────────────────────────────┘
                          │
  ┌───────────────────────▼──────────────────────────────────────────────┐
  │ 6. Render output                                                      │
  │                                                                       │
  │    -o table (default):                                                │
  │      prometheus: FormatTable → tabwriter with label columns +        │
  │        TIMESTAMP | VALUE; vector = one row per series,               │
  │        matrix = one row per data point                               │
  │      loki: FormatQueryTable → tabwriter with TIMESTAMP | LABELS |   │
  │        VALUE columns                                                 │
  │                                                                       │
  │    -o graph:                                                          │
  │      queryGraphCodec.Encode → graph.FromPrometheusResponse() or      │
  │        graph.FromLokiResponse() → *graph.ChartData                   │
  │      graph.RenderChart(w, chartData, opts):                          │
  │        IsInstantQuery() (single point per series at same time)       │
  │          → RenderBarChart (horizontal bars via ntcharts barchart)    │
  │        else                                                          │
  │          → RenderLineChart (time series via ntcharts                 │
  │             timeserieslinechart, with legend for multi-series)       │
  │      Terminal size auto-detected; falls back to text if TextOnly     │
  │                                                                       │
  │    -o json / -o yaml:                                                 │
  │      codec.Encode(w, resp) — serialize QueryResponse directly        │
  └──────────────────────────────────────────────────────────────────────┘
```

Key files:
- `cmd/gcx/datasources/query/query.go` — shared opts, `resolveTypedArgs`, `validateDatasourceType`
- `cmd/gcx/datasources/query/{prometheus,loki,pyroscope,tempo,generic}.go` — per-kind constructors (`PrometheusCmd`, `LokiCmd`, etc.)
- `cmd/gcx/datasources/query/codecs.go` — `queryTableCodec`, `queryGraphCodec` (codec registry)
- `cmd/gcx/datasources/query/time.go` — `ParseTime`, `ParseDuration` for flag parsing
- `cmd/gcx/datasources/{prometheus,loki,pyroscope,tempo,generic}.go` — kind subgroups that wire in the query constructors
- `internal/config/resolver.go` — `DefaultDatasourceUID(ctx, kind)` — shared 2-tier UID resolution
- `internal/query/prometheus/client.go` — HTTP client, request construction, response conversion
- `internal/query/prometheus/formatter.go` — table rendering (vector/matrix/scalar)
- `internal/query/loki/client.go` — HTTP client, request construction, response conversion
- `internal/query/loki/formatter.go` — log table rendering
- `internal/graph/chart.go` — `RenderChart`, `RenderBarChart`, `RenderLineChart`
- `internal/graph/convert.go` — `FromPrometheusResponse`, `FromLokiResponse`
- `internal/graph/types.go` — `ChartData`, `Series`, `Point`

### Instant vs Range Query

Both Prometheus and Loki clients auto-detect the query mode from `QueryRequest.IsRange()`:

```
IsRange() == false (no --start/--end):
  → "instant" mode: from="now-1m", to="now", query["instant"]=true
  → Prometheus: ResultType="vector" (one value per series)
  → Graph output: RenderBarChart (horizontal bars)

IsRange() == true (--start and --end provided):
  → "range" mode: from/to as Unix milliseconds
  → Prometheus: ResultType="matrix" ([]values per series)
  → Graph output: RenderLineChart (time series line chart)
```

### API Endpoint

Both Prometheus and Loki queries go through the same unified endpoint:

```
POST /apis/query.grafana.app/v0alpha1/namespaces/{namespace}/query
```

The datasource type is identified by the `datasource.type` field in the query body
(`"prometheus"` or `"loki"`), not by the URL path. Grafana routes the request to the
appropriate datasource plugin internally.

---

## 5b. PROVIDER QUERY Pipeline

Provider subcommands (`slo definitions status`, `slo reports status`, `synth checks status`, `synth checks timeline`) implement a "fetch + enrich + render" pattern distinct from the interactive `query` command:

1. **Fetch domain objects** — from the provider REST API (SLO definitions via k8s `/apis`, SM checks/probes via SM HTTP API)
2. **Resolve Prometheus datasource UID** — from CLI flag → context default → provider config cache → auto-discovery via provider plugin settings API (SM: `grafana-synthetic-monitoring-app` plugin settings; SLO: each definition carries its `DestinationDatasource`)
3. **Execute aggregate PromQL queries** — two queries cover all objects at once, grouped by label (`job/instance` for SM, `grafana_slo_uuid` for SLO), avoiding per-object query loops
4. **Merge** — domain objects joined to metric results by stable key; missing metrics yield NODATA status
5. **Render** — standard codec pipeline (`-o table`, `-o wide`, `--o json`, `-o graph`)

**Concurrency:** Init-phase operations (domain list, probe list, datasource resolution, REST config) run concurrently via `errgroup`. The two aggregate Prometheus queries also execute in parallel.

Key files:
- `internal/providers/slo/definitions/status.go` — `FetchMetrics` (4 parallel queries per datasource group)
- `internal/providers/synth/checks/status.go` — `BuildAllSuccessRateQuery`, `BuildAllProbeCountQuery`, `queryInstantByJobInstance`
- `internal/providers/synth/smcfg/loader.go` — `StatusLoader` interface (datasource UID resolution + caching)

---

## 6. Folder Hierarchy — Why Order Matters

Grafana folders can be nested. A child folder's `metadata.annotations` carries a
`folder` annotation pointing to its parent's UID. Creating a child before its parent
will fail with a 404/validation error from the API.

`internal/resources/remote/folder_hierarchy.go` implements a topological sort:

```
SortFoldersByDependency(folders []*Resource) [][]*Resource

Phase 1: buildFolderHierarchy
  For each folder:
    uid := folder.Name()
    parentUID := folder.GetFolder()  (reads annotation)
    if parentUID == "" → add to rootUIDs
    if parentUID in nodes → parentNode.children = append(...)
    else → orphan, treat as root (parent not in current set)

Phase 2: assignLevels (depth-first traversal)
  traverse(rootUID, level=0):
    node.level = level
    for each child: traverse(child.Name(), level+1)

Phase 3: Group by level
  levels[0] = all root folders
  levels[1] = direct children
  levels[2] = grandchildren
  ...

Concurrency strategy:
  Level 0 → push all concurrently    ──── wait ────
  Level 1 → push all concurrently    ──── wait ────
  Level 2 → push all concurrently
```

The two-phase push in `pusher.go:109-131`:
1. `pushFolders` handles all folders level-by-level (sequential between levels,
   concurrent within a level).
2. Non-folder resources are pushed concurrently after ALL folders complete.

This guarantees that when a dashboard specifies a parent folder, that folder
already exists in Grafana.

Circular dependency detection: if a node's level remains `-1` after traversal
(unreachable from any root), `SortFoldersByDependency` returns an error.

---

## 7. Local I/O Details

### FSReader (`internal/resources/local/reader.go`)

Three-goroutine pipeline with buffered channels:

```
goroutine 1 (path walker):              goroutine 2 (file readers):
  paths → stat → if dir: WalkDir   →   pathCh → errgroup.SetLimit(N)
                 if file: direct   →             ReadFile per path
                          ↓                        decoderForFormat(ext)
                      pathCh chan                    json: JSONCodec
                                                     yaml/yml: YAMLCodec
                                                   Decode → Unstructured
                                                   filters.Matches(obj)
                                                   → resCh chan
                                         ↓
goroutine 3 (collector):
  resCh → dedup by {gvk,name}
         → dst.Add(&obj)
```

Deduplication uses `objIdx{gvk, name}` as map key. First-seen wins; duplicates
are logged and skipped.

Source tracking: each `Resource` gets `SourceInfo{Path: filePath, Format: codec.Format()}`
attached via `dst.SetSource(...)`. This enables round-trip format preservation
(pull as YAML stays YAML on push).

Concurrency: `MaxConcurrentReads` controls both the channel buffer size and the
`errgroup.SetLimit(N)` on file readers. Default is 1 if not set; callers typically
use the same `MaxConcurrent` flag as push/pull operations (default 10).

### FSWriter (`internal/resources/local/writer.go`)

Sequential write — no concurrency:
```
for each resource in resources.AsList():
  filename := Namer(resource)   // e.g. GroupResourcesByKind("yaml")
  fullPath := filepath.Join(writer.Path, filename)
  ensureDirectoryExists(filepath.Dir(fullPath))
  file := os.OpenFile(fullPath, O_CREATE|O_WRONLY|O_TRUNC, 0644)
  Encoder.Encode(file, resource.ToUnstructured())
```

`GroupResourcesByKind(ext)` is the standard `FileNamer`:
  - Output: `{OutputDir}/{Kind}.{Version}.{Group}/{Name}.{ext}`
  - Example: `./resources/Dashboard.v1alpha1.dashboard.grafana.app/my-dashboard.yaml`

The encoder is fixed at construction time (caller chooses JSON or YAML). Unlike
FSReader which detects format per-file, FSWriter uses a single encoder for all output.

---

## 8. Format Handling (`internal/format/codec.go`)

Both `JSONCodec` and `YAMLCodec` implement `Codec` (Encoder + Decoder):

```go
type Codec interface {
    Encoder   // Encode(dst io.Writer, value any) error
    Decoder   // Decode(src io.Reader, value any) error
    Format() Format
}
```

| Codec | Library | Encode options | Decode options |
|-------|---------|----------------|----------------|
| JSONCodec | `encoding/json` | `SetIndent("", "  ")` | standard |
| YAMLCodec | `github.com/goccy/go-yaml` | Indent=2, IndentSequence, UseJSONMarshaler | Strict(), UseJSONUnmarshaler |

`UseJSONMarshaler`/`UseJSONUnmarshaler` on YAML means the YAML codec delegates
to JSON marshaling logic for types that implement `json.Marshaler`. This is
important for `unstructured.Unstructured` which implements `MarshalJSON()`.

`YAMLCodec.BytesAsBase64` is a flag for custom `[]byte` encoding/decoding — base64
in both directions. Used in some contexts where binary fields must survive YAML roundtrip.

Format detection in FSReader is file-extension based:
```go
switch ext {
case "json":          return JSONCodec
case "yaml", "yml":   return YAMLCodec
default:              return UnrecognisedFormatError
}
```

Round-trip preservation: because `SourceInfo.Format` is stored with each resource,
a pull-then-push workflow will write back in the same format as the original file.

---

## 9. SERVE Pipeline

Entry point: `internal/server/server.go:55` (`Server.Start`).

Command: `gcx dev serve [DIR]...` (the serve command moved from `resources serve` to `dev serve` in PR #35; the implementation is unchanged).

```
Startup sequence:
  ┌──────────────────────────────────────────────────────────┐
  │ 1. Build reverse proxy                                    │
  │    httputil.ReverseProxy targeting Grafana server URL    │
  │    Transport: httputils.NewTransport (handles TLS/auth)  │
  │    Rewrite: injects auth headers, removes Origin         │
  └──────────────────┬───────────────────────────────────────┘
                     │
  ┌──────────────────▼───────────────────────────────────────┐
  │ 2. Build Chi router                                       │
  │                                                           │
  │    Static proxy routes (transparent passthrough):        │
  │      GET /public/*  → proxy                              │
  │      GET /avatar/*  → proxy                              │
  │                                                           │
  │    Mock routes (return hardcoded JSON, suppress noise):  │
  │      GET /api/search → "[]"                              │
  │      GET /api/folders → "[]"                             │
  │      POST /api/frontend-metrics → "[]"                   │
  │      ... (15+ mock routes, see server.go:163-197)        │
  │                                                           │
  │    Resource handlers (DashboardProxy, FoldersProxy):     │
  │      GET /d/{uid}/{slug}          → proxy (HTML iframe)  │
  │      GET /apis/dashboard.../{name}/dto → dashboardJSON   │
  │      PUT /apis/dashboard.../{name}    → dashboardSave    │
  │                                                           │
  │    Live reload:                                           │
  │      GET /livereload → WebSocket upgrade                 │
  │                                                           │
  │    gcx UI:                                         │
  │      GET /  → resource index page (HTML template)        │
  │      GET /gcx/{group}/{version}/{kind}/{name}      │
  │               → iframe wrapper for previewing resource   │
  └──────────────────┬───────────────────────────────────────┘
                     │
  ┌──────────────────▼───────────────────────────────────────┐
  │ 3. File watcher (external — wired by cmd layer)          │
  │    watch.NewWatcher(ctx, callback)                       │
  │    watcher.Add(paths...) — WalkDir to register all dirs  │
  │    watcher.Watch() — goroutine listening to fsnotify     │
  │                                                           │
  │    Watcher fires callback(changedFilePath) on:           │
  │      fsnotify.Create or fsnotify.Write events            │
  │      Ignores temp files ending in "~"                    │
  │                                                           │
  │    Callback (in cmd layer): re-read file via FSReader,   │
  │    call resources.Update(res) which triggers OnChange    │
  └──────────────────┬───────────────────────────────────────┘
                     │
  ┌──────────────────▼───────────────────────────────────────┐
  │ 4. Live reload WebSocket hub                             │
  │    livereload.Initialize() starts hub goroutine          │
  │                                                           │
  │    resources.OnChange(func(res) {                        │
  │      livereload.ReloadResource(res)                      │
  │    })                                                     │
  │                                                           │
  │    ReloadResource builds JSON message:                   │
  │    {"command":"reload","path":"/gcx/{apiVer}/{kind}/{name}"}
  │    → wsHub.broadcast channel                             │
  │                                                           │
  │    hub.run() goroutine broadcasts to all connections     │
  │    Browser receives message and navigates to new path    │
  └──────────────────────────────────────────────────────────┘
```

### Dashboard Interception (DashboardProxy)

When Grafana's UI tries to fetch a dashboard for display:

```
Browser → GET /apis/dashboard.grafana.app/{version}/namespaces/{ns}/dashboards/{name}/dto
                                    ↓
                          dashboardJSONGetHandler
                                    ↓
           c.resources.Find("Dashboard", name)   ← in-memory resource store
                                    ↓
           Build DashboardWithAccessInfo JSON:
             - spec from in-memory resource
             - synthetic access config (canSave:true, canEdit:true, ...)
             - generation = max(resource.Generation, 1)
             ↓
           Return JSON to browser (not proxied to Grafana)
```

When a user saves from the UI:

```
Browser → PUT /apis/dashboard.grafana.app/{version}/.../{name}
                           ↓
                  dashboardJSONPostHandler
                           ↓
          c.resources.Find("Dashboard", name)
                           ↓
          Decode PUT body → unstructured.Unstructured
          Delete AnnotationSavedFromUI annotation
          Reset generation to 0
                           ↓
          Write back to resource.SourcePath() (preserving original format)
          Update in-memory resource.Raw
                           ↓
          Return 200 with the updated object
```

Note: script-generated resources (SourcePath == "") cannot be saved — returns 400.

### WebSocket Live Reload Protocol

Implements the [LiveReload protocol v7](http://livereload.com/protocols/official-7):

```
Browser connects: GET /livereload → WebSocket upgrade
  connection.reader() goroutine → waits for {"command":"hello"}
  → responds: {"command":"hello","protocols":["...official-7"],"serverName":"gcx"}

File changes → ReloadResource(res) → broadcast:
  {"command":"reload","path":"/gcx/{apiVersion}/{kind}/{name}"}

Browser's livereload client receives → navigates to /gcx/.../{name}
→ iframe reloads with fresh dashboard data from in-memory resources
```

---

## 10. OperationSummary — Thread-Safe Result Tracking

`internal/resources/remote/summary.go` provides thread-safe counters for batch operations.

```go
type OperationSummary struct {
    successCount atomic.Int64    // lock-free increment
    failedCount  atomic.Int64    // lock-free increment
    mu           sync.Mutex      // protects failures slice
    failures     []OperationFailure
}
```

- `RecordSuccess()` — atomic increment, no lock
- `RecordFailure(res, err)` — atomic increment + mutex-protected append to slice
- `OperationFailure.Resource` may be nil (e.g. a List operation failed, no specific resource)

Used by all three remote operations (push, pull, delete). The summary is returned
even on partial failure so callers can report both successes and failures.

---

## 11. Concurrency Model Summary

| Location | Mechanism | Limit |
|----------|-----------|-------|
| `FSReader.Read` — file reads | `errgroup` + `SetLimit(MaxConcurrentReads)` | configurable |
| `Puller.Pull` — API fetches | `errgroup` (one goroutine per filter) | = number of filters |
| `Pusher.Push` — folder levels | `ForEachConcurrently` per level, sequential across levels | `MaxConcurrency` |
| `Pusher.Push` — non-folders | `ForEachConcurrently` | `MaxConcurrency` |
| `Deleter.Delete` — API deletes | `ForEachConcurrently` | `MaxConcurrency` |
| LiveReload hub | single goroutine, channel-based fan-out | N/A |
| File watcher | single goroutine reading fsnotify events | N/A |

`ForEachConcurrently` (`resources.go:283`):
```go
func (r *Resources) ForEachConcurrently(ctx context.Context, maxInflight int,
    callback func(context.Context, *Resource) error) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(maxInflight)
    for _, resource := range r.collection {
        g.Go(func() error { return callback(ctx, resource) })
    }
    return g.Wait()
}
```

Error propagation: when `StopOnError=true`, the first error returned from a goroutine
cancels the `errgroup` context, causing other goroutines to exit early via `ctx.Done()`.
When `StopOnError=false`, errors are swallowed per-resource and recorded in the summary;
the goroutine returns `nil` so the errgroup continues.

Default `MaxConcurrent` = 10 (set in `push.go:30`, `pull.go`, etc.).

---

## 12. Key Invariants for Agents Modifying These Flows

1. **Folder ordering is mandatory.** Any modification to push must preserve the
   two-phase approach: folders first (level-by-level), then non-folders. Violating
   this breaks nested folder creation.

2. **FSReader deduplicates by {GVK, name}.** If the same resource appears in multiple
   files, only the first-seen instance is kept. Adding a second pass or merging
   results must account for this.

3. **SourceInfo is set in FSReader, used in FSWriter and DashboardProxy.** Any new
   code path that creates `Resource` objects outside of FSReader must set `SourceInfo`
   if round-tripping or save-back functionality is needed.

4. **ServerFieldsStripper rebuilds the entire object.** It does not patch in-place;
   it constructs a new `unstructured.Unstructured` with only `{apiVersion, kind,
   metadata{name, namespace, annotations, labels}, spec}`. Fields outside those
   will be lost after stripping. This is intentional for clean pull output.

5. **OperationSummary is not an error.** Failures recorded in the summary do not
   cause `Push`/`Pull`/`Delete` to return an error (unless `StopOnError=true`).
   Callers must inspect `summary.FailedCount()` separately.

6. **Format detection is extension-based, not content-based.** Files without
   `.json`, `.yaml`, or `.yml` extensions will return `UnrecognisedFormatError`.

7. **upsertResource reads resourceVersion before update.** `pusher.go:259` copies
   `resourceVersion` from the existing object. Any code doing updates outside of
   the Pusher must do the same or Grafana's API will reject the update with a conflict.

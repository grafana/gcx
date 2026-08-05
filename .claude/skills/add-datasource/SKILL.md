---
name: add-datasource
description: Use when adding a new datasource type to gcx (e.g., Elasticsearch, CloudWatch, InfluxDB), or when the user says "add datasource", "new datasource type", or "integrate [datasource]".
---

# Add Datasource Type

Orchestrates adding a new datasource type plugin — from API discovery through
verified implementation. Three stages with human approval gates.

## When to Use

- User wants to add CLI support for a new Grafana datasource type
- User says "add datasource", "new datasource type"
- A task references datasource type implementation

**When NOT to use**: If the datasource is Prometheus, Loki, Pyroscope, or Tempo —
those already exist. If the product is a Grafana Cloud product (not a datasource),
use `/add-provider` instead.

## Workflow

```
Discover ──gate──> Implement ──gate──> Verify
   │                    │                  │
   v                    v                  v
research report     code per step      smoke tests
```

| Stage | Deliverable | Gate |
|-------|-------------|------|
| 1. Discover | Research report | User approves findings |
| 2. Implement | Code (one step at a time) | `mise run all` passes per step |
| 3. Verify | Smoke tests + annotation check | All checks green |

### Prerequisites

Confirm with the user before starting:
- **Datasource type** — which Grafana datasource plugin (e.g., `elasticsearch`, `cloudwatch`)
- **Access** — do they have a gcx context configured that points to a Grafana instance
  with this datasource? If so, use it directly — run `bin/gcx datasources list -o json`
  yourself to find the datasource UID and plugin type string. Don't ask the user to
  run commands you can run yourself.
- **Scope** — which operations? (query, labels, metadata, series, etc.)

---

## Stage 1: Discover

### 1a. Gather User Context

1. Run `bin/gcx datasources list -o json` to find the datasource UID and plugin type
   string. If the user has a configured context, do this yourself rather than asking
   them to do it.
2. Ask for API documentation or source code for the datasource's query language and
   endpoints. Don't guess what query language or syntax the datasource uses — ask for
   docs. The user will need to provide documentation or links for query expression
   format and any metadata/label endpoints.
3. Known quirks — special auth, pagination, response formats?

### 1b. Research

- Use `gcx api` raw calls to probe the datasource proxy API surface
  (`/api/datasources/proxy/uid/{uid}/...` or `/api/datasources/uid/{uid}/resources/...`)
- Identify query endpoints and response shapes based on the docs the user provided
- Identify metadata endpoints (labels, series, etc.) — the user may need to provide
  explicit information about what endpoints exist for non-query operations

### 1c. Write Research Report

Document findings. Must include:
- API endpoints and response shapes
- Query request/response format
- Available metadata operations
- At least one successful API call result

### Gate: User Approves Research

---

## Stage 2: Implement

### Step 1: Query Client

Create `internal/query/{kind}/` with:

- **`client.go`** — HTTP client wrapping Grafana datasource API
  ```go
  type Client struct {
      restConfig config.NamespacedRESTConfig
      httpClient *http.Client
  }

  func NewClient(cfg config.NamespacedRESTConfig) (*Client, error)
  func (c *Client) Query(ctx context.Context, uid string, req QueryRequest) (*QueryResponse, error)
  // Add Labels, Metadata, etc. as needed
  ```
- **`types.go`** — Request/Response structs
- **`formatter.go`** — Table rendering functions

Use `rest.HTTPClientFor(&cfg.Config)` for the HTTP client (datasource proxy
calls go through Grafana, which handles auth).

Reference: `internal/query/prometheus/`, `internal/query/loki/`

### Step 1b: Command Constructors

Create `internal/datasources/{kind}/` with command constructor files:

- **`query.go`** — `QueryCmd(loader *providers.ConfigLoader) *cobra.Command`
- **`labels.go`** — `LabelsCmd(...)` (if the datasource supports label discovery)
- Other commands as needed (metadata, series, etc.)

Each file follows this pattern:
```go
package {kind}

import (
    "github.com/grafana/gcx/internal/agent"
    dsquery "github.com/grafana/gcx/internal/datasources/query"
    "github.com/grafana/gcx/internal/providers"
    "github.com/grafana/gcx/internal/query/{kind}"
    "github.com/spf13/cobra"
)

func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
    shared := &dsquery.SharedOpts{}
    var datasource string

    cmd := &cobra.Command{
        Use:   "query EXPR",
        Short: "Execute a query against a {Name} datasource",
        Long: `Execute a query against a {Name} datasource.

EXPR is the query expression to evaluate.
Datasource is resolved from -d flag or datasources.{kind} in your context.`,
        Example: `
  # Query using configured default datasource
  gcx datasources {kind} query 'EXPR'

  # Query with explicit datasource UID
  gcx datasources {kind} query -d UID 'EXPR' --since 1h

  # Output as JSON
  gcx datasources {kind} query -d UID 'EXPR' -o json`,
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            // ... resolve datasource, create client, execute query
        },
    }

    cmd.Annotations = map[string]string{
        agent.AnnotationTokenCost: "medium",
        agent.AnnotationLLMHint:   "gcx datasources {kind} query -d UID 'EXPR' -o json",
    }

    shared.Setup(cmd.Flags(), true)
    cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID")
    return cmd
}
```

**Command field conventions:**
- **`Long`**: Include a description of what the command does plus how the datasource
  is resolved. Mention `datasources.{kind}` as the config key.
- **`Example`**: Use `gcx datasources {kind} <subcommand>` format (not the top-level
  provider path). Use `UID` as the placeholder for datasource UIDs.
- **`Annotations`**: Set `agent.AnnotationTokenCost` (`"small"` for metadata/labels,
  `"medium"` for queries) and `agent.AnnotationLLMHint` (a representative one-liner
  using `gcx datasources {kind} ...` format). Import `"github.com/grafana/gcx/internal/agent"`.

Reference: `internal/datasources/prometheus/`, `internal/datasources/loki/`

### Step 1c: Explore Link (required)

Every query-class leaf command must build a Grafana Explore URL. Users rely on
`--share-link` to hand a query to a colleague, and on `--open` to continue in
the UI. A datasource that omits this is inconsistent with the rest of the CLI.

Create `internal/datasources/{kind}/explore.go`. Pick the shape that matches how
the datasource carries its query.

**Shape A — expression datasources** (SQL, PromQL, LogQL, TraceQL). The whole
query is one string, so `dsquery.ExploreQuery.Expr` holds it.

```go
package {kind}

import (
    "strings"

    dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// QueryExploreURL builds a Grafana Explore URL for a {Name} query.
func QueryExploreURL(host string, query dsquery.ExploreQuery) string {
    if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
        return ""
    }

    from, to := dsquery.ExploreRange(query.From, query.To, false)

    q := map[string]any{
        "refId":      "A",
        "datasource": dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
        // ... the plugin's own query fields
    }

    return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{q}, from, to, nil), nil)
}
```

Reference: `internal/datasources/clickhouse/explore.go`.

**Shape B — structured datasources** (CloudWatch, Cloud Monitoring, Azure
Monitor, Elasticsearch). The query is a set of typed fields — project, metric,
namespace, aggregation, group-bys, filters — not one string. Take the client's
request struct as a third parameter. Do not flatten it into `Expr` and re-parse
it: that loses information and defeats the shared builder.

```go
// QueryExploreURL builds a Grafana Explore URL for a {Name} query.
// base supplies the datasource UID, the time range and the org ID.
// base.Expr is unused — the query lives in req.
func QueryExploreURL(host string, base dsquery.ExploreQuery, req {kind}client.QueryRequest) string {
    if strings.TrimSpace(host) == "" || base.DatasourceUID == "" || req.Project == "" {
        return ""
    }

    from, to := dsquery.ExploreRange(base.From, base.To, false)
    model := {kind}client.BuildQueryModel(base.DatasourceUID, req)

    return dsquery.BuildExploreURL(host, base.OrgID,
        dsquery.SinglePane(base.DatasourceUID, []any{model}, from, to, nil), nil)
}
```

Reference: `internal/datasources/cloudwatch/explore.go`.

**Guard the fields the datasource actually requires — not `Expr` by reflex.**
Always check `host` and `DatasourceUID`. Beyond that:

- Shape A: guard `Expr`, unless an empty query is meaningful. An empty
  Elasticsearch Lucene expression matches every document, so an `Expr` guard
  there suppresses the link for a legal invocation.
- Shape B: guard the required request fields (project + metric type,
  subscription + metric name, and so on). `Expr` is empty by design. Never
  guard it.

Return `""` when a required field is missing. The caller warns the user; a
missing link does not fail the command.

Then wire it into the command's `RunE`:

```go
share := &dsquery.ExploreLinkOpts{}          // next to shared := &dsquery.SharedOpts{}
// ...
share.Setup(cmd.Flags(), "executed query")   // next to shared.Setup(...)
```

Replace the direct `IO.Encode(...)` return with:

```go
// Shape A — the query is the Expr string:
exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
    DatasourceUID:  datasourceUID,
    DatasourceType: dsType,
    Expr:           expr,
    From:           shared.From,
    To:             shared.To,
    OrgID:          dsquery.OrgID(cfgCtx),
})

// Shape B — pass the same req the client executed, so the link and the
// query can never disagree. Leave Expr unset.
exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
    DatasourceUID:  datasourceUID,
    DatasourceType: dsType,
    From:           shared.From,
    To:             shared.To,
    OrgID:          dsquery.OrgID(cfgCtx),
}, req)

unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("query")

return dsquery.EncodeAndHandleExplore(cmd, func() error {
    return shared.IO.Encode(cmd.OutOrStdout(), resp)
}, *share, dsquery.ExploreLink{
    URL:            exploreURL,
    UnavailableMsg: unavailableMsg,
    FailedOpenMsg:  failedOpenMsg,
})
```

**Rules:**

- **The Explore query map must mirror the request body the client sends**, minus
  `from` and `to` — the pane range carries the time span. Plugins reject or
  silently misread a query shape that differs from their own model. Prefer one
  exported query-map builder in the client package, called by both `client.go`
  and `explore.go`, so the two shapes cannot drift apart.
- **One builder per query model.** A datasource with several query types needs
  one URL builder per type (e.g. Azure Monitor metrics vs Logs vs Resource
  Graph). Do not reuse a single builder across different `queryType` values.
- **Never leak an internal rewrite into the link.** When the command rewrites
  the query before it sends it (e.g. a sentinel `LIMIT eff+1`), pass the
  user-facing form to `Expr`. See the `displaySQL` split in
  `internal/datasources/clickhouse/query.go`.
- **Mention both flags** in the command's `Long` and `Example` text.
- **A unit test cannot prove the URL is right.** Verify each new URL in a
  browser during Stage 3.

Reuse these helpers — do not write new ones:

| Helper | File |
|--------|------|
| `ExploreQuery`, `ExploreRange`, `ExploreDatasource`, `SinglePane`, `BuildExploreURL` | `internal/datasources/query/explore.go` |
| `ExploreLinkOpts`, `ExploreLink`, `ExploreMessages`, `OrgID`, `EncodeAndHandleExplore` | `internal/datasources/query/sharelink.go` |

Reference: `internal/datasources/clickhouse/explore.go` (the SQL case) and
`internal/datasources/prometheus/explore.go` (the metrics case).

### Step 2: DatasourceProvider

Add a registration file in `internal/datasources/providers/`. This package
contains one registration file per built-in datasource (see
`prometheus.go`, `loki.go`, `tempo.go`, `pyroscope.go`).

```go
// internal/datasources/providers/{kind}.go
package providers

import (
    "github.com/grafana/gcx/internal/datasources"
    "github.com/grafana/gcx/internal/datasources/{kind}"
    "github.com/grafana/gcx/internal/providers"
    "github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
    datasources.RegisterProvider(&{kind}DSProvider{})
}

type {kind}DSProvider struct{}

func (p *{kind}DSProvider) Kind() string      { return "{kind}" }
func (p *{kind}DSProvider) ShortDesc() string { return "Query {Name} datasources" }

func (p *{kind}DSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
    return {kind}.QueryCmd(loader)
}

func (p *{kind}DSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
    return []*cobra.Command{
        // {kind}.LabelsCmd(loader),
    }
}
```

The `DatasourceProvider` interface is defined in
`internal/datasources/provider.go`. The `loader` is supplied by the mounting
code in `cmd/gcx/datasources/command.go`, which binds `--config`/`--context`
on each provider sub-command. Forward it to each command constructor.

Reference: `internal/datasources/providers/prometheus.go`.

### Step 3: Registration & Wiring

1. The `internal/datasources/providers/` package is already blank-imported in
   `cmd/gcx/root/command.go` — new registrations in that package are
   automatically picked up. No import changes needed.
2. **`NormalizeKind()` mapping** — Grafana plugin IDs often differ from the short
   kind name (e.g., `grafana-pyroscope-datasource` → `pyroscope`,
   `prometheus` → `prometheus`). Check the plugin ID via
   `gcx datasources list -o json` and add a mapping in
   `internal/datasources/query/resolve.go` if they don't match. Without this,
   auto-discovery and datasource type validation will fail silently.
3. Optionally add to the auto-detecting `datasources query` switch in
   `cmd/gcx/datasources/query.go`

### Step 4: Agent Annotations

Annotations should already be set on each command via `cmd.Annotations` in the
constructor (see Step 1b). Verify every leaf command has both
`agent.AnnotationTokenCost` and `agent.AnnotationLLMHint` set.

If the datasource also needs entries in `internal/agent/command_annotations.go`
(for commands that exist outside the DatasourceProvider path), add them there too:

```go
"gcx datasources {kind} query": {Cost: "large", Hint: "..."},
"gcx datasources {kind} labels": {Cost: "small"},
```

### Gate: `mise run all` passes

---

## Stage 3: Verify

### 3a. Smoke Tests

Only test the subcommands that were actually added:

```bash
# Build
mise run build

# Verify the parent command and each subcommand exist
bin/gcx datasources {kind} --help

# Test each subcommand against real Grafana — only commands you implemented
bin/gcx datasources {kind} query '<expr>' --since 1h
# bin/gcx datasources {kind} labels -d UID  (if labels was added)
# etc.
```

### 3a-bis. Explore Link Check (required)

Run this for every query-class subcommand you added. A unit test cannot prove
the URL opens the right query, so check it in a browser:

```bash
bin/gcx datasources {kind} query -d UID '<expr>' --since 1h --share-link
bin/gcx datasources {kind} query -d UID '<expr>' --since 1h --open
```

Confirm three things in Grafana:

1. Explore loads the correct datasource.
2. The query text matches what you sent.
3. The time range matches `--since`.

Repeat for each query type when the datasource has more than one.

### 3b. Run Checks

```bash
# Full quality gates
mise run all

# Agent annotation consistency
go test ./internal/agent/...
```

### Gate: All Green

---

## Reference Implementations

| Kind | Commands | DSProvider Registration | Query Client | Explore Builder |
|------|----------|----------------------|-------------|-----------------|
| prometheus | `internal/datasources/prometheus/` | `internal/datasources/providers/prometheus.go` | `internal/query/prometheus/` | `internal/datasources/prometheus/explore.go` |
| loki | `internal/datasources/loki/` | `internal/datasources/providers/loki.go` | `internal/query/loki/` | `internal/datasources/loki/explore.go` |
| clickhouse | `internal/datasources/clickhouse/` | `internal/datasources/providers/clickhouse.go` | `internal/query/clickhouse/` | `internal/datasources/clickhouse/explore.go` (Shape A) |
| cloudwatch | `internal/datasources/cloudwatch/` | `internal/datasources/providers/cloudwatch.go` | `internal/query/cloudwatch/` | `internal/datasources/cloudwatch/explore.go` (Shape B) |
| pyroscope | `internal/datasources/pyroscope/` | `internal/datasources/providers/pyroscope.go` | `internal/query/pyroscope/` | — |
| tempo | `internal/datasources/tempo/` | `internal/datasources/providers/tempo.go` | `internal/query/tempo/` | `internal/datasources/tempo/explore.go` |

Use `clickhouse` as the reference for an expression datasource (Shape A), and
`cloudwatch` for a structured datasource (Shape B).

Note: `cloudwatch`'s Explore map has drifted from its client — the client sends
`expression` and `metricQueryType`, the Explore map omits them. Copy its
signature shape, not its map. The drift is the reason the shared-builder rule
above exists.

## Common Pitfalls

| Pitfall | Mitigation |
|---------|------------|
| Datasource proxy path varies | Check if `/api/datasources/proxy/uid/` or `/api/datasources/uid/.../resources/` |
| Plugin ID vs short kind | Add mapping to `NormalizeKind()` in `internal/datasources/query/resolve.go` |
| Missing agent annotations | New leaf commands must appear in `internal/agent/command_annotations.go` |
| PersistentPreRun chain | Always propagate to root in the DatasourceProvider parent command |
| Explore URL opens an empty pane | The query map must mirror the client's request body. Build both from one shared function. |
| Explore link leaks an internal rewrite | Pass the user-facing query to `Expr`, not the sentinel-limited form |
| Duplicated request-body code | Reuse `internal/query/sql` for SQL datasources. Do not copy another client's `Query` method. |

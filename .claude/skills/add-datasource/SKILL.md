---
name: add-datasource
description: Use for the implementation workflow that adds gcx CLI support for a datasource type not registered in internal/datasources/providers — query client, command constructors, DatasourceProvider registration. Trigger on "add support for an unsupported datasource type" or "new datasource type". NOT for creating or configuring a datasource instance in a Grafana stack (that is the shipped `gcx datasources create`), NOT for extending an already-registered kind, and NOT for deciding the integration tier or contract or running pre-review self-checks — use the existing kind's implementation or the repo-local integrate-with-gcx contributor skill instead.
---

# Add Datasource Type

Orchestrates adding a new datasource type plugin — from API discovery through
verified implementation. Three stages, worked autonomously: the stage boundaries
are checkpoints you satisfy, not approvals you wait for.

## When to Use

- User wants gcx CLI support for a datasource type gcx does not yet support
- User says "add support for an unsupported datasource type", "new datasource type"
- A task references datasource type implementation

**When NOT to use**:

- **The user wants a datasource instance, not a type.** "Add a datasource" most
  often means creating or configuring one in a Grafana stack — that is
  `gcx datasources create` / `update`, already shipped. This skill writes Go
  code to teach gcx a new *kind*. Confirm which one is meant before starting.
- The kind is already registered under `internal/datasources/providers/` —
  extend that implementation instead of adding a duplicate.
- The product is a Grafana Cloud product, not a datasource — use
  `/add-provider`.

## Entry paths

**Invoked from `integrate-with-gcx`** (the placement section already exists —
necessity, command path, backend evidence, wiring, readiness):

- Skip the Stage 1 questions it already answers: the datasource kind and plugin
  type string, the query/metadata endpoints, and the readiness verdict. Record
  them and move on rather than re-asking.
- The Stage 1 approval gate **does not apply on this path**. Build autonomously.
  If a query-language or endpoint detail is genuinely missing, discover it from
  the vendor docs, or ask one targeted question carrying the evidence and a
  recommendation — never fall back to a blanket approval gate.
- Start at Stage 2, and use Stage 3 verification as written.

**Invoked directly** (no placement section): work through all three stages.
Autonomy is the same as above — the Stage 1 gate is a checkpoint you satisfy, not
an approval you wait for. Discover the plugin type, endpoints and response shapes
from `bin/gcx datasources list -o json`, the vendor's API docs and `bin/gcx api`
probes; present findings and keep going. Ask only where an unresolved answer would
materially change the implementation — an unknown query-expression format, an
endpoint you cannot verify. If no instance is reachable, report the live checks as
**UNVERIFIED** with the reason rather than blocking or claiming them green.


## The flow, however you got here

```text
contract (proportional)  →  implementation  →  Review
```

Both entry paths run all three, and none of them is a document or a gate:

- **Contract, before code** — `.claude/skills/integrate-with-gcx/references/contract-and-tests.md`,
  sized to the change. If you arrived from `integrate-with-gcx` the contract
  already exists; use it, don't redo it.
- **Review, before calling it review-ready** —
  `.claude/skills/integrate-with-gcx/references/self-review.md`, re-run after
  every fix push.

That is where the naming, typed-input, output-class, completeness, error,
token-cost and test-quality guidance lives. Read those two rather than restating
them here.

## Workflow

```
Discover ───────> Implement ──gate──> Verify
   │                    │                  │
   v                    v                  v
research findings   code per step      smoke tests
```

| Stage | Deliverable | Gate |
|-------|-------------|------|
| 1. Discover | research findings | findings presented; no approval wait |
| 2. Implement | Code (one step at a time) | `mise run gate` passes per step; `GCX_AGENT_MODE=false mise run all` once before push |
| 3. Verify | Smoke tests + annotation check | smoke tests run or reported UNVERIFIED; wiring checks pass |

### Prerequisites — discover these, don't ask for them

Settle each from the repo and the environment first. Ask only if what remains is
materially insufficient, and then in one grouped question carrying the evidence
and a recommendation:

- **Datasource type** — usually stated in the request. Confirm the plugin type
  string yourself with `bin/gcx datasources list -o json`.
- **Access** — check for a configured context the same way. If none is reachable,
  proceed against the vendor's API docs and report every live check as UNVERIFIED
  with the reason; do not stop.
- **Scope** — infer from the request (a "query client" means `query` first) and
  state what you inferred. Extra verbs are additive later; a wrong frozen name is
  not.

---

## Stage 1: Discover

### 1a. Gather User Context

1. Run `bin/gcx datasources list -o json` to find the datasource UID and plugin type
   string. If the user has a configured context, do this yourself rather than asking
   them to do it.
2. Find the query language and endpoint shapes from the vendor's API docs or the
   plugin's source before writing anything — do not guess them. If they cannot be
   settled that way, ask once, naming exactly what is missing and what you will
   assume otherwise.
3. Known quirks — special auth, pagination, response formats?

### 1b. Research

- Use `bin/gcx api` raw calls to probe the datasource proxy API surface
  (`/api/datasources/proxy/uid/{uid}/...` or `/api/datasources/uid/{uid}/resources/...`)
- Identify query endpoints and response shapes from the vendor's API docs or the
  plugin's source
- Identify metadata endpoints (labels, series, etc.) the same way; if a non-query
  endpoint cannot be established, record it as UNVERIFIED rather than guessing

### 1c. Record findings

Keep them in your working notes and the PR description; write a standalone
`docs/research/` report only if the investigation has lasting repository value or
staged work must resume from it. Either way, what you record must cover:
- API endpoints and response shapes
- Query request/response format
- Available metadata operations
- At least one successful probe result — or, if no instance is reachable, the
  probe you would run, marked UNVERIFIED with the reason

### Checkpoint: Research Complete

Direct-invocation path only — see [Entry paths](#entry-paths).

---

## Stage 2: Implement

### Step 1: Query Client

Create `internal/query/{kind}/` with `client.go` and `types.go`. Add a
`formatter.go` **only if** you define your own response type — if you reuse a
shared one, its formatter and codecs already exist (see Step 3).

**Start with the shared transport — it is the default, not an optimisation.**
AGENTS.md Key Conventions: a client that calls Grafana's unified datasource query
API (`/apis/query.grafana.app/.../query`, with the `/api/ds/query` fallback) must
reuse `internal/query/grafanaquery` for the HTTP transport (POST + fallback +
response-size limiting) and `internal/query/dataframe` for the data-frame wire
types. Do not duplicate that logic or re-declare
`GrafanaQueryResponse`/`DataFrame`. Check the current set with
`grep -rl query/grafanaquery internal/query/` and copy the closest one.

**If the datasource takes raw SQL, the request body and `--limit` enforcement are
shared too** — `querysql.BuildRawQueryBody` and `querysql.EnforceLimit` with a
dialect-local `bail` predicate, never a hand-rolled clamp. Read
`references/raw-sql.md` before writing either: it carries the plugin-`format`
exception, the stderr disclosure `capped` owes the caller, the four statement
shapes `bail` has to catch, and which of the existing dialects is safe to copy.

**Pick the client shape from what your commands actually call — there are three,
and the middle one is the common case.** The two transports are not alternatives:
unified query is a POST to `/apis/query.grafana.app/.../query`, while label,
series, metadata and other discovery endpoints are proxy/resource requests
(`/api/datasources/uid/{uid}/resources/...`) that `grafanaquery.Client` cannot
reach — it only exposes `Execute`. The verb there is per-plugin, not always GET:
prometheus and loki GET their label endpoints, athena POSTs a body to its
resource endpoint (`internal/query/athena/client.go:42`).

1. **Query-only** — a `query` leaf and nothing else:

```go
type Client struct {
    queryClient *grafanaquery.Client
}

func NewClient(cfg config.NamespacedRESTConfig) (*Client, error)
func (c *Client) Query(ctx context.Context, uid string, req QueryRequest) (*QueryResponse, error)
```

2. **Hybrid query + discovery** — a `query` leaf *plus* `labels`/`metadata`/
   `series`. Hold both transports; this is what every reference client with
   non-query commands does (prometheus, loki, cloudwatch: `restConfig` +
   `httpClient` + `queryClient`; athena: `host` + `httpClient` + `queryClient`):

```go
type Client struct {
    restConfig  config.NamespacedRESTConfig     // or a plain host string
    httpClient  *http.Client                    // rest.HTTPClientFor(&cfg.Config)
    queryClient *grafanaquery.Client
}

func (c *Client) Query(ctx context.Context, uid string, req QueryRequest) (*QueryResponse, error)  // queryClient
func (c *Client) Labels(ctx context.Context, uid string) ([]string, error)                         // httpClient
```

   Build `httpClient` with `rest.HTTPClientFor(&cfg.Config)` — a fresh
   `http.Client` drops the kubeconfig auth and TLS wiring, so calls can fail
   authentication or TLS depending on how the context is configured.

3. **Direct-HTTP only** — the datasource is not served by the unified query API
   at all: hold the config/host and `httpClient`, no `queryClient`.

Say in the PR which shape you used and why.

Wire types alias the shared package rather than redeclaring it
(`type GrafanaQueryResponse = dataframe.Response`). Response-type reuse is a
separate decision with a codec consequence — see Step 3.

Reference: `internal/query/prometheus/`, `internal/query/loki/` (hybrid, built on
`grafanaquery` + `dataframe`), `internal/query/athena/client.go` (hybrid over a
plain host)

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
        Use:   "query [EXPR]",
        Short: "Execute a query against a {Name} datasource",
        Long: `Execute a query against a {Name} datasource.

EXPR is the query expression to evaluate; --expr is accepted instead.
Datasource is resolved from -d flag or datasources.{kind} in your context.`,
        Example: `
  # Query using configured default datasource
  gcx datasources {kind} query 'EXPR'

  # Query with explicit datasource UID
  gcx datasources {kind} query -d UID 'EXPR' --since 1h

  # Output as JSON
  gcx datasources {kind} query -d UID 'EXPR' -o json`,
        // RangeArgs, not ExactArgs — the current family accepts the
        // expression positionally OR via --expr. Copy the newest kind
        // (internal/datasources/athena/query.go) rather than this sketch.
        Args: cobra.RangeArgs(0, 1),
        RunE: func(cmd *cobra.Command, args []string) error {
            // ... resolve datasource, create client, execute query
        },
    }

    cmd.Annotations = map[string]string{
        agent.AnnotationTokenCost: "medium",
        agent.AnnotationLLMHint:   "gcx datasources {kind} query -d UID 'EXPR' -o json",
    }

    // The bool is enableGraph. Default to false: it decides whether `graph`
    // is a valid -o value and advertised in the format help, so only pass true
    // once a graph codec arm actually handles your response type (Step 3).
    shared.Setup(cmd.Flags(), false)
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
Monitor, Elasticsearch). The query is a set of typed fields, not one string.
Take the client's request struct as a third parameter. Do not flatten it into
`Expr`.

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
  the query before it sends it (e.g. a sentinel `LIMIT`), pass the rewritten
  form to the client, and pass the user-facing form to `Expr`. No datasource
  does this yet: `internal/datasources/athena/query.go` and
  `internal/datasources/clickhouse/query.go` both pass the `EnforceLimit`
  result to `Expr`, so the link carries a `LIMIT` the user never typed.
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
contains one registration file per built-in datasource — `ls` it for the current
set and copy the most recently added one rather than trusting a list here.

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
code in `cmd/gcx/datasources/command.go`, which binds `--config` on each
provider sub-command. The root owns `--context` and passes its value through the
command context. Forward the loader to each command constructor.

Reference: `internal/datasources/providers/prometheus.go`.

### Step 3: Codec dispatch — the step that breaks the default invocation

`internal/datasources/query/codecs.go` ends `RegisterCodecs` with
`ioOpts.DefaultFormat("table")`, so the plain `gcx datasources {kind} query …`
invocation — no `-o` — goes through the table codec. Its `Encode` is a type
switch that falls through to
`errors.New("invalid data type for query table codec")` (line 59), so a **new**
response type with no arm there fails at encode time. The wide and graph codecs
have their own fallthrough errors (lines 92 and 156); JSON and YAML do not type
switch at all — they delegate to the shared format codecs and serialize whatever
they are handed, which is why the default invocation breaks while `-o json`
looks fine. Lint and unit tests will not catch it.

Two ways out, cheapest first:

1. **Reuse an existing response type.** If your results are table-shaped, returning
   `internal/query/sql`'s response gets you the table and wide codecs for free.
   Verify the fit first: `internal/query/sql/parse.go` takes `Frames[0]` **only**,
   so reuse it for genuinely SQL/`sqlds`-backed one-frame results and not for any
   query that returns one frame per series. It also carries a graph consequence:
   the graph codec's `*querysql.QueryResponse` arm returns "graph output is not
   supported for SQL datasource queries", so pass `shared.Setup(flags, false)` and
   keep `graph` off your `-o` list — athena and clickhouse both do exactly that
   (`internal/datasources/athena/query.go:130`,
   `internal/datasources/clickhouse/query.go:128`). Leaving graph enabled
   advertises a format that fails with an error naming a datasource family the
   caller never asked about.
2. **Add an arm to every codec your type can actually reach** — table and wide
   always, plus graph when a graph-enabled path can return your type.

**On graph specifically, two different commands are in play, which is why "keep
graph off" and "still write a graph arm" are both right.** Your *typed* command
controls its own `-o` list via `shared.Setup(flags, enableGraph)` — keep that
`false` unless the arm genuinely renders a chart. The *generic*
`datasources query` is separate and passes `true`
(`cmd/gcx/datasources/query.go:211`), so if you also add a case to its switch,
your response type reaches the graph codec no matter what your typed command
did. Give it an explicit "not supported for {kind}" arm there: that is why
`*querysql.QueryResponse` has one (`internal/datasources/query/codecs.go:148`)
even though athena and clickhouse both pass `false`. Without the arm the caller
gets the generic "invalid data type for graph codec", which names nothing.
If your kind is *not* in the generic switch and your typed command disables
graph, the arm is unreachable — skip it, per self-review T1 check 4.

Then trace each registration in `RegisterCodecs` to a reachable `Encode`
(self-review T1 check 4).

### Step 4: Registration & Wiring

1. The `internal/datasources/providers/` package is already blank-imported in
   `cmd/gcx/root/command.go` — new registrations in that package are
   automatically picked up. No import changes needed.
2. **`NormalizeKind()` mapping** — Grafana plugin IDs often differ from the short
   kind name (e.g., `grafana-pyroscope-datasource` → `pyroscope`,
   `prometheus` → `prometheus`). Check the plugin ID via
   `gcx datasources list -o json` and add a mapping in
   `internal/datasources/query/resolve.go` if they don't match. Without this,
   auto-discovery and datasource type validation will fail silently.
3. **Routing for the auto-detecting `datasources query`** — registration mounts
   your typed `datasources <kind>` subtree but does not reach the generic
   command, which routes through the tables in
   `cmd/gcx/datasources/query_routes.go`. Add exactly one entry, keyed by the
   normalized kind:
   - the generic `<uid> <expr>` form can honestly carry your query → add a
     `dispatch` entry plus a small handler alongside the existing ones;
   - it cannot, because your query takes structured parameters no single
     expression represents → add a `redirects` entry built with
     `structuredQueryRedirect`, naming your typed command. CloudWatch is the
     worked example.

   Adding neither is also a choice: your kind then reports as unsupported. Make
   it deliberately — a caller who reasonably reaches for `datasources query`
   gets a dead end. The two tables must stay disjoint and keyed by normalized
   kinds; `query_routes_internal_test.go` enforces both.

   The supported-kind list in the unsupported-type error is derived from the
   tables, so you never edit that string. You **do** update the two places that
   pin its exact value, because it is user-visible text on a GA path and is
   deliberately not free to drift:
   `TestQueryRoutesSupportedKindsIsTheSortedUnion` in
   `query_routes_internal_test.go`, and `wantUnsupportedMessage` in
   `query_unsupported_test.go`. Both fail with the old and new lists side by
   side, so the update is mechanical.

### Step 5: Agent Annotations

Annotations should already be set on each command via `cmd.Annotations` in the
constructor (see Step 1b). Verify every leaf command has both
`agent.AnnotationTokenCost` and `agent.AnnotationLLMHint` set.

If the datasource also needs entries in `internal/agent/command_annotations.go`
(for commands that exist outside the DatasourceProvider path), add them there too:

```go
"gcx datasources {kind} query": {Cost: "large", Hint: "..."},
"gcx datasources {kind} labels": {Cost: "small"},
```

### Gate: `mise run gate` per step, `GCX_AGENT_MODE=false mise run all` once before push

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

### 3b. Explore Link Check (required)

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

### 3c. Run Checks

```bash
# Full quality gates
GCX_AGENT_MODE=false mise run all

# Agent annotation consistency
mise exec -- go test ./internal/agent/...
```

### Checkpoint: Verified

---

## Reference Implementations

| Kind | Commands | DSProvider Registration | Query Client | Explore Builder |
|------|----------|----------------------|-------------|-----------------|
| athena | `internal/datasources/athena/` | `internal/datasources/providers/athena.go` | `internal/query/athena/` | `internal/datasources/athena/explore.go` (Shape A) |
| clickhouse | `internal/datasources/clickhouse/` | `internal/datasources/providers/clickhouse.go` | `internal/query/clickhouse/` | `internal/datasources/clickhouse/explore.go` (Shape A) |
| cloudwatch | `internal/datasources/cloudwatch/` | `internal/datasources/providers/cloudwatch.go` | `internal/query/cloudwatch/` | `internal/datasources/cloudwatch/explore.go` (Shape B) |
| infinity | `internal/datasources/infinity/` | `internal/datasources/providers/infinity.go` | `internal/query/infinity/` | — |
| influxdb | `internal/datasources/influxdb/` | `internal/datasources/providers/influxdb.go` | `internal/query/influxdb/` | — |
| loki | `internal/datasources/loki/` | `internal/datasources/providers/loki.go` | `internal/query/loki/` | `internal/datasources/loki/explore.go` |
| postgres | `internal/datasources/postgres/` | `internal/datasources/providers/postgres.go` | `internal/query/postgres/` | — |
| prometheus | `internal/datasources/prometheus/` | `internal/datasources/providers/prometheus.go` | `internal/query/prometheus/` | `internal/datasources/prometheus/explore.go` |
| pyroscope | `internal/datasources/pyroscope/` | `internal/datasources/providers/pyroscope.go` | `internal/query/pyroscope/` | — |
| tempo | `internal/datasources/tempo/` | `internal/datasources/providers/tempo.go` | `internal/query/tempo/` | `internal/datasources/tempo/explore.go` |

A `—` marks a datasource that still has no Explore link. Four kinds have this
gap now: `infinity`, `influxdb`, `postgres`, and `pyroscope`.

Use `clickhouse` as the reference for an expression datasource (Shape A), and
`cloudwatch` for a structured datasource (Shape B).

## Common Pitfalls

| Pitfall | Mitigation |
|---------|------------|
| Datasource proxy path varies | Check if `/api/datasources/proxy/uid/` or `/api/datasources/uid/.../resources/` |
| Plugin ID vs short kind | Add mapping to `NormalizeKind()` in `internal/datasources/query/resolve.go` |
| Missing agent annotations | Every leaf needs a `token_cost` annotation on the built command. Setting it inline via `cmd.Annotations` in the constructor satisfies this, as does an entry in `internal/agent/command_annotations.go` — `agent.ApplyAnnotations` merges that map into the tree at startup. Per-kind datasource leaves normally do it inline |
| PersistentPreRun chain | Always propagate to root in the DatasourceProvider parent command |
| Explore URL opens an empty pane | See the rules in Step 1c |
| Duplicated request-body code | Reuse `internal/query/sql` for SQL datasources. Do not copy another client's `Query` method. |

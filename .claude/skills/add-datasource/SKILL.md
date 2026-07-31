---
name: add-datasource
description: Use for the implementation workflow once a capability is already classified as a new gcx datasource type (e.g., Elasticsearch, CloudWatch, InfluxDB) — query client, command constructors, DatasourceProvider registration. Trigger on "add datasource", "new datasource type". NOT for deciding the integration tier, integration contracts, or pre-review self-checks — use the integrate-with-gcx skill for those (bundled; `gcx agent skills get integrate-with-gcx`).
---

# Add Datasource Type

Orchestrates adding a new datasource type plugin — from API discovery through
verified implementation. Three stages, worked autonomously: the stage boundaries
are checkpoints you satisfy, not approvals you wait for.

## When to Use

- User wants to add CLI support for a new Grafana datasource type
- User says "add datasource", "new datasource type"
- A task references datasource type implementation

**When NOT to use**: If the datasource is Prometheus, Loki, Pyroscope, or Tempo —
those already exist. If the product is a Grafana Cloud product (not a datasource),
use `/add-provider` instead.

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
from `bin/gcx datasources list -o json`, the vendor's API docs and `gcx api`
probes; present findings and keep going. Ask only where an unresolved answer would
materially change the implementation — an unknown query-expression format, an
endpoint you cannot verify. If no instance is reachable, report the live checks as
**UNVERIFIED** with the reason rather than blocking or claiming them green.


## The flow, however you got here

```text
contract (proportional)  →  implementation  →  Review
```

Both entry paths run all three. The contract and the review are the
high-value part — naming against the frozen surface, typed inputs and
explicitly-empty values, output protocol class, completeness honesty, actionable
errors, token cost, shared-transport reuse, mutation-resistant tests — and none
of it is a document or a gate.

- **Contract, before you write code:** cover
  `claude-plugin/skills/integrate-with-gcx/references/contract-and-tests.md`,
  sized to the change. A one-flag addition needs three lines of it; a new
  provider needs all of it. If you arrived from `integrate-with-gcx` the contract
  already exists — use it, don't redo it.
- **Review, before you call it review-ready:** run the diff-triggered checks in
  `claude-plugin/skills/integrate-with-gcx/references/self-review.md`, and re-run
  them after every fix push. Report only unresolved risks, unverified
  assumptions, failed or skipped checks, and architecture deviations.

Read those two files from the checkout, or via
`gcx agent skills get integrate-with-gcx`. Skipping them is the one way to get
this wrong while every gate stays green.

## Workflow

```
Discover ───────> Implement ──gate──> Verify
   │                    │                  │
   v                    v                  v
research report     code per step      smoke tests
```

| Stage | Deliverable | Gate |
|-------|-------------|------|
| 1. Discover | research findings | findings presented; no approval wait |
| 2. Implement | Code (one step at a time) | `mise run gate` passes per step; `GCX_AGENT_MODE=false mise run all` once before push |
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

Direct-invocation path only — see [Entry paths](#entry-paths).

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

**Shared transport rule (AGENTS.md Key Conventions):** if the client calls
Grafana's unified datasource query API (`/apis/query.grafana.app/.../query`,
with the `/api/ds/query` fallback), it must reuse `internal/query/grafanaquery`
for the HTTP transport (POST + fallback + response-size limiting) and
`internal/query/dataframe` for the Grafana data-frame wire types — do not
duplicate that logic or re-declare `GrafanaQueryResponse`/`DataFrame` structs
in the new package. Most existing query clients already reuse them — check the
current set with `grep -rl query/grafanaquery internal/query/` rather than
trusting a count here.

Reference: `internal/query/prometheus/`, `internal/query/loki/` (both built on
`grafanaquery` + `dataframe`)

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
3. Decide, deliberately, what the auto-detecting `datasources query` should do
   for your kind — the switch in `cmd/gcx/datasources/query.go` is
   hand-maintained and no test enforces parity with registration:
   - the generic `<uid> <expr>` form can carry your query → add the case, so a
     caller reaching for `datasources query` is not met with the bare
     "datasource type %q is not supported" default;
   - it cannot (a structured query with several required parameters) → add an
     explicit redirect naming your typed command and its flags, the way
     CloudWatch does. Do not force a lossy generic path.

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

### 3b. Run Checks

```bash
# Full quality gates
GCX_AGENT_MODE=false mise run all

# Agent annotation consistency
mise exec -- go test ./internal/agent/...
```

### Gate: All Green

---

## Reference Implementations

| Kind | Commands | DSProvider Registration | Query Client |
|------|----------|----------------------|-------------|
| prometheus | `internal/datasources/prometheus/` | `internal/datasources/providers/prometheus.go` | `internal/query/prometheus/` |
| loki | `internal/datasources/loki/` | `internal/datasources/providers/loki.go` | `internal/query/loki/` |
| pyroscope | `internal/datasources/pyroscope/` | `internal/datasources/providers/pyroscope.go` | `internal/query/pyroscope/` |
| tempo | `internal/datasources/tempo/` | `internal/datasources/providers/tempo.go` | `internal/query/tempo/` |

## Common Pitfalls

| Pitfall | Mitigation |
|---------|------------|
| Datasource proxy path varies | Check if `/api/datasources/proxy/uid/` or `/api/datasources/uid/.../resources/` |
| Plugin ID vs short kind | Add mapping to `NormalizeKind()` in `internal/datasources/query/resolve.go` |
| Missing agent annotations | New leaf commands must appear in `internal/agent/command_annotations.go` |
| PersistentPreRun chain | Always propagate to root in the DatasourceProvider parent command |

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
| 2. Implement | Code (one step at a time) | `make all` passes per step |
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

### Step 2: DatasourceProvider

Add a registration file in `internal/datasources/providers/`. This package
already contains the shared `dsCmdWithConfig` helper and registrations for
the four built-in datasources.

```go
// internal/datasources/providers/{kind}.go
package providers

import (
    "github.com/grafana/gcx/internal/datasources"
    "github.com/grafana/gcx/internal/datasources/{kind}"
    "github.com/spf13/cobra"
)

func init() {
    datasources.RegisterProvider(&{kind}DSProvider{})
}

type {kind}DSProvider struct{}

func (p *{kind}DSProvider) Kind() string      { return "{kind}" }
func (p *{kind}DSProvider) ShortDesc() string { return "Query {Name} datasources" }

func (p *{kind}DSProvider) QueryCmd() *cobra.Command {
    return dsCmdWithConfig({kind}.QueryCmd)
}

func (p *{kind}DSProvider) ExtraCommands() []*cobra.Command {
    return []*cobra.Command{
        // dsCmdWithConfig({kind}.LabelsCmd),
    }
}
```

The `dsCmdWithConfig` helper is already defined in
`internal/datasources/providers/config.go` — it creates a `ConfigLoader`,
passes it to the command constructor, and binds `--config`/`--context` flags.

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

### Gate: `make all` passes

---

## Stage 3: Verify

### 3a. Smoke Tests

Only test the subcommands that were actually added:

```bash
# Build
make build

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
make all

# Agent annotation consistency
go test ./internal/agent/...
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

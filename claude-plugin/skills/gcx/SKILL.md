---
name: gcx
description: >
  Use gcx CLI to manage Grafana Cloud resources. Trigger when the user wants to
  inspect, create, update, delete, query, or automate any Grafana resource —
  dashboards, datasources, alerts, SLOs, synthetic checks, oncall, incidents,
  fleet, k6, knowledge graph, or adaptive telemetry.
user-invocable: true
disable-model-invocation: false
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent, AskUserQuestion
---

# gcx — Grafana Cloud CLI

gcx is a unified CLI for Grafana Cloud, organized like kubectl: named contexts,
structured output, and a consistent verb model across all resource types.

## Discover Before You Act

gcx has a built-in command catalog. Never guess a command — discover it first.
Use **progressive disclosure** to minimize token cost:

**Step 1 — Orient** (30 lines, all top-level groups):
```bash
gcx help-tree --depth 1 -o text
```

**Step 2 — Drill down** (5-20 lines per group):
```bash
gcx help-tree <group> -o text      # full subtree for one group
gcx <group> <subcommand> --help    # exact flags and args
```

**Build payloads:**
```bash
gcx resources schemas <kind>        # JSON schema for a resource type
gcx resources examples <kind>       # example manifest
```

Only fall back to `gcx commands --flat -o json` when you need structured metadata
for automation — it is 255KB and should not be used for orientation.

### Intent-to-Group Quick Reference

When you already know the user's intent, skip discovery and go straight to the
right group:

| Intent | Group | Example |
|--------|-------|---------|
| Dashboards, folders, K8s resources | `resources` | `gcx resources get dashboards` |
| SLO definitions and reports | `slo` | `gcx slo definitions list` |
| Alert rules and groups | `alert` | `gcx alert rules list` |
| Synthetic Monitoring checks | `synth` | `gcx synth checks list` |
| IRM (OnCall + Incidents) | `irm` | `gcx irm oncall schedules list`, `gcx irm incidents list` |
| k6 load tests, projects, runs | `k6` | `gcx k6 load-tests list` |
| PromQL / Adaptive Metrics | `metrics` | `gcx metrics query 'up'` |
| LogQL / Adaptive Logs | `logs` | `gcx logs query '{app="foo"}'` |
| Profiling (Pyroscope) | `profiles` | `gcx profiles query` |
| Tracing (Tempo) | `traces` | `gcx traces query` |
| Datasource info and queries | `datasources` | `gcx datasources list` |
| Fleet pipelines, collectors | `fleet` | `gcx fleet pipelines list` |
| Knowledge Graph (Asserts) | `kg` | `gcx kg entities list` |
| Frontend Observability | `frontend` | `gcx frontend apps list` |
| App Observability | `appo11y` | `gcx appo11y overrides list` |

If no command exists for the requested operation, say so and propose the nearest
supported flow.

### Avoid Raw API Passthrough

**Do not use `gcx api`** when a dedicated command exists. `gcx api` is a low-level
fallback for endpoints not yet covered by dedicated commands. Dedicated commands
provide proper output formatting, pagination, error handling, and token-efficient
output. Check the intent-to-group table above first.

Similarly, prefer `gcx metrics query` over `gcx datasources query <prometheus-uid>`
for PromQL queries — the signal-specific command handles datasource resolution
automatically.

## Verify Context First

Before any operation, confirm which environment is targeted:
- `gcx config check` — validates the active context and tests connectivity
- `gcx config view` — shows full config (secrets redacted; use `--raw` to reveal)
- `gcx config current-context` — shows just the active context name
- `gcx config use-context <name>` — switch contexts
- `--context <name>` flag on any command — target a specific context without switching

## Output Control

| Intent | Flag |
|--------|------|
| Structured output for parsing | `-o json` |
| Field selection | `--json <field1,field2>` (use `--json list` or `--json ?` to discover fields) |
| Full table output (no truncation) | `--no-truncate` |
| YAML output | `-o yaml` |
| Wide table with extra columns | `-o wide` |

Default to `-o json` when working programmatically.

## Safe Mutation Workflow

Follow this sequence for any change. Skip steps only when the user explicitly
asks for speed.

1. **Verify context** — confirm which environment is targeted
2. **Read current state** — list or get the resource first
3. **Build from template** — use schemas/examples output, not hand-crafted payloads
4. **Preview** — use `--dry-run` where available before applying
5. **Apply** — create, update, or delete
6. **Verify** — re-read the resource to confirm the change landed

## Key Flags for Operations

| Intent | Flag |
|--------|------|
| Preview without changing anything | `--dry-run` |
| Target a specific context | `--context <name>` |
| Continue on errors vs stop | `--on-error fail\|ignore\|abort` |
| Control concurrency | `--max-concurrent <n>` (default 10) |

## Resource Operations

The `gcx resources` group handles CRUD for Grafana's K8s-tier resources:
- `get` — list or fetch resources
- `push` — create or update from local files
- `pull` — export resources to local files
- `delete` — remove resources
- `edit` — edit resources interactively
- `validate` — validate local files against a live instance
- `schemas` — discover available resource types and their schemas
- `examples` — get example manifests for resource types

All resource commands accept selectors: `gcx resources get dashboards`,
`gcx resources get dashboards/my-dash`, `gcx resources get dashboards folders`.

## Datasource Queries

The `gcx datasources` group provides typed query interfaces:
- `list` / `get` — discover available datasources
- `prometheus` — PromQL queries (query, labels, metadata, targets)
- `loki` — LogQL queries (query, labels, series)
- `pyroscope` — profiling queries
- `tempo` — trace queries
- `generic` — auto-detect datasource type

Use `gcx datasources <type> --help` to discover type-specific flags.

## Grafana Assistant

gcx provides direct access to the Grafana Assistant — use it for **reasoning
and exploration**, not data retrieval. Deterministic commands are faster and
cheaper for known queries; the Assistant adds value when you need intelligence.

| Situation | Use | Example |
|-----------|-----|---------|
| You know the exact query | Deterministic command | `gcx metrics query 'rate(http_requests_total[5m])'` |
| You don't know which metrics/labels exist | `gcx assistant prompt` | `"What metrics exist for the checkout service?"` |
| You need cross-signal root cause analysis | `gcx assistant investigations` | Multi-agent parallel exploration across metrics, logs, traces, profiles |
| You need a precise, repeatable data point | Deterministic command | `gcx slo definitions status`, `gcx alert instances list --state firing` |
| You need to understand service dependencies | `gcx assistant prompt` | `"How are services in namespace X connected?"` — Assistant Memories know your stack topology |

### Commands

```bash
# Ask a question (Assistant uses Infrastructure Memories for context)
gcx assistant prompt "What services are unhealthy in namespace checkout?"

# Follow up on the previous conversation
gcx assistant prompt "Dig into the database connection issue" --continue

# Launch a deep investigation (multi-agent, parallel signal exploration)
gcx assistant investigations create --title="Checkout latency spike"

# Monitor and read results
gcx assistant investigations timeline <id>
gcx assistant investigations report <id>
```

### Recommended Workflow: Interleave Both

1. **Detect** with deterministic commands — `gcx alert instances list --state firing`
2. **Understand** with the Assistant — `gcx assistant prompt "Why is checkout-latency firing?"`
3. **Investigate** if needed — `gcx assistant investigations create --title="..."`
4. **Verify fix** with deterministic commands — `gcx slo definitions status <uuid>`

## Provider Commands

Product-specific providers register their own top-level command groups.
Discover them with `gcx providers`, then explore with `gcx <provider> --help`.

Each provider adds domain-specific subcommands for managing that product's
resources. The set of providers grows over time — always discover rather than
hardcode.

## Parallelism

gcx commands are stateless API calls. When multiple operations are independent
(no output dependency between them), issue them as parallel Bash tool calls in
a single message. This applies to:

- Multiple list/get calls across different resource types
- Multiple schema/example fetches
- Independent create/update operations
- Concurrent datasource queries

Only sequence commands when a later call needs output from an earlier one.

## Secret Safety

Never read raw config files — they contain plaintext tokens. Use `gcx config view`
(which redacts secrets) for inspection. When passing tokens to external tools,
use shell variables rather than inline values.

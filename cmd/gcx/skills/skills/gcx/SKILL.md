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

**Find what's available:**
- Full command catalog with metadata: `gcx commands --flat -o json`
- Explore a command group: `gcx <group> --help`
- Explore a specific command: `gcx <group> <subcommand> --help`
- List registered product providers: `gcx providers`

**Find how to build payloads:**
- JSON schema for a resource type: `gcx resources schemas <kind>`
- Example payload for a resource type: `gcx resources examples <kind>`

When intent is unclear, start with the command catalog, then drill into `--help`
for the matching group. If no command exists for the requested operation, say so
and propose the nearest supported flow.

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
| Field selection | `--json <field1,field2>` (use `--json ?` to discover fields) |
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

The `gcx datasources` group provides datasource discovery (`list`, `get`,
`query`). Signal-specific query commands are top-level groups:
- `gcx metrics` — PromQL queries and metadata (`query`, `labels`, `metadata`)
- `gcx logs` — LogQL queries and label/series discovery (`query`, `labels`, `series`)
- `gcx traces` — trace search/query commands
- `gcx profiles` — profiling query commands

Use `gcx metrics --help`, `gcx logs --help`, `gcx traces --help`, and
`gcx profiles --help` for signal-specific flags.

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

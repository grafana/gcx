# ADR-001: Codify CLI Design Principles in CONSTITUTION.md and Design Guide

**Created**: 2026-03-25
**Status**: proposed
**Bead**: grafanactl-experiments-uxu
**Supersedes**: none

## Context

grafanactl's core design principles exist in code and in developers' heads but
are not consistently codified. This causes drift — especially from agentic
contributors who lack the institutional context. The goal is to capture key
design parameters as either constitutional invariants (require human approval
to violate) or design-guide prescriptions (required for new code, adopted
incrementally for existing code).

## Document Taxonomy

| Document | Role | Audience |
|----------|------|----------|
| `CONSTITUTION.md` | Invariants — "what you must not break" | All contributors, gate for PRs |
| `docs/reference/design-guide.md` | Prescriptive UX handbook — "how to build correctly" | Implementers, code reviewers |
| `DESIGN.md` | Orientation map — "what is this project" | First-time readers, navigation |

The three documents are cross-referenced but serve distinct roles. Constitution
is short and authoritative. Design guide is long and detailed with
`[CURRENT]`/`[ADOPT]`/`[PLANNED]` status markers. DESIGN.md is a thin map.

---

## CONSTITUTION.md — New Sections

### CLI Grammar (new section after "Architecture Invariants")

```markdown
## CLI Grammar

- **Command structure follows `$AREA $NOUN $VERB`.** Resource and provider
  commands use `grafanactl {area} {resource-type} {verb}` (e.g.
  `grafanactl slo definitions list`, `grafanactl resources get`,
  `grafanactl datasources loki query`). Tooling commands (`dev`, `config`)
  may use `$AREA $VERB` when there is no meaningful noun — these operate on
  the project or CLI itself, not on Grafana resources.
- **Extension commands nest under their resource type.** Domain-specific
  operations (`status`, `timeline`, `acknowledge`) live alongside CRUD verbs,
  never as top-level commands. Extensions must not duplicate CRUD semantics —
  if it can be done with list/get/push/pull/delete, it is not an extension.
- **Positional arguments are the subject, flags are modifiers.** The thing
  being acted on (resource selectors, UIDs, expressions, file paths) is
  positional. How to act on it (output format, concurrency, dry-run, filters)
  is a flag.
```

### Dual-Purpose Design (new section)

```markdown
## Dual-Purpose Design

Every command serves both humans and agents. Agent mode switches defaults
(JSON output, no color, no truncation) but does not change available
functionality. Explicit flags always override agent mode defaults.

See [design-guide.md §6](docs/reference/design-guide.md#6-agent-mode) for
agent mode detection, behavior changes, and opt-out mechanisms.

- **All output goes through the codec system.** No command writes unstructured
  prose as its primary output. CRUD data commands output resources. CRUD
  mutation commands output structured operation summaries. Extension commands
  output domain-specific structured data.
- **Default output is proportional to what is actionable.** Mutation summaries
  enumerate exceptions (failures, skips) and summarize successes by count.
  Full per-resource detail is opt-in.
- **STDOUT is the result, STDERR is the diagnostic.** Summary tables and
  resource data go to stdout. Failure details and progress feedback go to
  stderr. Both use structured formats (tables or JSON), not unstructured prose.
```

### Push/Pull Philosophy (new section)

```markdown
## Push/Pull Philosophy

- **Local manifests are clean, portable, and environment-agnostic.** `pull`
  strips server-managed fields and writes a consistent format (default: YAML).
  `push` is idempotent (create-or-update) and treats local files as
  authoritative. The same manifests can be pushed to any Grafana instance
  via `--context` without modification.
- **Three workflows, one pipeline.** Whether used as source-of-truth (edit
  locally, push to Grafana), backup/rollback (pull periodically, push to
  restore), or migration/fanout (pull from source instance, push to targets),
  the push/pull pipeline is the same. The workflow differs only in triggering
  — manual, CI, or scheduled.
- **Folder-before-resource ordering** on push. Folders are topologically
  sorted by parent-child relationships and pushed level-by-level before
  any non-folder resources.
```

### Provider Architecture (new section)

```markdown
## Provider Architecture

- **Dual CRUD access paths are permanent.** Provider commands
  (`slo definitions list`) are ergonomic shorthands with domain-rich table
  output. Generic commands (`resources get slos.v1alpha1.slo.ext.grafana.app`)
  serve the push/pull pipeline and cross-resource operations. Neither path
  is deprecated; both are first-class.
- **JSON/YAML output is identical between both paths.** This is enforced
  structurally: provider CRUD commands must use their registered
  `ResourceAdapter` (via TypedCRUD) for data access, not raw API clients.
  Table/wide codecs may diverge — provider tables show domain-specific
  columns, generic tables show resource-management columns.
- **Typed resource trajectory.** Provider resource types are progressing
  toward implementing K8s metadata interfaces directly. TypedCRUD is a
  transitional bridge. New providers must design domain types with eventual
  K8s interface compliance in mind and must not introduce patterns that
  deepen the typed-to-unstructured gap.
```

### Dependency Rules Addition

```markdown
# Add to existing "Dependency Rules" section:

- Provider implementations must use `providers.ConfigLoader` for config and
  auth resolution. Providers must not construct HTTP clients or load
  credentials independently — this ensures consistent env var precedence,
  secret handling, and auth behavior across all providers.
```

---

## docs/reference/design-guide.md — New/Updated Sections

### New: Codec Requirements by Command Type `[ADOPT]`

Add after Section 1.3 (Default Format by Command Type):

```markdown
### 1.X Codec Requirements by Command Type `[ADOPT]`

| Command type | `text` (table) | `wide` | `json` | `yaml` | Domain-specific |
|---|---|---|---|---|---|
| CRUD data (list, get) | Required, default | Required | Built-in | Built-in | — |
| CRUD mutation (push, pull, delete) | Required, default (summary) | Required (summary) | Built-in (summary) | Built-in (summary) | — |
| Extension (status, timeline...) | Required, default | Optional | Built-in | Built-in | Optional (e.g. graph) |

All data-display and mutation commands must register a `text` table codec
and call `DefaultFormat("text")`. The `text` codec is the human default;
`json` becomes the default only in agent mode.

Codec registration happens in `setup(flags)`, not in `RunE`.
```

### New: Mutation Command Output `[ADOPT]`

Add as a new section:

```markdown
## X. Mutation Command Output `[ADOPT]`

### X.1 Summary Table

CRUD mutation commands (push, pull, delete) output a structured summary
through the codec system. The summary replaces ad-hoc `cmdio.Success/Warning`
status messages as the primary output.

**STDOUT** — summary table grouped by resource kind:

| RESOURCE KIND | TOTAL | SUCCEEDED | SKIPPED | FAILED |
|---|---|---|---|---|
| Dashboard | 2452 | 2440 | 2 | 10 |
| Folder | 48 | 48 | 0 | 0 |

**STDERR** — failures enumerated individually with error detail:

| RESOURCE | ERROR |
|---|---|
| dashboards/revenue-overview | 409 conflict: resource modified server-side |
| dashboards/checkout-funnel | 413 payload too large |

**Rules:**
- Successes are counted, never enumerated individually.
- Failures are always enumerated individually — they require action.
- Skipped resources are enumerated if count < 20, otherwise grouped.
- `cmdio.Success/Warning/Error` remain for progress feedback *during*
  execution. The summary table is the *final* output.

### X.2 JSON Summary Shape

```json
{
  "summary": [
    {"kind": "Dashboard", "total": 2452, "succeeded": 2440, "skipped": 2, "failed": 10}
  ],
  "failures": [
    {"name": "dashboards/revenue-overview", "error": "409 conflict: resource modified server-side"}
  ],
  "skipped": [
    {"name": "dashboards/archived-q3", "reason": "no changes detected"}
  ]
}
```

Verbose opt-in (`-v` or `-o wide`) adds a `"succeeded"` array for audit.
```

### New: Pull Format Consistency `[ADOPT]`

```markdown
### X.X Pull Format Consistency `[ADOPT]`

`pull` accepts a `--format` flag (values: `yaml`, `json`; default: `yaml`)
that enforces consistent file format on disk. All pulled files use the
specified format regardless of the server's response format.

Files are written as `plural.version.group/name.{ext}` where `{ext}`
matches the chosen format (`.yaml` or `.json`).
```

### New: Provider Command / Resources Pipeline Consistency `[ADOPT]`

```markdown
### X.X Provider / Resources Output Consistency `[ADOPT]`

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
```

### Update: TypedCRUD `[ADOPT -> EVOLVE]`

```markdown
### X.X TypedCRUD Pattern `[ADOPT → EVOLVE]`

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
```

### New: ConfigLoader Requirement `[ADOPT]`

```markdown
### X.X Provider ConfigLoader `[ADOPT]`

All provider commands must use `providers.ConfigLoader` for flag binding
(`--config`, `--context`) and config resolution (YAML + env var precedence).

Do not:
- Import `cmd/grafanactl/config` from provider code (import cycle)
- Roll custom flag binding for `--config`/`--context`
- Construct HTTP clients or load credentials outside ConfigLoader
- Hardcode env var names — ConfigLoader handles `GRAFANA_PROVIDER_*` resolution
```

---

## DESIGN.md — Minor Updates

- Add cross-reference to new CONSTITUTION.md sections under "Detailed
  Architecture Documentation"
- No structural changes — keep it as a thin orientation map

---

## Follow-Up Work (beads)

| # | Title | Type | Priority | Description |
|---|-------|------|----------|-------------|
| 1 | Update CONSTITUTION.md with new invariants | task | P2 | Add CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture sections and ConfigLoader dependency rule |
| 2 | Update design-guide.md with new sections | task | P2 | Add codec matrix, mutation summaries, pull format, adapter compliance, TypedCRUD status, ConfigLoader requirement |
| 3 | Update DESIGN.md cross-references | task | P3 | Add links to new constitution sections |
| 4 | Implement mutation summary table codec | feature | P3 | Replace cmdio.Success/Warning output in push/pull/delete with structured summary table (stdout) and failure detail (stderr) |
| 5 | Add --format flag to pull | feature | P3 | Enforce consistent pull output format, default yaml |
| 6 | Remove provider CRUD deprecation warnings | task | P2 | Dual paths are permanent, remove warnings from SLO/Synth provider commands |
| 7 | Audit providers for adapter compliance | task | P2 | Ensure all provider CRUD commands use ResourceAdapter, not raw clients |
| 8 | Audit providers for ConfigLoader compliance | task | P2 | Ensure no provider rolls custom config loading |

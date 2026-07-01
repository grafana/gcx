# Research: Command Surface Consistency Audit

**Created**: 2026-07-01
**Confidence**: High (derived from the generated CLI reference + code inspection)
**Sources**: 5

## Executive Summary

- Audited all ~470 leaf commands in `docs/reference/cli/` against four consistency goals: `$PRODUCT $RESOURCE $VERB` grammar, standard verbs (`list|get|create|update|delete`), the standardized `TypedCRUD` interface, and the output model (lists → table, individual items → YAML, agent/scripting → JSON).
- **Grammar** and **verb** deviations are the bulk of the findings and are mode-independent. Most are mechanical renames; a few (signal providers) are deliberate exceptions that should be ratified in `CONSTITUTION.md` rather than "fixed".
- **`TypedCRUD`** is broadly adopted; the notable bespoke holdouts are `dashboards`, `cloud stacks`, and `instrumentation`.
- **Output**: `list` → table and agent-mode → JSON (`agents` codec) are already correct. The one systemic gap is that `get` / single-item commands default to `text`/table instead of YAML — a **non-agent-mode-only** defect rooted in one design-doc rule (`docs/design/output.md` §1.3) and one shared default call, not 62 separate bugs.

This audit is the input for the pending verb-taxonomy and output-consistency implementation plans anticipated by [the UX Consistency design](../plans/2026-04-14-ux-consistency-design.md) (dimensions 4-5) and tracked under issue #387.

## Scope and Method

- Command inventory: the generated per-command reference under `docs/reference/cli/` (leaf = a command file with no `_`-prefixed children).
- Rules checked against: `CONSTITUTION.md` §CLI Grammar (authoritative grammar + verb constraints), `DESIGN.md`, `docs/design/output.md` §1.3 (default format by command type).
- `TypedCRUD` adoption determined by `grep` for `adapter.TypedCRUD[T]` usage per provider.
- Output precedence confirmed in `internal/output/format.go:81-84` (`BindFlags` overrides the per-command default with the `agents` codec when `agent.IsAgentMode()`).

### Rule clarifications used

- The codebase's canonical verb set is effectively `list | get | create | update | delete | push | pull` — CONSTITUTION treats `push`/`pull`/`delete` as first-class CRUD verbs for the GitOps tier. Findings below flag against `list|get|create|update|delete` and note where CONSTITUTION already blesses an exception.
- Output rules are **non-agent-mode** defaults. Agent mode always emits the `agents` codec (JSON, with temp-file spill above 100 KiB) regardless of command type. Precedence: explicit `-o` flag → agent-mode `agents` → per-command human default.
- Individual-item target format is **YAML**. Today only `config view` defaults to YAML; every `get` defaults to table.

## Findings

### 1. Grammar — not `$PRODUCT $RESOURCE $VERB`

| Command(s) | Problem | Suggested fix |
|---|---|---|
| `gcx api` | Bare noun, no verb; not in the closed bare-verb set (`login`,`setup`,`version`,`help`,`completion`) | Add a verb (`gcx api call ...`) or ratify as an explicit exception in CONSTITUTION |
| `gcx metrics query\|labels\|series\|metadata` | `$PRODUCT $VERB`, no resource noun; `labels`/`series`/`metadata` are nouns-as-verbs | Accepted per ADR `signal-provider-ux/001` — ratify the exception in CONSTITUTION |
| `gcx logs query\|labels\|series\|metrics` | Same | Same |
| `gcx traces query\|labels\|metrics\|get` | Same; `traces get <id>` has an implicit resource | Same |
| `gcx profiles query\|labels\|metrics\|profile-types` | Same | Same |
| `gcx assistant dashboard`, `gcx assistant prompt` | `$PRODUCT $VERB`, no resource noun | Nest under a noun, or document as a verb-first exception |
| `gcx kg open\|status\|summary` | `$PRODUCT $VERB`, bare verbs under the product | Fold under a noun (`kg graph open/status`) or ratify |
| `gcx kg diagnose service\|labels` | Inverted `$PRODUCT $VERB $NOUN` | Re-order to `kg <noun> diagnose` |
| `gcx kg meta all\|logs\|metrics\|traces\|profiles\|schema\|scopes` | Noun leaves, no verb | `kg meta <noun> get/list` |
| `gcx kg insights chart\|sources` | Noun leaves, no verb | `kg insights <noun> get/list` |
| `gcx k6 auth token` | `auth` isn't a resource | Rework (`k6 auth get-token` or config) |
| `gcx k6 test-run status\|emit\|runs list` | `test-run` isn't a CRUD resource; mixed verb/noun leaves | Clarify the resource model |
| `gcx datasources prometheus labels\|metadata`; `... loki labels\|series\|metrics`; `... tempo labels\|metrics`; `... pyroscope labels\|metrics\|profile-types`; `... influxdb field-keys\|measurements\|tag-keys\|tag-values` | Trailing noun stands in for a verb (`$PRODUCT $RESOURCE $NOUN`) | Treat as sub-resources with `list`, or ratify as a query-discovery exception |

### 2. Non-standard verbs — should be `list|get|create|update|delete`

| Command(s) | Current verb | Should be |
|---|---|---|
| `gcx datasources cloudwatch list-namespaces\|list-metrics\|list-dimensions\|list-regions\|list-accounts` | `list-X` compound | `cloudwatch <noun> list` |
| `gcx datasources athena list-catalogs\|list-databases\|list-tables` | `list-X` | `athena <noun> list` |
| `gcx datasources clickhouse list-tables` | `list-X` | `clickhouse tables list` |
| `gcx appo11y services list-operations` | `list-X` | `services operations list` |
| `gcx irm oncall alert-groups list-alerts` | `list-X` | `alert-groups alerts list` |
| `gcx config list-contexts` | `list-X` | `config contexts list` (tooling — lower priority) |
| `gcx datasources clickhouse describe-table`, `... athena describe-table` | `describe-X` | `get` |
| `gcx kg entities inspect` | `inspect` | `get` |
| `gcx logs adaptive patterns show`; `gcx metrics adaptive recommendations show`; `gcx traces adaptive recommendations show` | `show` | `get` |
| `gcx frontend apps show-sourcemaps` | `show-X` | `frontend apps sourcemaps list` |
| `gcx alert templates upsert` | `upsert` (only one in tree) | `create` / `update` |
| `gcx alert notification-policies set\|reset` | `set`/`reset` | `update` (singleton) |
| `gcx alert contact-points export`, `... mute-timings export`, `... notification-policies export` | `export` | fold into `get`/`pull` with a flag |
| `gcx aio11y saved-conversations save` | `save` | `create` |
| `gcx k6 load-tests update-script` | `update-script` | flag on `update` |
| `gcx aio11y collections conversations add\|remove` | `add`/`remove` | `create`/`delete` (or accept as membership ops) |
| `gcx frontend apps apply-sourcemap\|remove-sourcemap` | compound verbs | `frontend apps sourcemaps apply\|delete` |
| `gcx instrumentation clusters configure\|remove`, `... clusters apps configure\|remove` | `configure`/`remove` | `create`/`update`/`delete` |
| `gcx instrumentation services include\|exclude\|clear` | 3 domain verbs | `update`/`delete` (or document as a wizard exception) |
| `gcx irm incidents open\|close`, `... incidents activity add` | `open`/`close`/`add` | `create`/`update`; `activity create` |
| `gcx irm oncall alert-groups acknowledge\|unacknowledge\|resolve\|unresolve\|silence\|unsilence`, `gcx irm oncall escalate` | 7 state-transition verbs | Defensible as extensions per CONSTITUTION — verify each isn't expressible as `update`; note oncall has no CRUD at all |

### 3. TypedCRUD gaps

| Command group | Problem | Suggested fix |
|---|---|---|
| `gcx dashboards create\|update\|delete\|get\|list` | Bespoke CRUD, no `TypedCRUD[T]` (highest-value resource) | Migrate to TypedCRUD |
| `gcx cloud stacks create\|update\|delete\|get\|list` | Bespoke CRUD | Migrate to TypedCRUD |
| `gcx instrumentation clusters\|clusters apps\|services *` | Bespoke + non-standard verbs; doesn't fit the typed pattern | Rework toward TypedCRUD or document exception |
| `gcx alert rules` (get/list only), `gcx alert instances` (list only) | Read-only while sibling `contact-points`/`mute-timings` are full CRUD — asymmetry | Confirm intended (likely API read-only) |
| Most of `gcx irm oncall *`; `gcx aio11y templates\|generations\|conversations\|scores` | Read-only where CRUD might be expected | Confirm intended |

### 4. Output model — non-agent-mode only (agent mode already forces JSON via the `agents` codec)

| Command(s) | Problem | Suggested fix |
|---|---|---|
| **All `gcx <product> <resource> get`** (~62) + singleton getters (`appo11y overrides/settings get`, `alert notification-policies get`, …) | Default to `text`/table; should be YAML for individual items. Root cause: `output.md` §1.3 groups `get` with `list`, and commands call `DefaultFormat("text")` | Change `output.md` §1.3 (`get` → `yaml`); switch single-item commands to `DefaultFormat("yaml")`. One coordinated change, not 62 edits |
| **All `gcx <product> <resource> list`** | Default to `text`/table — correct | None |
| `gcx * query` (metrics/logs/traces/profiles + all `datasources * query`) | Non-tabular payload (series/streams/traces); table shape is synthetic | Confirm `-o json/yaml` is stable in both modes |
| `gcx assistant prompt\|chat`, `... investigations narrative\|report\|document\|regenerate-report`, `gcx kg summary` | Prose/streaming, not resource-shaped | Confirm/limit format support |
| `gcx dashboards snapshot` (PNG), `gcx kg open`, `gcx assistant dashboard` (deep-link), `gcx k6 test-run emit` | Binary / side-effecting, no data payload | Exempt explicitly |
| `gcx metrics adaptive recommendations diff` | Diff output, not codec-driven | Exempt explicitly |

Compliant (recorded so they are not re-litigated): `list` → table; `json`/`yaml`/`agents` free on all data commands; agent-mode JSON override.

## Recommendations

Highest-leverage, in order:

1. **`list-X` → `X list`** across datasources (cloudwatch/athena/clickhouse), `appo11y services`, `irm oncall alert-groups`. Pure mechanical grammar win, low risk.
2. **Normalize get-synonyms**: `describe-table`/`show`/`inspect` → `get`; `upsert`/`save`/`set` → `create`/`update`; `export` → `get`/`pull` with a flag.
3. **Ratify the signal-provider exception** in CONSTITUTION (already ADR'd in `signal-provider-ux/001`) so `metrics query`-style commands don't read as violations.
4. **Reconcile instrumentation's verb vocabulary** (`configure`/`remove`/`include`/`exclude`/`clear`) with standard CRUD, or document it as a deliberate wizard-style exception.
5. **`get` → YAML (human mode)**: edit `output.md` §1.3 and switch single-item commands to `DefaultFormat("yaml")`. Decide first whether `get` with multiple positional IDs defaults YAML (multi-doc `---` stream) too — cleanest is "one command, one default".
6. **Migrate `dashboards` and `cloud stacks` to TypedCRUD**.

These map to dimensions 4-5 of [the UX Consistency design](../plans/2026-04-14-ux-consistency-design.md), which anticipated separate implementation plans for verb taxonomy and output consistency.

## Sources

1. `docs/reference/cli/` — generated per-command CLI reference (command inventory).
2. `CONSTITUTION.md` §CLI Grammar — authoritative grammar and verb constraints.
3. `docs/design/output.md` §1.3 — default format by command type.
4. `internal/output/format.go:81-84` — agent-mode output override precedence.
5. `docs/adrs/signal-provider-ux/001-cross-signal-command-consistency.md` — signal-provider command exceptions.

# Output Contract

> Defines the rules for command output: codecs, status messages, JSON field selection, codec requirements by command type, mutation command summaries, and pull format consistency.

Reference alongside [cli-layer.md](../architecture/cli-layer.md) for command structure and [patterns.md](../architecture/patterns.md) for architectural patterns.

---

## 1. Output Contract

### 1.1 Built-in Codecs

Every command gets `json`, `yaml`, and `agents` output for free via `io.Options`.
The `json` and `yaml` codecs produce the full resource object as returned by
the API — no envelope wrapping, no field filtering. This output is stable.
The `agents` codec is described in [§ 1.1.1](#111-agents-codec) below.

```go
ioOpts := &io.Options{}
ioOpts.BindFlags(cmd.Flags())
```

#### 1.1.1 Agents Codec

The `agents` codec is optimised for AI-agent contexts. It emits compact JSON
(no indentation, no HTML escaping) when the serialised payload is within the
spill threshold (default **100 KiB**), and spills to a temp file otherwise.

**Below threshold** — output is compact JSON identical in content to `-o json`.

**Above threshold** — the full payload is written to
`$TMPDIR/gcx-results-<random>.json` and a short summary is printed to stdout:

```json
{
  "spilled_to": "/tmp/gcx-results-3781234567.json",
  "bytes": 143200,
  "total_items": 312,
  "preview_sample": [ { ... }, { ... }, { ... } ],
  "message": "Response too large for stdout (143200 bytes). Full data written to ..."
}
```

| Field | Always present | Description |
|-------|---------------|-------------|
| `spilled_to` | yes | Absolute path to the full-payload file |
| `bytes` | yes | Byte size of the full payload |
| `total_items` | only for lists | Element count — named `total_items` (not `items`) to avoid collision with the k8s list `items` array shape |
| `preview_sample` | yes | First 3 items for list shapes; sorted top-level key names for object/map shapes; `null` for other shapes. Named `preview_sample` (not `preview`) to signal it is never the complete dataset |
| `message` | yes | Human-readable guidance: references `spilled_to` path and opt-outs |

**Override:** `-o json` forces full compact JSON to stdout regardless of size.
`-o text` renders the human table.

**Threshold configuration:** `GCX_AGENT_SPILL_BYTES` (int, bytes; default
`102400`). Invalid or missing values fall back to the default.

**Guidance for provider authors:** Do **not** pre-truncate output for agent
mode. The codec handles oversized payloads. Pre-truncation defeats the spill
mechanism because agents that need the full data can no longer retrieve it.

**Implementation:** `internal/output/agents.go`

### 1.2 Custom Codecs

Commands register additional formats (e.g. `text`, `wide`, `graph`) via
`io.Options.RegisterCustomCodec()`. The `text` codec is a Kubernetes-style
table printer (`k8s.io/cli-runtime/pkg/printers.NewTablePrinter`).

```go
ioOpts.RegisterCustomCodec("text", myTableCodec)
ioOpts.DefaultFormat("text")   // makes "text" the default instead of "json"
```

**Data fetching is format-agnostic.** Commands must fetch all available data
in `RunE` regardless of the `--output` value. The output format controls
**presentation**, not **data acquisition**. Table/wide codecs select which
columns to render; the built-in JSON/YAML codecs serialize the full data
structure. Do not gate data fetches on `opts.IO.OutputFormat` — this causes
JSON/YAML to silently omit fields. See Pattern 13 in `patterns.md`.

### 1.3 Default Format by Command Type

| Command type | Default format | Rationale |
|-------------|---------------|-----------|
| `list`, `get` | `text` (with table codec) | Human-scannable |
| `config view` | `yaml` | Config is YAML-native |
| `push`, `pull`, `delete` | Status messages only | Operations, not data |
| Agent mode ([agent-mode.md](agent-mode.md)) | `agents` | Token-efficient: compact JSON below 100 KiB, temp-file spill above (see [§ 1.1.1](#111-agents-codec)) |

When building a new command: call `ioOpts.DefaultFormat("text")` for data
display commands and register a table codec. Don't leave `json` as the default
for interactive commands.

### 1.4 Status Messages

Use the `cmdio` functions for operation feedback — they use Unicode symbols
and respect `color.NoColor`:

```go
cmdio.Success(cmd.OutOrStdout(), "Pushed %d resources", count)  // ✔
cmdio.Warning(cmd.OutOrStdout(), "Skipped %d resources", count) // ⚠
cmdio.Error(cmd.OutOrStdout(), "Failed %d resources", count)    // ✘
cmdio.Info(cmd.OutOrStdout(), "Using context %q", ctx)          // 🛈
```

Status messages go to stdout. Errors (via `DetailedError`) go to stderr.

Reference: `internal/output/messages.go`

### 1.5 JSON Field Selection

The `--json` flag selects specific fields from output objects. When provided,
output is always JSON regardless of the `--output` default.

```bash
# Select specific fields from a single resource
gcx resources get dashboards/my-dash --json metadata.name,spec.title

# List operation: output is {"items": [...]}
gcx resources get dashboards --json metadata.name

# Discover available field paths for a resource type
gcx resources get dashboards/my-dash --json ?
```

**Flag semantics:**

| Value | Behavior |
|-------|----------|
| `--json field1,field2` | Emit JSON with only those fields; missing fields produce `null` |
| `--json ?` | Print available field paths (one per line, sorted) and exit 0 |
| `--json` + `-o json` | Allowed — both request JSON, no conflict |
| `--json` + `-o <non-json>` | Usage error — field selection requires JSON output |

**Field path syntax:** Dot-notation resolves nested fields. `metadata.name`
extracts `metadata → name`. Top-level keys and `spec.*` sub-keys are enumerated
by `--json ?`. Field discovery introspects a sample object from the API — no
additional list calls are made (NC-005).

**Output shape:**
- Single resource: `{"field": "value", ...}` (flat object, only selected fields)
- List/collection: `{"items": [{"field": "value"}, ...]}`

**Backward compatibility:** `-o json` is unchanged — it still produces the full
resource object. `--json` is an independent mechanism (NC-002).

**Implementation:** `internal/output/field_select.go` (`FieldSelectCodec`,
`DiscoverFields`). Flag parsing and mutual-exclusion enforcement in
`internal/output/format.go` (`applyJSONFlag`).

### 1.6 JQ Transformation

The `--jq` flag applies a [jq](https://jqlang.github.io/jq/) expression to the
command's JSON output before it reaches stdout. This eliminates the need to
pipe gcx output into external scripts for grouping, reducing, or filtering — a
common pain point when agents drive investigations and resort to generated
Python.

```bash
# Count items
gcx resources get dashboards --jq '.items | length'

# Extract names
gcx resources get dashboards --jq '.items[] | .metadata.name'

# Reshape into a custom collection
gcx resources get dashboards --jq '[.items[] | {name: .metadata.name, title: .spec.title}]'
```

**Flag semantics:**

| Value | Behavior |
|-------|----------|
| `--jq '<expr>'` | Run the jq expression against the full JSON output |
| `--jq` + `-o json` (or `-o` unset) | Allowed; auto-flips to JSON when `-o` is unset |
| `--jq` + `-o <non-json>` | Usage error — jq operates on JSON input |
| `--jq` + `--json ...` | Usage error — jq supersedes field selection |
| Invalid jq expression | Validation error (syntax fails fast at flag parse time) |

**Output shape:** Real-jq-compatible NDJSON. Each yielded value is
pretty-printed JSON on its own line, so filters like `.items[]` stream one
object per line — matching what users intuit from real `jq`. Empty result sets
emit nothing.

**Relationship to `--json`:** `--jq` strictly subsumes `--json` field
selection. Combining the two is rejected to keep the model simple — anything
`--json field1,field2` does, `{field1: .field1, field2: .field2}` does in jq.

**Agents codec:** `--jq` bypasses the agents codec's spill-to-tempfile
behavior. A caller using `--jq` wants the transformed results in-stream, not a
"spilled to /tmp" summary.

**Implementation:** `internal/output/jq.go` (`JQCodec`). Flag parsing and
mutual-exclusion enforcement in `internal/output/format.go` (`applyJQFlag`).
Library: [`github.com/itchyny/gojq`](https://github.com/itchyny/gojq).

---

## 11. Codec Requirements by Command Type

| Command type | `text` (table) | `wide` | `json` | `yaml` | Domain-specific |
|---|---|---|---|---|---|
| CRUD data (list, get) | Required, default | Required | Built-in | Built-in | — |
| CRUD mutation (push, pull, delete) | Required, default (summary) | Required (summary) | Built-in (summary) | Built-in (summary) | — |
| Extension (status, timeline...) | Required, default | Optional | Built-in | Built-in | Optional (e.g. graph) |

All data-display and mutation commands must register a `text` table codec
and call `DefaultFormat("text")`. The `text` codec is the human default;
`agents` becomes the default in agent mode (compact JSON with spill — see
[§ 1.1.1](#111-agents-codec)).

Codec registration happens in `setup(flags)`, not in `RunE`.

---

## 12. Mutation Command Output

### 12.1 Summary Table

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

### 12.2 JSON Summary Shape

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

---

## 13. Agent-Mode Output Contract

When agent mode is active:
1. No Unicode TUI box characters in any string field of JSON output.
2. Non-format presentation properties (color, truncation, charset) suppressed across all formats.
3. `--json ?` and `--json list` are both valid sentinels for field discovery; both force OutputFormat to `json` so the discovery path is reached even for table-default commands.

---

## 14. Pull Format Consistency

`pull` accepts a `--format` flag (values: `yaml`, `json`; default: `yaml`)
that enforces consistent file format on disk. All pulled files use the
specified format regardless of the server's response format.

Files are written as `plural.version.group/name.{ext}` where `{ext}`
matches the chosen format (`.yaml` or `.json`).

---

## 15. List Truncation Contract [PROPOSED — #387 Track C]

> **Status: proposed.** Implemented as an opt-in shared contract in
> `internal/output/listmeta.go` and migrated to two exemplar commands
> (`datasources list`, `irm oncall alert-groups list`).
> Not yet a repo-wide requirement; see
> `docs/research/2026-07-17-global-limit-investigation.md` for the migration
> plan and open questions.

List commands must never truncate silently. All truncation flows through the
shared helpers in `internal/output/listmeta.go` — do not roll per-command
hint strings or ad-hoc slicing.

### 15.1 The `--limit` flag

Uncapped list commands register `--limit` through the shared binder:

```go
opts.IO.BindListLimit(flags, &opts.Limit, "<subject>", <default>)
```

which produces exactly this wording and rejects negative values via
`Options.Validate()`:

```
Maximum number of <subject> to return. 0 means all results are returned
```

**Capped-source exception:** commands whose fetch is bounded by a client-side
safety cap (e.g. `irm oncall alert-groups list`, capped at 1000) must NOT use
the binder — "0 means all" would be dishonest there. They keep a bespoke flag
description that discloses the cap, and disclose the cap at runtime via
`ListMeta.Cap` plus the cap-variant hint (below). `--limit 0` on a capped
source means "as much as the cap allows", and the output must say so.

The binder is deliberately minimal: commands still pass the limit to their
clients for server-side pushdown where the API supports it.

### 15.2 Machine-readable payload signal: `list_meta`

`list_meta` is a **reserved envelope key**. A truncated page carries it in
the items envelope; **absence means the output is the complete result set**:

```json
{
  "items": [ ... ],
  "list_meta": {"truncated": true, "returned": 50, "continue": "gcx ... list --limit 100"}
}
```

Fields (`internal/output.ListMeta`):

| Field | Presence | Meaning |
|---|---|---|
| `truncated` | always (when attached) | Always `true`; a `list_meta` is only attached to partial pages |
| `returned` | always | Items in this page |
| `total` | only when observed | Size of the complete set — never guessed. Fully-fetched sources, or a paginated source whose pagination happened to end (drained) while trimming to the bound |
| `cap` | only when the safety cap was the bound | The cap value; raising `--limit` cannot retrieve more |
| `continue` | when a runnable continuation exists | Command derived from the real invocation argv (filters survive); empty for cap-bounded pages |

Attach it to the envelope struct with exactly this key and `omitempty`
(required — a `null list_meta` on complete sets would defeat the
absence-means-complete rule and confuse the discovery path):

```go
ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
```

Bare-array list outputs (no envelope) cannot carry the signal; they get the
stderr hint only and should migrate to an envelope when their consumers can
absorb the shape change (`alert rules list` is the tracked example).

### 15.3 Constructors by source shape

Never drain a source just to count it. Pick the constructor matching the
source, then finalize with `AttachListMeta(meta, os.Args)`:

| Source shape | Helper | Total |
|---|---|---|
| Cheaply complete (no server-side limit; full set already fetched) | `TruncateCompleteList(items, limit)` | observed |
| Paginated, API reports more-pages (continue token / next cursor) | `PagedListMeta(returned, limit, serverHasMore, safetyCap)` | unknown |
| Paginated, no more-pages signal | over-fetch by one (`limit+1` on the wire), then `TruncatePagedList(items, limit)` | unknown |

`PagedListMeta` honors `serverHasMore` **even when `limit <= 0`**: a fetch
bounded by a safety cap is still a partial page. This is the fix for the
PR988 defect where `--limit 0` silently returned a hard-capped page as if it
were complete. `serverHasMore` must also be true when the final page
**overshot** the bound and in-hand items were trimmed, even without a next
cursor — dropped items are truncation evidence. In that drained-overshoot
case the total was genuinely observed, and the command may attach it to the
constructed meta when honest (always on a cap-bounded page; otherwise only
when `--limit 0` can really retrieve it) — observed, never guessed
(`irm oncall alert-groups list` is the reference implementation).

The cap-recording rule fires at `limit >= safetyCap`, including
`limit == safetyCap` exactly: a doubled `--limit` continuation could never
return more than the cap allows, so the cap variant (refine filters, no
continuation) is used there.

### 15.4 Human-readable stderr hint

Emit via `EmitListTruncationHint(cmd.ErrOrStderr(), meta)` after the payload
encode, with `meta` finalized by `AttachListMeta` — the single derivation
point for the continuation, so the stderr hint and the payload's
`list_meta.continue` can never disagree. It routes through `EmitHint` (agent
mode emits the JSONL `class:"hint"` form with the continuation in `command`).
No-op when `meta` is nil. TTY templates:

```
hint: showing first 5 of 219. See all results with: gcx datasources list --limit 0          # total known
hint: showing first 50; more results are available. See more with: gcx ... list --limit 100  # total unknown (doubled limit)
hint: showing first 1000 (safety cap). Refine filters to narrow the result set               # cap was the bound
```

Rules baked into the helper:

1. The continuation command is derived from the real argv with any prior
   `--limit` stripped — the user's filter flags always survive. Never a
   hardcoded string.
2. `--limit 0` is only suggested when the total was observed (the full set is
   genuinely retrievable). Unknown totals get a doubled limit — never a
   promise that `--limit 0` retrieves everything when a cap may exist.
3. The cap variant suggests no `--limit` at all: a bigger limit cannot beat
   the cap.

### 15.5 `--json` guarantees

The reserved key is transparent to field selection and discovery
(`internal/output/field_select.go`, `format.go`):

- `--json field1,field2` on a truncated envelope selects from the **items**
  and **re-attaches** `list_meta` to the output — the truncation signal
  survives selection.
- `--json list` / `--json ?` discovery samples the first item; `list_meta.*`
  paths are never listed, and the reserved field on the envelope struct does
  not break empty-envelope discovery.

Only the reserved `list_meta` key gets this treatment; envelopes with other
extra keys keep the pre-existing selection behavior.

These guarantees hold for typed envelope structs and for envelopes assembled
as dynamic `map[string]any` values — for dynamic maps, the reserved key
itself is the opt-in signal: a map without `list_meta` keeps whole-object
selection and as-is discovery even when it happens to be items-shaped, so raw
passthrough payloads (`gcx api`) are unaffected by the reservation. Dynamic
maps may hold native Go values (a `*ListMeta`, a typed item slice) — envelope
handling JSON-normalizes the map first, so producers don't have to
pre-flatten to the JSON-decoded representation. An empty dynamic envelope has
no element type to reflect on, so discovery degrades to the envelope's own
keys (never `list_meta.*`). `unstructured.UnstructuredList` values are not
part of the contract yet — no producer attaches truncation metadata to
unstructured lists; that lands with the resources-pipeline migration (see the
research doc's remaining-migration section).

### 15.6 Reference migrations

- `cmd/gcx/datasources/list.go` — cheaply complete source, binder,
  default `--limit 0`, known total.
- `internal/providers/irm/oncall_commands_extra.go` (alert-groups list) —
  paginated source, both server-reported (`PagedListMeta` with safety cap)
  and over-fetch-by-one (`TruncatePagedList`, alternate-implementation
  fallback path) variants.

`alert rules list` is deliberately not migrated yet: its JSON/YAML output is
a bare array (no envelope to carry `list_meta`) and its `--limit` counts
different units per format (flattened rules in the table, groups in JSON).
The envelope and unit decisions are tracked in
`docs/research/2026-07-17-global-limit-investigation.md` §6–7.

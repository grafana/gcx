# Output Contract

> Defines the rules for command output: codecs, status messages, JSON field selection, codec requirements by command type, mutation command summaries, and pull format consistency.

Reference alongside [cli-layer.md](../architecture/cli-layer.md) for command structure and [patterns.md](../architecture/patterns.md) for architectural patterns.

---

## 1. Output Contract

### 1.1 Built-in Codecs

Commands using `io.Options` get `json`, `yaml`, and `agents` codecs; commands
with a different declared protocol may restrict them (see § 11). JSON/YAML
serialize the value passed to `Encode`, without adding a resource envelope.
Commands supply their registered resource or domain response according to
[Pattern 17](../architecture/patterns.md#17-k8s-envelope-wrapping-for-provider-listget).
Explicit `--json`/`--jq` may transform that value; the default preserves all
fields. The `agents` codec is described in [§ 1.1.1](#111-agents-codec).

```go
ioOpts := &io.Options{}
ioOpts.BindFlags(cmd.Flags())
```

#### 1.1.1 Agents Codec

The `agents` codec is optimised for AI-agent contexts. It emits compact JSON
(no indentation, no HTML escaping) when the serialised payload is within the
spill threshold (default **100 KiB**), and spills to a temp file otherwise.

**Below threshold** — output is compact JSON, content-equivalent to `-o json`
(NOT byte-identical: `-o json` is indented, the agents codec is compact).

**Above threshold** — the full payload is written to
`$TMPDIR/gcx-results-<random>.json` and a short summary is printed to stdout:

```json
{
  "type": "gcx.spill_reference",
  "schema_version": "1",
  "spilled_to": "/tmp/gcx-results-3781234567.json",
  "bytes": 143200,
  "content_format": "json",
  "total_items": 312,
  "preview_sample": [ { ... }, { ... }, { ... } ],
  "message": "Response too large for stdout (143200 bytes). Full data written to ..."
}
```

| Field | Always present | Description |
|-------|---------------|-------------|
| `type` | yes | Fixed discriminator `gcx.spill_reference` — the receipt shape differs from the domain result, so consumers dispatch on this marker instead of heuristics |
| `schema_version` | yes | Version of the receipt shape itself (currently `1`) |
| `content_format` | yes | `json` for documents; `jsonl` for jq streams (see § 1.6) |
| `spilled_to` | yes | Absolute path to the full-payload file |
| `bytes` | yes | Byte size of the full payload |
| `total_items` | only for lists | Element count — named `total_items` (not `items`) to avoid collision with the k8s list `items` array shape |
| `total_values` | only for jq streams | Yielded-value count, not list elements; replaces `total_items` |
| `preview_sample` | yes | First 3 items for list shapes; sorted top-level key names for object/map shapes; `null` for other shapes and jq streams. Named `preview_sample` (not `preview`) to signal it is never the complete dataset |
| `message` | yes | Human-readable guidance: references `spilled_to` path and opt-outs |

**Override:** `-o json` forces the full document inline to stdout regardless
of size (standard indented JSON — see the byte-identity note above).
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
| `list`, `get` | a narrow table codec — `text` or `table` | Human-scannable |
| `config view` | `yaml` | Config is YAML-native |
| `push`, `delete` | a structured summary (see [§ 12](#12-mutation-command-output)) | Operations, not data |
| the `pull` family | `json` (pinned; selects the *file* format, see [§ 14](#14-pull-format-consistency)) | Files are the output |
| Agent mode ([agent-mode.md](agent-mode.md)) | `agents` | Token-efficient: compact JSON below 100 KiB, temp-file spill above (see [§ 1.1.1](#111-agents-codec)) |

**There is no repo-wide format set.** `text` and `table` are both sanctioned
names for the narrow table codec and both ship today; `agents` is the agent-mode
default for display commands but is *rejected* by the artifact family, which
writes files the pipeline reads back. Some `get` commands legitimately default
to `yaml` with no table codec at all.

When building a new command: register a narrow table codec and make it the
default with `ioOpts.DefaultFormat(...)`, using **the same name its siblings in
the same command area already use**. Don't leave `json` as the default for
interactive commands, and don't rename a released format to make a checklist
uniform. To find what any command actually supports, read its own `-o` line:

```bash
GCX_AGENT_MODE=false gcx <command> --help | grep -- '-o, --output'
```

The prefix prevents agent detection from replacing a display command's human
default in help. Commands with pinned formats keep their own defaults.

### 1.4 Status Messages

Use the `cmdio` functions for operation feedback — they use Unicode symbols
and respect `color.NoColor`:

```go
cmdio.Success(cmd.ErrOrStderr(), "Pushed %d resources", count)  // ✔
cmdio.Warning(cmd.ErrOrStderr(), "Skipped %d resources", count) // ⚠
cmdio.Error(cmd.ErrOrStderr(), "Failed %d resources", count)    // ✘
cmdio.Info(cmd.ErrOrStderr(), "Using context %q", ctx)          // 🛈
```

**Status messages go to stderr**, per
[CONSTITUTION.md § Output](../../CONSTITUTION.md) — stdout is the result, stderr
is the diagnostic. A status line on stdout lands inside a caller's redirected
output. Errors (via `DetailedError`) also go to stderr.

The command's declared protocol matters: interactive prompts and server startup
messages can be the result themselves, while `EmitArtifactResult` writes a
structured receipt to stdout (see § 12). Check
`cmd/gcx/root/testdata/output_classes.json` before changing these paths.
Existing finite-command no-data notices on stdout, such as those in SLO status
and Synth checks status, are known nonconforming behavior, not examples to copy.

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
- Provider list envelope: the wrapper key is preserved. A single-key envelope
  (`{"datasources": [...]}`) is detected structurally. A multi-key envelope
  that carries list-level metadata alongside the items (e.g.
  `{"investigations": [...], "total": 42}`) must opt in by implementing
  `output.ListEnvelope` (`ListItemsKey() string` — satisfied structurally, so
  result types need no import). Selection applies per item under the declared
  key; metadata siblings pass through unchanged. `--json ?` discovers
  item-level fields for both shapes (reflecting on the declared slice field
  when the list is empty), and the agent-mode spill summary previews and
  counts the envelope's items. Detail objects that merely contain a nested
  array are never treated as envelopes — descent is explicit, not heuristic.

**Backward compatibility:** `-o json` is unchanged — it still produces the full
resource object. `--json` is an independent mechanism (NC-002).

**Implementation:** `internal/output/field_select.go` (`FieldSelectCodec`,
`DiscoverFields`). Flag parsing and mutual-exclusion enforcement in
`internal/output/format.go` (`applyJSONFlag`).

### 1.6 JQ Transformation

`--jq` transforms the full command result **before** formatting or spilling,
never a spill receipt.

```bash
# Count contexts, with compact/spill handling
gcx config list-contexts --jq '.contexts | length' -o agents

# Yield one value per dashboard
gcx resources get dashboards --jq '.items[] | .metadata.name'
```

| Combination | Behavior |
|-------------|----------|
| Bare `--jq` or `-o json --jq` | Pretty-printed JSON values, no spill, including in agent mode |
| `-o agents --jq` | Compact JSONL, without HTML escaping, with aggregate spilling |
| Other `-o` formats or `--json` with `--jq` | Rejected |
| Invalid syntax | Validation error before command execution |
| Empty/whitespace expression or unknown function | jq evaluation error |

**Shape:** Zero values emit nothing; one or many values (including primitives)
remain separate, never implicitly wrapped in an array. A yielded `null` emits
`null\n`. Pretty-printed JSON can span lines; only agents output is JSONL.

**Spilling with explicit `-o agents`:**
- `GCX_AGENT_SPILL_BYTES` (default 100 KiB) limits the **entire transformed
  stream**, including newlines—not each value or the original payload.
- At or below the threshold, stdout gets the complete stream. Above it, the
  same bytes go to one `$TMPDIR/gcx-results-<random>.jsonl` file; stdout gets
  only a spill receipt, and stderr gets a hint.
- The receipt uses `content_format: "jsonl"`, `total_values`, no `total_items`,
  and `preview_sample: null` to avoid unbounded previews. Its fixed metadata
  may exceed a very small threshold. `gcx agent prune` includes JSONL spills.
- Evaluation/encoding errors discard buffered output and partial spill files;
  I/O errors propagate without an inline fallback. Bare jq and `-o json`
  retain already-emitted values on a later error.

**Implementation:** `internal/output/{format,jq,agents}.go`, using
[gojq](https://github.com/itchyny/gojq).

---

## 11. Codec Requirements by Command Type

| Command type | narrow table (`text` or `table`) | `wide` | `json` | `yaml` | Domain-specific |
|---|---|---|---|---|---|
| CRUD data — `list` | Required, default | If it adds columns | Built-in | Built-in | — |
| CRUD data — `get` | Expected, default; `yaml` where a single object reads better | If it adds columns | Built-in | Built-in | — |
| CRUD mutation (push, delete) | Required, default (summary) | If it adds columns | Built-in (summary) | Built-in (summary) | — |
| `artifact` class (the pull family) | — files are the output | — | Required, pinned default | Required | — |
| Extension (status, timeline...) | Required, default | Optional | Built-in | Built-in | Optional (e.g. graph) |

A `list` command always gets a narrow table. A `get` command usually should, but
`yaml` is a legitimate default for one object a user is about to edit — that is
what `slo definitions get` does (`agents,json,yaml`, defaulting to `yaml`), and
it is not a defect. Everything else registers a narrow table codec and makes it
the default.

The narrow codec is the human default; `agents` becomes the default in agent
mode (compact JSON with spill — see [§ 1.1.1](#111-agents-codec)).

**The `artifact` class is the exception, and it has no table at all.** Its real
output is files the push pipeline reads back, so `-o` selects the *file* format:
`resources pull` offers `json, yaml` only, pins the default with
`PinDefaultFormat`, and rejects `-o agents` (`cmd/gcx/resources/pull.go`). A
narrow table codec there would produce a file nothing can read back. See
[§ 14](#14-pull-format-consistency).

`wide` is **not** a separate obligation. Most commands register it by hand with
`RegisterCustomCodec("wide", ...)`, and that is fine. For a command built on
`Table[T]`, prefer `RegisterTableAs` (`internal/output/table.go`): it adds
`wide` when — and only when — some column is marked `WideOnly`, so the narrow
and wide renderings cannot drift apart. Either way, a command with nothing extra
to show in `wide` should not have one, and that is correct.

Codec registration happens in `setup(flags)`, not in `RunE`.

---

## 12. Mutation Command Output

### 12.1 Shared Result Family

New finite mutations encode structured results through `io.Options`. Use the
constructors in `internal/output/mutation.go` so `type` and `schema_version`
are always populated. Select the shape that fits the operation:

| Constructor | Purpose |
|---|---|
| `NewSingleMutation(action, target)` | One target; optional `changed`, `dry_run`, and `error` |
| `NewBatchMutation(action)` | Counts for the whole batch and an always-present `failures` array |
| `NewArtifactReceipt(action, format)` | Files produced, their format, counts, and failures |

Batch counts cover every matched target exactly once. Successes and skips are
counted; failures include a `target` and an `error`. Zero `skipped` is omitted.
The shared family does not add a success array for `-v` or `-o wide`.
Human codecs render the command's summary on stdout, with progress and advisory
diagnostics on stderr. Agent output includes the failure detail in its result;
a caller must not need stderr to understand the outcome. If the command has
already emitted a complete failure result, use `gcxerrors.EmittedError` to
prevent a second document and preserve the appropriate nonzero exit code.

For artifact commands whose `-o` selects the file format, pass the receipt to
`EmitArtifactResult`: it writes JSON in agent mode and invokes the command's
human renderer otherwise. Do not send the receipt through the file codec.

These shapes are not a universal replacement for released provider contracts.
IRM OnCall action envelopes and skills receipts retain their existing forms;
instrumentation's existing `MutationResult` carries additive discriminators.
See [CONSTITUTION.md § Dual-Purpose Design](../../CONSTITUTION.md#dual-purpose-design).

### 12.2 JSON Examples

A single result from `NewSingleMutation("deleted", MutationTarget{Kind:
"dashboard", Name: "revenue-overview"})`:

```json
{
  "type": "gcx.mutation",
  "schema_version": "1",
  "action": "deleted",
  "target": {"kind": "dashboard", "name": "revenue-overview"}
}
```

A populated `NewBatchMutation("pushed")` result:

```json
{
  "type": "gcx.mutation_batch",
  "schema_version": "1",
  "action": "pushed",
  "summary": {"succeeded": 2, "failed": 1, "skipped": 1},
  "failures": [
    {
      "target": {"kind": "dashboard", "name": "revenue-overview"},
      "error": "409 conflict: resource modified server-side"
    }
  ]
}
```

A populated `NewArtifactReceipt("pulled", "json")` result:

```json
{
  "type": "gcx.artifact_receipt",
  "schema_version": "1",
  "action": "pulled",
  "format": "json",
  "files": [{"path": "dashboards/revenue-overview.json", "kind": "dashboard"}],
  "summary": {"succeeded": 1, "failed": 0},
  "failures": []
}
```

---

## 13. Agent-Mode Output Contract

When agent mode is active:
1. No Unicode TUI box characters in any string field of JSON output.
2. Non-format presentation properties (color, truncation, charset) suppressed across all formats.
3. `--json ?` and `--json list` are both valid sentinels for field discovery; both force OutputFormat to `json` so the discovery path is reached even for table-default commands.

---

## 14. Pull Format Consistency

`resources pull` uses the standard `-o/--output` flag (values: `json`,
`yaml`; default: `json`) to enforce a consistent file format on disk. All
pulled files use the specified format regardless of the server's response
format.

Files are written as `plural.version.group/name.{ext}` where `{ext}` is the
chosen format name (`.json` or `.yaml`).

Because the output format doubles as the on-disk file extension and the
encoder, file-writing commands pin their default with
`Options.PinDefaultFormat`: agent mode must not flip their default to the
`agents` display codec (which would write `<name>.agents` files containing
spill-summary envelopes for large resources). `resources pull` and
`resources edit` reject an explicit `-o agents` at validation time for the
same reason. The `json` default is ratified policy (#1030 Decision 2):
CONSTITUTION.md § Push/Pull Philosophy was amended accordingly in the same
change that landed this contract.

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
safety cap must NOT use the binder — "0 means all" would be dishonest there.
They keep a bespoke flag description that discloses the cap, and disclose the
cap at runtime via `ListMeta.Cap` plus the cap-variant hint (below).
`--limit 0` on a capped source means "as much as the cap allows", and the
output must say so. No command currently takes this exception: the last one,
`irm oncall alert-groups list`, dropped its 1000-item cap in favour of a real
cursor drain (grafana/gcx#1157). Prefer draining — a cap that a caller cannot
raise makes the complete set unreachable, which is the defect that issue
reported.

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
case the total was genuinely observed and the command attaches it to the
constructed meta — observed, never guessed
(`irm oncall alert-groups list` is the reference implementation). On a capped
source the total may only be attached when it is honest to do so: always on a
cap-bounded page (which carries no continuation), otherwise only when
`--limit 0` can really retrieve it.

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
  paginated source, both server-reported (`PagedListMeta`, no safety cap:
  `--limit 0` drains every `next` cursor) and over-fetch-by-one
  (`TruncatePagedList`, alternate-implementation fallback path) variants.

`alert rules list` is deliberately not migrated yet: its JSON/YAML output is
a bare array (no envelope to carry `list_meta`) and its `--limit` counts
different units per format (flattened rules in the table, groups in JSON).
The envelope and unit decisions are tracked in
`docs/research/2026-07-17-global-limit-investigation.md` §6–7.

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

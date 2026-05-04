# Tree-Table Rendering for `traces get`

**Created**: 2026-04-30
**Status**: accepted
**Supersedes**: none

<!-- Spec: docs/specs/2026-04-29-traces-get-table-design.md -->
<!-- Spike: branch feat/traces-get-table -->

## Context

`gcx traces get <trace-id>` retrieves a single trace from Tempo and today
emits only raw OTLP JSON. JSON is the right shape for agents and scripts but
unusable at a glance for humans — there is no tree structure, no critical
path indicator, and no way to see which subtrees are detached from the rest
of the trace.

This is a gap relative to the rest of the signal-provider surface. Sibling
commands (`metrics query`, `logs query`, `traces search`/`metrics`) all use
`internal/datasources/query.RegisterCodecs` to wire up `-o table`/`-o wide`
codecs that delegate to per-response formatters in `internal/query/{loki,
tempo}/formatter.go`. `traces get` is the only signal-tier command that
bypasses this pipeline — `internal/datasources/tempo/get.go` calls
`opts.IO.DefaultFormat("json")` directly, so `*tempo.GetTraceResponse` has
no human-readable codec at all. The user has to choose between unreadable
CLI output or leaving the terminal for Grafana Explore.

Constraints on any solution:

1. **Agent-mode JSON contract is fixed.** `cmdio.Options` already forces
   JSON output when `GCX_AGENT_MODE`/`CLAUDECODE`/`CLAUDE_CODE` are set,
   regardless of the registered default. Whatever we register must layer
   under that override, not replace it.
2. **Reuse the shared codec pipeline.** Pattern 13 (format-agnostic data
   fetching) and the cross-signal SharedOpts pattern (see
   `docs/adrs/signal-provider-ux/001-cross-signal-command-consistency.md`)
   require codecs to control display, not data acquisition.
3. **Stay within "navigable summary" scope.** The non-goal is replacing
   Grafana Explore. This is a CLI summary, not a full waterfall UI.
4. **Terminal styling rules apply.** Use `style.NewTable` so output
   auto-degrades to plain `tabwriter` when stdout is piped, agent-mode is
   active, or `--no-color` is set.

The full design exploration lives in
[docs/specs/2026-04-29-traces-get-table-design.md](../../specs/2026-04-29-traces-get-table-design.md).
A working spike is on branch `feat/traces-get-table` and validates that the
shape below is buildable in ~340 LOC of formatter code without changes to
the underlying client or response types.

## Decision

We will render `gcx traces get` as a **tree-indented table with a duration
% column and a separate detached-subtrees section**. The non-agent terminal
default is flipped to `table` in this PR; agent mode and piped output
continue to receive JSON via the `cmdio.Options` override.

### What ships

#### Codec wiring

`internal/datasources/tempo/get.go` switches from a direct
`opts.IO.DefaultFormat("json")` call to
`dsquery.RegisterCodecs(&opts.IO, false)` — the same registration sibling
signal commands use. This adds three new switch-statement cases in
`internal/datasources/query/codecs.go`:

- `queryTableCodec.Encode` → `tempo.FormatTraceTable(w, resp)`
- `queryWideCodec.Encode`  → `tempo.FormatTraceWide(w, resp)`
- `queryGraphCodec.Encode` → return an explicit error
  ("graph output is not supported for individual traces; use -o
  table/wide/json")

Agent-mode override in `cmdio.Options` continues to force JSON regardless.

#### Header

A single line above the table:

```
Trace <full-trace-id>  duration: <human-dur>  spans: <N>  services: <M>[ (async tail detected)]
```

- Trace ID printed in full (32 hex chars).
- Duration is `max(end) - min(start)` across all spans (matches Grafana
  Explore's "trace duration" semantic).
- Async-tail suffix appears when any span ends >1s after the latest end of
  the attached subtree. This warns users why % values may look small on
  traces with long async fan-out.

#### Default columns (`-o table`)

```
SPAN                       SERVICE     SPAN_ID     DURATION     %
```

- **SPAN** — tree-indented span name. Connectors: `└ ` (last child),
  `├ ` (sibling), `│ ` (continuation), `  ` (terminal continuation).
- **SERVICE** — resource attribute `service.name`; missing → `-`.
- **SPAN_ID** — base64 → hex (16 chars). The OTLP wire format is base64;
  hex is the form humans (and Grafana URLs) recognize.
- **DURATION** — `endTimeUnixNano - startTimeUnixNano`, formatted at
  nanosecond precision (`500ns`, `37µs`, `504ms`, `28.65s`, `1m32s`).
  Returns `?` for `ns <= 0`.
- **%** — `span_dur / trace_total_dur * 100`, 1 decimal, right-aligned.

Children are sorted by `startTimeUnixNano` ascending. Roots are sorted the
same way.

#### Wide columns (`-o wide`)

Adds **KIND** and **START** between SERVICE and SPAN_ID:

```
SPAN                       SERVICE     KIND     SPAN_ID     START     DURATION     %
```

- **KIND** — `span.kind` with the `SPAN_KIND_` prefix stripped
  (`server`/`client`/`internal`/`producer`/`consumer`); missing → `-`.
- **START** — `span.start - trace_start`, formatted as `+0`, `+13µs`,
  `+182ms`, `+28.64s` using the same nanosecond formatter.

PARENT_ID, STATUS, and SCOPE are intentionally not added — see "Rejected"
below.

#### Detached subtrees

Spans whose `parentSpanId` is non-empty but doesn't resolve to any span in
the response are rendered in a separate section after the attached tree,
preceded by a dim divider:

```
── Detached subtrees (N) — parent span not in trace ──
```

Detached spans are common in real traces (sampling drops parents, parent
lives in a different trace). The divider tells the reader these are real
spans, not a rendering bug.

#### Color and dim rules

% column thresholds, using existing `style.ChartPalette`:

| Threshold     | Style              | Color        |
|---------------|--------------------|--------------|
| `pct >= 50`   | bold red           | `#E24D42`    |
| `pct >= 10`   | yellow             | `#EAB839`    |
| `pct >= 1`    | default fg         | —            |
| `pct < 1`     | dim — *whole row*  | `ColorMuted` |

Full-row dimming for negligible spans (<1%) creates the visual hierarchy
that makes the view scannable at a glance.

Spans with `status.code == STATUS_CODE_ERROR` override dim treatment: the
SPAN cell is rendered in red and the name is prefixed with `⚠ `. This makes
errors pop even when the duration share is small.

`style.NewTable` already auto-degrades to plain `tabwriter` (no ANSI, no
borders) when stdout is piped, in agent mode, or `--no-color` is set; no
custom branching needed.

#### Default format

The non-agent terminal default is flipped to `table` in this PR via
`opts.IO.DefaultFormat("table")` in `internal/datasources/tempo/get.go`.
Agent mode and piped output continue to receive JSON — `cmdio.Options`
overrides the registered default when `GCX_AGENT_MODE`/`CLAUDECODE`/
`CLAUDE_CODE` are set or when stdout is not a TTY.

### Rejected alternatives

**Flat table sorted by duration descending.** Conceptually simpler — one
row per span, sorted by duration, optional `--top N`. Rejected because
parent → child relationships are essential to "where did the time go".
Without the tree, sequential vs concurrent fan-out is invisible, and
heavy children that dominate light parents look identical to heavy
parents that dominate everything beneath them. The original problem is
not "list big spans" — it is "show structure with cost annotations".

**ASCII waterfall / Gantt bars.** Most visually proportional, mirrors
Grafana Explore's mental model. Rejected on three grounds: (a) horizontal
resolution is poor at typical terminal widths (120–160 cols), so short
spans collapse to invisible glyphs; (b) async-tail traces compress almost
everything into a one-character bar; (c) overlaps with the explicit
non-goal of replacing Grafana Explore — anyone who needs a real waterfall
should follow the deeplink to Explore, not look at ASCII bars.

**Adding PARENT_ID, STATUS, or SCOPE columns to wide.** PARENT_ID is
redundant with the visual tree for attached spans, and the detached
section divider already explains why orphan parents aren't shown. STATUS
is unset for ~95%+ of rows in most traces; errors are already surfaced via
row coloring + the `⚠` glyph. SCOPE / ATTR_COUNT add width for negligible
information density — deferred until someone asks.

**Reusing the existing `formatDuration(ms int)` from
`internal/query/tempo/formatter.go`.** That formatter assumes
millisecond-precision input and is used for search/metrics formatters.
Trace span durations span six orders of magnitude (sub-µs RPC dispatch to
multi-minute batch jobs) and need ns-precision. Adding
`formatDurationNanos(ns int64)` alongside the existing helper isolates the
two precision regimes and avoids a risky retrofit on shared code.

**Sortable / filterable flags (`--sort`, `--top-n`).** Out of scope for
v1. The view renders one full trace top-to-bottom in tree order. Sorting
breaks the tree; filtering hides cost contributions and corrupts the %
column. If users ask for it later, it's an additive flag.

## Consequences

### Positive

- `gcx traces get <id>` becomes immediately useful at a glance for
  humans. The view answers "where did time go", "what's on the critical
  path", and "what is detached" without leaving the CLI.
- `traces get` joins the shared codec pipeline (`dsquery.RegisterCodecs`),
  closing the inconsistency where it was the only signal-tier command
  registering codecs ad hoc.
- Agent-mode contract is preserved — `cmdio.Options` keeps forcing JSON
  for agents regardless of the new registration.
- Adds a reusable `formatDurationNanos(ns int64)` helper for any future
  ns-precision rendering elsewhere in the codebase.

### Negative

- ~340 LOC of new formatter code in `internal/query/tempo/formatter.go`,
  including a private `traceSpan` extractor that walks the OTLP
  `resourceSpans` shape from `map[string]any`. The OTLP shape is stable,
  but any change in how the response is parsed upstream would surface
  here.
- The async-tail threshold (>1s gap between attached-tree max-end and any
  later span) is a heuristic, not a contract. Tuned against the
  prom-query-range trace; may need re-tuning after dogfooding on more
  workloads.
- Color thresholds (50/10/1) are tuned for the same trace and may feel
  aggressive on traces where every span is genuinely on the critical
  path. Listed as a follow-up risk in the spec.
- Custom ANSI handling sits inline (`colorPct`, `colorCell`) rather than
  going through a `style` package primitive. Acceptable for a v1 narrow
  to one formatter; if a second formatter wants the same red/yellow/dim
  treatment, lift it into `internal/style`.

### Neutral / follow-up

- **Tests.** Shipped in this PR — table-driven tests in
  `internal/query/tempo/formatter_test.go` cover basic tree, detached
  subtrees, async-tail, error span, nil trace, wide columns, and
  `formatDurationNanos`. Codec dispatch tests in
  `internal/datasources/query/codecs_test.go` cover the three
  `*tempo.GetTraceResponse` codec cases.
- **Lint compliance.** Shipped in this PR — `mise run lint` passes with 0
  issues across all packages.
- **Default flip.** Shipped in this PR — non-agent terminal default is
  `table`. Agent and pipe overrides remain in place via `cmdio.Options`.
- **`--llm` flag content-type bug.** `Accept: application/vnd.grafana.llm`
  causes the response to not be JSON, but the client unconditionally
  `json.Unmarshal`s. Out of scope here; tracked as a separate item.
- **Span events / links / attributes rendering.** Out of scope.
  `-o yaml` already renders attributes; events/links are rare enough to
  defer.
- **Multi-trace rendering.** Out of scope; single-trace view only.

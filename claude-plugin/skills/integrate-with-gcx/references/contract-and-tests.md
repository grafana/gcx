# The Integration Contract and Test Plan

Contents:
1. [Why: every leaf is an agent-facing tool](#1-why-every-leaf-is-an-agent-facing-tool)
2. [The worksheet](#2-the-worksheet)
3. [Field guidance](#3-field-guidance)
4. [Agent-routing test matrix](#4-agent-routing-test-matrix)
5. [Test-plan bar](#5-test-plan-bar)
6. [gcx-native guardrails](#6-gcx-native-guardrails)

## 1. Why: every leaf is an agent-facing tool

Coding agents select and call gcx commands the way they call tools: by reading
`Use`, `Short`, `Long`, `Example`, flag help, `token_cost`, and `llm_hint` — all
of which gcx exposes verbatim through `gcx commands` and `gcx help-tree`. That
metadata is the operational contract, not decorative help text. An ambiguity in
it becomes a misroute or a malformed call that no amount of downstream error
handling can fully repair. Write the contract before the code, and get a human
to review the contract — it is much cheaper to change than a shipped command
(names are frozen within a major version).

## 2. The worksheet

Fill this in completely; "n/a" is an answer, blank is not:

```text
INTEGRATION CONTRACT — <capability>

Purpose (one sentence, exact outcome): ...
Stability: stable | experimental (experimental must be marked before release)

USE SIGNALS
  Direct:   <requests that should route here — "show me X", "list the Y">
  Indirect: <needs that imply it — "which Z is failing" implies listing Z>
  NOT for:  <adjacent requests that must route elsewhere>
  Nearest sibling: <command> — distinction: <one sentence>

COMMAND SURFACE
  Path: gcx <area> <noun> <operation>   (check docs/design/command-naming.md;
        for a discovery facet with no addressable item use <operation>-<subject>)
  Use / Short / Long / Example: <drafts — written as routing metadata>
  Args validator: <cobra.NoArgs | cobra.ExactArgs(n) | ...>  (every leaf declares one)

INPUTS (one row per positional/flag)
  name | type | constraints | default (+ why it's safe) | example | empty-value behavior

OUTPUT
  Protocol class: finite | artifact | stream | interactive | server | shell | prose | raw
  Success schema sketch: <fields + types>
  Representative agent-mode result: <one compact JSON example>

BACKEND REQUEST MAPPING
  endpoint(s) | params (which flag → which param) | pagination mechanism | auth used

COMPLETENESS
  complete | limited (--limit + list_meta) | capped by source (disclose cap)
  Constructor: TruncateCompleteList | PagedListMeta | TruncatePagedList
  Client-side filtering? <yes/no — if yes, disclose in help and plan push-down>

ERRORS (one row per expected failure)
  condition | summary-vocabulary entry | exit code | recovery suggestion (runnable) | retryable?

SIZE & COST
  Expected result size: <typical / worst case>
  token_cost: small | medium | large (+ llm_hint that teaches narrowing, required for medium/large)

BOUNDARIES
  Auth/ownership: <what gcx manages vs what the product owns>
  Reuse: <exact shared packages this must use>
  Non-goals: <explicitly out of scope>
```

## 3. Field guidance

**Routing metadata.** `Short` states what the command does in one line;
`Long` adds when to use it and when not to; `Example` shows the most common
real invocation. Follow `docs/design/help-text.md`. Check sibling commands'
vocabulary before naming flags — if the family says `--name` for substring
matching, do not introduce `--filter` for the same idea; if siblings document
"case-insensitive", match or explicitly differ. Enum values and JSON field
names should match siblings for the same concepts.

**Defaults.** Every default needs a one-line rationale: it should produce a
useful, bounded result with no extra parameters. A default that dumps an
unbounded collection is not safe; a default that silently narrows without
disclosure is not honest.

**Parameter count.** If the leaf needs more than ~8 flags, pause: group related
options, split the surface, or reconsider placement. This is a review trigger,
not a hard rule.

**Inputs.** For every string filter flag, decide the explicitly-empty behavior
now: passing `--contains ""` (e.g. from an unset shell variable) must be a
usage error, not a silent no-op that returns everything. Detect it with
`cmd.Flags().Changed(...)`, not `value != ""`. Sibling commands must agree on
where validation happens — if one parses a selector client-side and returns a
quoted error, its sibling must not ship the same input to the server for an
opaque 400.

**Output class.** Three questions: does it finish and print a result (finite)?
does it write files as its main outcome (artifact — but commands that write
files as a side effect and answer with a result document are finite)? does it
emit events until stopped (stream)? Everything else is one of the declared
exempt classes. In agent mode a finite command emits exactly one JSON value on
stdout; stderr is advisory. See `docs/design/agent-mode.md` §6.4. Register the
class in `cmd/gcx/root/testdata/output_classes.json` — CI enforces the entry.

**Completeness.** `list_meta` is the shared truncation contract
(`docs/design/output.md` §15, `internal/output/listmeta.go`): its absence means
the list is complete, so any command that slices a list MUST attach it —
silent truncation is forbidden. Bind the flag with
`opts.IO.BindListLimit(...)`, truncate with the constructor that matches the
source shape (`TruncateCompleteList` for cheaply-complete sources,
`PagedListMeta`/`TruncatePagedList` for paginated ones), finalize with
`AttachListMeta`, and emit the stderr hint via `EmitListTruncationHint` after
the payload. Do not pre-truncate output for agent mode — the agents codec
spills oversized results to a file with a receipt; pre-truncation defeats it.

**Errors.** Summaries come from the closed vocabulary in
`docs/design/errors.md`; exit codes from `docs/design/exit-codes.md` (0-6).
For invalid input the error must carry: the rejected value (safely — never echo
secrets), the expected type/format or allowed values, and a concrete corrected
invocation. "invalid selector" alone strands both humans and agents. Note
retryability where the backend rate-limits (suggest backoff, don't implement
retry loops silently).

**Large responses.** The gcx-native toolkit, in priority order: push filters
to the server; bind `--limit` with `list_meta`; let `--json` field selection
and `--jq` reduce the payload; let the agents codec spill what's still large.
Set `token_cost` to match the actual bound and write the `llm_hint` so it
teaches narrowing (which flags reduce the result), not just describes cost.

## 4. Agent-routing test matrix

Design these five cases in the worksheet; execute them if a routing harness is
available, otherwise mark them `UNVERIFIED` in the PR summary (never silently
assume them):

| Case | Given | Expected |
|------|-------|----------|
| Positive | a request squarely in scope | this command, with correct args |
| Near miss | the nearest sibling's request | the sibling, or no invocation — not this command |
| Ambiguous | a request that underspecifies | a discovery step (`--help`, `gcx commands`) before committing |
| Malformed | a call with a bad flag value | the error names the value, the expected format, and a corrected call |
| Large result | a request over a big collection | narrowing flags or accepting list_meta/spill — not an unbounded dump |

## 5. Test-plan bar

Root-level conformance suites (`cmd/gcx/root/`) enforce wiring and protocol
shape for every leaf automatically. They do NOT prove your command talks to its
backend correctly — that is the package's job. Plan, per command:

- **Request mapping** against an `httptest` capture server: every flag
  provably reaches the wire (method, path, params asserted).
- **Validation before I/O**: bad input fails without a network call.
- **Explicitly-empty flags**: `--flag ""` errors; unset flag behaves per contract.
- **Pagination**: page boundaries, has-more signals, and the cap path.
- **Both output modes**: human table/text and agent-mode single-JSON-document
  (use `internal/testutils` helpers to pin agent mode BEFORE constructing the
  command — the default resolves at flag-binding time).
- **Error paths**: backend 4xx/5xx map to the declared summaries and exit codes.
- **Destructive confirmation** where applicable: `--force`, `GCX_AUTO_APPROVE`,
  agent-mode declines without `--force`.

Mutation test for vacuity: "if `--prefix` were accidentally bound to
`opts.Suffix`, would this suite fail?" A test that only asserts a flag exists
is not coverage. Exemplar to copy:
`internal/providers/metrics/adaptive/client_test.go`.

## 6. gcx-native guardrails

- No MCP-style qualified naming — gcx command paths are the namespace.
- No invented `concise|detailed` response modes — `-o`, `--json`, `--jq`, and
  the agents codec already cover acquisition shaping.
- No blind CRUD consolidation — CONSTITUTION dual-path and naming rules govern
  what merges; consolidation of *your own overlapping proposal* is Phase A's job.
- Restate nothing from the governing docs in your PR — link them; this file
  restates only rules that are otherwise unwritten.

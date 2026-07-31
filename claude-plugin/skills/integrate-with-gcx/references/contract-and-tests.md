# The Integration Contract

Coding agents select and call gcx commands the way they call tools: by reading
`Use`, `Short`, `Long`, `Example`, flag help, `token_cost` and `llm_hint`, all of
which gcx exposes verbatim through `gcx commands` and `gcx help-tree`. An
ambiguity there becomes a misroute or a malformed call that no downstream error
handling can repair.

Cover the contract below before writing code, **at the size of the change** — a
new flag needs three lines of it, a new provider needs all of it. It is working
knowledge, not a document to hand over. What you surface to a human is decisions,
questions and risks.

## 1. What the contract covers

```text
Purpose (one sentence, exact outcome)
Stability: stable | experimental (experimental must be marked before release)

USE SIGNALS
  Direct:   requests that should route here ("show me X", "list the Y")
  Indirect: needs that imply it ("which Z is failing" implies listing Z)
  NOT for:  adjacent requests that must route elsewhere
  Nearest sibling: <command> — distinction in one sentence

COMMAND SURFACE
  Path: from docs/design/command-naming.md; for a discovery facet with no
        addressable item, an <operation>-<subject> compound
  Use / Short / Long / Example — drafted as routing metadata, written as `gcx …`
  Args validator: cobra.NoArgs | cobra.ExactArgs(n) | …

INPUTS (one row per positional/flag)
  name | type | constraints | default (+ why it's safe) | example | empty-value behavior

OUTPUT
  Protocol class: finite | artifact | stream | interactive | server | shell | prose | raw
  Success schema sketch (fields + types)
  One representative result, in the shape that class actually emits (§3)

BACKEND REQUEST MAPPING
  endpoint(s) | which flag feeds which param | pagination mechanism | auth used

COMPLETENESS
  complete | limited | capped by source — and how partiality becomes visible
  (see self-review.md T3; list_meta is PROPOSED and opt-in, not required)

ERRORS (one row per expected failure)
  condition | summary | exit code | runnable recovery suggestion | retryable?

SIZE & COST
  Expected result size (typical / worst case)
  token_cost: small | medium | large (+ an llm_hint that teaches narrowing)

BOUNDARIES
  Auth/ownership: what gcx manages vs what the product owns
  Reuse: the exact shared packages this must use
  Non-goals
```

## 2. Field guidance

**Routing metadata.** `Short` states what the command does in one line; `Long`
adds when to use it and when not to; `Example` shows the most common real
invocation. Follow `docs/design/help-text.md`. Check sibling vocabulary before
naming flags — if the family says `--name` for substring matching, do not
introduce `--filter` for the same idea; if siblings document
"case-insensitive", match it or differ explicitly. Enum values and JSON field
names match siblings for the same concepts.

**Defaults.** Every default needs a one-line rationale: it should produce a
useful, bounded result with no extra parameters.

**Parameter count.** More than ~8 flags on one leaf is a review trigger, not a
hard rule: group related options, split the surface, or reconsider placement.

**Large responses.** In priority order: push filters to the server; bind a
`--limit`; let `--json` field selection and `--jq` reduce the payload; let the
agents codec spill what is still large. Set `token_cost` to match the actual
bound.

## 3. Output is per protocol class

The eight classes in `docs/design/agent-mode.md` §6.4 do **not** share one
JSON-document contract. Register the class in
`cmd/gcx/root/testdata/output_classes.json` — CI enforces the entry — then use
the row that applies:

| Class | Representative result | What the output test asserts |
|---|---|---|
| `finite` | exactly one JSON value on stdout — the result, or a fused in-band error document, with the process exit code agreeing | one value, not two; usage text never on stdout; in-band `exitCode` matches the process code |
| `artifact` | files on disk are the real output; stdout carries exactly **one** JSON receipt (`gcx.artifact_receipt`: paths, format, counts, failures) | the receipt's paths and counts match the files actually written; `-o` selects the FILE format and is pinned |
| `stream` | typed, versioned JSONL — every line independently parseable with a `type` discriminator | a terminal success/error event always arrives; each line parses alone |
| `interactive` | exempt from the JSON contract, but **must never block** in agent mode | confirmation gates fail fast without `--force`; an explicitly configured non-interactive editor is honored — declining is one valid behavior, not the rule |
| `server` / `shell` / `prose` / `raw` | long-running listeners, completion scripts, help prose, byte passthrough | exempt and *declared*; the test pins the declared behavior, not a JSON shape |

Human output is a separate axis from all of this: a table or text rendering with
its own empty state. Test the formats your command actually declares — don't
assert a JSON shape against a human codec, and don't force `[]` into a table.

## 4. Agent-routing matrix

Design these five; execute them if a routing harness is available, otherwise say
`UNVERIFIED` rather than silently assuming them:

| Case | Given | Expected |
|---|---|---|
| Positive | a request squarely in scope | this command, correct args |
| Near miss | the nearest sibling's request | the sibling, or no invocation — not this command |
| Ambiguous | a request that underspecifies | a discovery step (`--help`, `gcx commands`) before committing |
| Malformed | a bad flag value | the error names the value, the expected format, and a corrected call |
| Large result | a request over a big collection | narrowing flags or accepting truncation metadata — not an unbounded dump |

## 5. Tests

The general bar — request mapping, validation before I/O, empty-flag cases,
pagination, zero results, error paths, mutation resistance — is in
[self-review.md](self-review.md) T8. The class-specific assertions are the table
in §3 above. Exemplar to copy:
`internal/providers/metrics/adaptive/client_test.go`.

## 6. gcx-native guardrails

- No MCP-style qualified naming — gcx command paths are the namespace.
- No invented `concise|detailed` response modes — `-o`, `--json`, `--jq` and the
  agents codec already cover output shaping.
- No blind CRUD consolidation — CONSTITUTION dual-path and naming rules govern
  what merges. Consolidating *your own* overlapping proposal is placement's job.
- Restate nothing from the governing docs in your PR — link them. This file
  restates only rules that are otherwise unwritten.

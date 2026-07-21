# Output-Format Consistency Audit & Corrections (#387 output track)

> **Status: audit record + proposal, not accepted policy.** This documents
> the output-format consistency review run for the #387 output workstream:
> the full inconsistency inventory with evidence, the safe corrections that
> ship in the PR carrying this document, and the items that need a
> maintainer decision before anything else changes. Nothing in the
> DECISION NEEDED sections is settled — those decisions are presented for
> sign-off in issue #1030. The corrections were first implemented and
> adversarially reviewed on an internal feasibility branch, then extracted
> onto main; a related list-truncation contract ("Track C") explored on
> that branch has **not** landed and is referenced below only as a
> proposal.

## 1. Purpose

`docs/design/output.md` prescribes one output model; the code ships several.
An evidence-cited audit of output mechanics found (a) untyped diagnostics
invisible to agents, (b) an agent-mode default that corrupts file-writing
commands, (c) an unpredictable `text`/`table` spelling split, and (d) design
docs describing flags and defaults that do not exist. The mandate for this
track: implement only the safe, well-supported corrections; record
everything that needs an owner decision here and in issue #1030.

## 2. Method

Every claim below was re-verified against `origin/main` at extraction time
(not taken from the earlier feasibility-branch audit on faith) by reading
the cited files and re-running the counts. Corrections were implemented
with table-driven tests where practical and validated with `go test`
(full suite, race detection), `golangci-lint`, regenerated reference docs,
and behavior demos comparing a main-built binary against a branch-built
binary (see §9).

## 3. Census (re-measured on origin/main, non-test code)

| Measurement | Value |
|---|---|
| `RegisterCustomCodec("table", …)` call sites | 156 |
| `RegisterCustomCodec("text", …)` call sites | 28 |
| `DefaultFormat("table")` | 131 |
| `DefaultFormat("json")` | 40 |
| `DefaultFormat("yaml")` | 49 |
| `DefaultFormat("text")` | 27 |
| `DefaultFormat("graph")` | 3 |
| `DefaultFormat("pretty")` | 1 |
| `cmdio.Success` → stdout / stderr | 77 / 39 |
| `cmdio.Warning` → stdout / stderr | 10 / 8 |
| `cmdio.Info` → stdout / stderr | 21 / 16 |
| `cmdio.Error` → stdout / stderr | 1 / 0 |
| Status-message stream total | 109 stdout vs 63 stderr — no discernible rule |

An earlier pass measured the per-`get`-command default split at roughly
37 yaml / 14 table / 7 json / 6 text (with the alert provider splitting
internally between table and json). That split is consistent with the
`DefaultFormat` census above and with the per-provider spot checks in §7,
but was not re-derived command-by-command here.

Commands registering **both** spellings on one options instance: only
`internal/providers/assistant/mcpservers/commands.go:291-292`, and both
names point at the same codec type (`ListTableCodec`). No command registers
the two names with genuinely different **tabular** codecs — but commands do
register non-tabular codecs under one of the two names (e.g. help-tree's
prose codec named `text`), which is what sank the §4.5 synonym attempt.

---

## 4. IMPLEMENTED — safe corrections in this PR

### 4.1 `kg entities list` truncation hint was invisible to agents

**Was:** `internal/providers/kg/commands.go` wrote a raw
`fmt.Fprintf(os.Stderr, "hint: …")` — bypassing the command's redirected
stderr (`cmd.ErrOrStderr()`) and emitting no JSONL `{"class":"hint"}` record
in agent mode.

**Now:** routed through `output.EmitHint(cmd.ErrOrStderr(), …)` with the same
informational content (limit reached; raise `--limit` or pass `--limit 0`).
The hint deliberately stays a plain "may be truncated" notice: the kg source
is a paginated backend page truncated client-side with
`adapter.TruncateSlice`, and the `>=` trigger fires even when the page is
exactly `--limit` long — it does not prove truncation, so no stronger
machine-readable claim would be honest here. Migrating kg search to a
proven-truncation envelope is part of the proposed (unlanded) Track C work.

The sibling per-type pagination hint in `searchByTypes` (same defect
class: typed on a TTY, invisible as JSONL) was converted the same way. TTY
output is byte-identical for both (the messages already began with
`"hint: "`).

### 4.2 `resources get` truncation notice was untyped

**Was:** `cmd/gcx/resources/get.go` wrote a plain
`fmt.Fprintf(cmd.ErrOrStderr(), "Showing first %d items per resource type. …")`
— invisible as JSONL to agents.

**Now:** routed through `output.EmitHint` keeping the per-resource-type
meaning intact ("showing first N items per resource type; use --limit=0 to
fetch all"). This is K8s per-type paging — the limit applies to each
resource type independently, so no list-level truncation metadata was
bolted on. TTY delta: the line now carries the standard `hint: ` prefix and
lowercase phrasing.

### 4.3 IRM OnCall: stale comment + dead assignments

`internal/providers/irm/oncall_commands.go:35-38` claimed the alert-groups
tree "defaults to `text` (table)" while the code initialized
`defaultFmt := "table"` and redundantly re-assigned `"table"` in the
`alert-groups` and `alerts` switch cases. Comment rewritten to match reality
(everything defaults to `table`), the dead variable and both redundant
assignments removed, `DefaultFormat("table")` called directly. No behavior
change; IRM package tests green.

### 4.4 Agent-mode default corrupted file-writing commands (real bug)

**Was:** `resources pull` set no `DefaultFormat`, so
`BindFlags`' agent-mode branch (`internal/output/format.go`) flipped its
default to `agents`. Pull uses `OutputFormat` as **both** the on-disk file
extension and the encoder (`cmd/gcx/resources/pull.go` →
`local.GroupResourcesByKind(opts.IO.OutputFormat, …)`,
`internal/resources/local/writer.go`): agent invocations wrote
`<name>.agents` files, and any resource whose payload exceeded the 100 KiB
spill threshold got a spill-summary envelope written **into the resource
file** (`internal/output/agents.go`). `resources edit` had the same default
dependence across its encode → editor → decode round-trip, and its agent-mode
round-trip could never succeed (the `FSReader` has no `agents` decoder —
`internal/resources/local/reader.go`, `format.Codecs()` is json/yaml only).

**Now:**

- `Options.PinDefaultFormat(name)` added to `internal/output/format.go`:
  sets the default and exempts the command from the agent-mode override.
  Explicit `-o` still wins (cobra flag precedence unchanged).
- `pull` and `edit` pin `json` — their current non-agent effective default,
  so non-agent behavior is unchanged; agent-mode default is now `json`.
- `pull` and `edit` reject an explicit `-o agents` at validation time with a
  clear error (see §5 behavior changes). For `edit` this converts a
  guaranteed post-editing failure (decode error after the user's edits are
  lost) into an upfront validation error.
- `edit`'s `RunE` never called `opts.Validate()` at all (pre-existing
  options-pattern defect) — the call was added.
- `docs/design/output.md` §14 updated to document the pinning rule.

Tests: `internal/output/format_options_test.go`
(`TestBindFlags_PinDefaultFormat`) and
`cmd/gcx/resources/pull_edit_format_test.go` (agent-mode default regression
for pull and edit; `-o agents` rejection for both; explicit `-o yaml`
override still honored).

### 4.5 `-o text` / `-o table` synonym — attempted, then REVERTED (DEFERRED)

**Was:** 156 call sites register the tabular codec as `table`, 28 as `text`
(§3); users cannot predict the spelling — `slo definitions list -o text`
errors, `resources get -o table` errors.

**Attempted (on the feasibility branch):** a resolution-time alias in
`Options.Validate()` rewriting `OutputFormat` between the two spellings
when the requested one resolved to no codec but the sibling was registered.

**Reverted — DEFERRED; the alias does not ship.** The alias was presented
as a safe correction but changed **non-tabular** commands: any command that
registers a codec under one of the two names for output that is not a table
gained an unintended `-o` spelling for it. Verified example:
`gcx help-tree version -o table` emitted the prose tree text, because
help-tree registers its prose codec under the name `"text"` — the alias
routed `-o table` to prose. The resolution-failure guard only scopes the
alias to commands that register exactly one of the two names; it cannot
know whether that codec is genuinely tabular.

The underlying 156-`table` / 28-`text` split (census in §3) still needs
fixing, but it requires either (a) an owner decision on the canonical
spelling with a call-site migration — the same ADR as 7.1 / §8's
doc-vs-code divergence — or (b) an aliasing mechanism scoped to genuinely
tabular codecs (e.g. opt-in registration metadata), not a name-based
rewrite. Deferred to issue #1030; both spellings behave exactly as before
this PR.

### 4.6 `docs/design/output.md` §14 was factually false

The section claimed pull has a `--format` flag (yaml|json, default yaml).
Reality: the standard `-o` flag with an implicit `json` default
(`docs/reference/cli/gcx_resources_pull.md` agrees). Rewritten to describe
actual behavior including the §4.4 pinning rule. The original yaml-default
design intent is **not** silently adopted — recorded as an open divergence
(§7.10, issue #1030).

### 4.7 Agents-codec spill hint broke the JSONL stderr contract

**Was:** `internal/output/agents.go` wrote the spill notice as a raw
`fmt.Fprintf(c.errWriter, "hint: response too large…")`. The agents codec
runs almost exclusively in agent mode (it *is* the agent-mode default), so
its own diagnostic violated the FR-104 typed-class JSONL stderr contract on
exactly the consumer that depends on it.

**Now:** routed through the same-package `EmitHint` with identical
informational content (payload size, spill file path, `-o json` escape
hatch; the command argument is empty — the summary already embeds the file
path). TTY output keeps the `hint: `-prefixed line byte-compatible with the
previous shape; agent mode now emits `{"class":"hint","summary":…}`. Tests:
`TestAgentsCodec_Spill_EmitsStderrHint` (TTY prefix) and
`TestAgentsCodec_Spill_AgentModeHintIsJSONL` in
`internal/output/agents_test.go`.

### 4.8 `resources get` truncation hint skipped under `--json`

**Was:** the §4.2 typed truncation hint fired only on the shared encode
tail of `cmd/gcx/resources/get.go`; the `--json field1,field2` branch
returned early through `writeFieldSelect`, so field-selecting consumers —
disproportionately agents, the audience the hint exists for — silently lost
the truncation notice.

**Now:** the RunE output tail is extracted into `writeGetOutput`, and the
`emitGetTruncationHint` helper fires on every output path including field
selection (hint after encode, before the captured error is returned). Test:
`TestGetTruncationHint` in
`cmd/gcx/resources/get_truncation_hint_test.go` (JSONL hint in agent mode,
`hint: ` line on a TTY, silence when not truncated, plain-output
regression).

### 4.9 `resources pull` advertised but silently ignored `--json`/`--jq`

**Was:** pull encodes resource files via `opts.IO.Codec()` directly, so the
`--json` and `--jq` flags bound by `BindFlags` were advertised in help but
had no effect — worse than an error, because a user asking for
field-selected output got full resource files with no indication their
flags were dropped.

**Now:** pull's `Validate()` rejects both flags upfront with round-trip
errors exactly mirroring the edit rejections (§5 item 3): a field-selected
or jq-transformed document is not the resource and cannot round-trip as an
on-disk resource file that push reads back. Test:
`TestPullAndEditRejectJSONAndJQ` in
`cmd/gcx/resources/pull_edit_format_test.go` (covers both commands).

---

## 5. Behavior changes shipped (enumerated)

All are stderr-only, validation-time, or agent-mode-only; none change a
successful non-agent stdout payload.

1. **`resources pull` / `resources edit` reject `-o agents`** (new
   validation error). Previously `pull -o agents` "worked" — wrote
   `.agents` files with display/spill envelopes; `edit -o agents` always
   failed, but only after the user finished editing. The usage menus no
   longer advertise `agents` for these two commands (implemented via
   `Options.HideFormat`; see §6.3).
2. **Agent-mode default for `pull`/`edit` is now `json`** (was `agents`).
   Non-agent default unchanged (`json` before and after).
3. **`resources edit` now runs `Validate()` and rejects `--json`/`--jq`
   outright** — unknown `-o` values fail before fetching/opening the
   editor. Note: merely activating `Validate()` was NOT safe for
   `--json`/`--jq` — validation would have *accepted* well-formed uses and
   let field-selected/jq-transformed output into the editor buffer, which
   edit then round-trips back as the resource. Both flags are now rejected
   with a round-trip error (`cmd/gcx/resources/edit.go`;
   `TestPullAndEditRejectJSONAndJQ`, which covers the matching pull
   rejections from §4.9 too).
4. **`resources pull` now rejects `--json`/`--jq` outright** (new
   validation error, §4.9). Previously both flags were advertised but
   silently ignored — pull encodes via `opts.IO.Codec()` directly.
5. **Agent-mode hints became JSONL** for `kg entities list` (both hints) and
   `resources get` truncation; `resources get`'s TTY notice gained the
   `hint: ` prefix. The kg `--limit` hint also moved from process stderr to
   the command's `cmd.ErrOrStderr()`.
6. **The agents-codec spill notice became a typed JSONL hint in agent
   mode** (§4.7); the TTY line keeps its `hint: ` prefix.
7. **`resources get --json <fields>` now emits the truncation hint**
   (§4.8) — stderr-only; previously the field-select path dropped it.
8. **The `-o text`/`-o table` resolution-time synonym does not ship**
   (§4.5): on the feasibility branch it changed non-tabular commands
   (verified: `help-tree version -o table` rendered prose) and was
   reverted. Both spellings behave exactly as on main before this PR; the
   split is DEFERRED to an owner decision (issue #1030).

## 6. RECOMMENDED — safe but visible; needs a nod (not implemented)

### 6.1 `dashboards list` pagination hint gated to table/wide only

`internal/providers/dashboards/crud.go:119,135-137`
(`shouldEmitListPaginationHint`) suppresses the continue-token hint exactly
for the json/agents consumers who most need it. **Judgment call: not
implemented**, for three reasons:

1. The suppression is deliberate and pinned by an explicit test
   (`internal/providers/dashboards/pagination_test.go:91`,
   `TestEmitListPaginationHintStructuredOutput` asserts empty output for
   json/yaml/agents) — overriding a considered, tested decision without an
   owner nod is what this section is for.
2. The proposed Track C list-truncation contract (feasibility work, not
   landed) queues `dashboards list` for a per-command decision: converge it
   on a machine-readable truncation envelope with the cursor command in a
   `continue` field. Un-gating the legacy ad-hoc hint now would be churn
   ahead of that sanctioned fix.
3. The right end state is a truncation envelope plus an unconditional typed
   hint, not the current bespoke hint emitted more often.

The blast radius of un-gating would be trivial (stderr-only), so if the
owner prefers the interim fix over waiting for the migration, it is a
two-line change plus a test update.

### 6.2 kg per-type server-error warning is untyped

`internal/providers/kg/commands.go` (`searchByTypes`, the
`"warning: skipping entity type …"` write) still uses a raw `Fprintf`.
Routing through `output.EmitWarn` would make it agent-legible but changes the
TTY prefix from `warning:` to `warn:` — left for a nod.

### 6.3 Rejected `agents` format still advertised in pull/edit usage

After §4.4, `-o agents` was rejected but `Output format. One of: agents,
json, yaml` still appeared in pull/edit help (the menu comes from
`allowedCodecs()` merging builtins). **Since implemented in this PR**:
`Options.HideFormat(name)` removes a format from the advertised menu (usage
string and unknown-format error listings) without unregistering it; pull and
edit hide `agents`. Resolution is untouched, so the commands' own bespoke
rejection errors still fire on an explicit `-o agents`
(`TestPullAndEditDoNotAdvertiseAgentsFormat`, `TestHideFormat`).

## 7. DECISION NEEDED — recorded, not implemented (see issue #1030)

### 7.1 `get`-command default-format convergence

Split on main: slo/k6/fleet get → yaml; dashboards/synth/faro get → table;
the alert provider splits internally (rules/groups list → table, several
single-object reads → json). See §3 census. **Question:** what is the
canonical default for `get <name>` — the human table (per output.md §1.3) or
the full serialized object (yaml/json)? **Options:** (a) table everywhere,
detail via `-o yaml`; (b) yaml for single-object get, table for list; (c)
leave and document. Needs an ADR-level call; blast radius is every scripted
consumer of a changed default.

### 7.2 Adaptive-telemetry family divergence

Within one product family: adaptive logs get → table, adaptive metrics get →
json, adaptive traces get → yaml; creates split json vs yaml
(`internal/providers/logs/adaptive/commands.go` alone mixes
`DefaultFormat("table")` and `DefaultFormat("json")` across subcommands —
lines 79/131/541 vs 629/677/881/932). Same question as 7.1 scoped to the
signal providers, which also advertise themselves as one consistent family.

### 7.3 `--json` list-shape divergence and sibling-key dropping

Three list shapes coexist under `-o json`: bare array, `{"items":[…]}`, and
`{"<wrapper>":[…]}`. Field selection over single-key envelopes drops sibling
keys: `appo11y operations` loses `service`/`window`/`metrics_mode`/
`span_kinds` under `--json`; `assistant conversation get`'s transcript
`{"chat","messages"}` shape defeats selection entirely. The general sibling
policy (preserve all non-items siblings? canonicalize on `items`?) is
unresolved. **Question:** one canonical list envelope pre-GA, or grandfather
the three shapes?

### 7.4 Mutation-output generations + status-stream anarchy

Three generations coexist: `resources push`/`delete` have **no `-o` at all**
(pull only grew one as a file-format selector); IRM ships a structured
`MutationResult` envelope; adaptive commands echo the mutated object to
stdout plus `cmdio.Success` to stderr. `cmdio.Success/Warning/Info`
(`internal/output/messages.go`) are not agent-aware (no JSONL class), and
the 109/63 stdout-vs-stderr split (§3) follows no rule — output.md §12 even
prescribes both ("summary to stdout" and "Success for progress"). §12's
summary-table/JSON-summary design is implemented almost nowhere.
**Question:** adopt §12 as written, adopt IRM's `MutationResult` as the
repo-wide envelope, or codify the status-message stream rule first? This is
the largest coherence gap found and is squarely ADR territory.

### 7.5 `cloud stacks` internal split

`internal/providers/stacks/commands.go`: list/create/update/regions default
`table` (lines 32/140/234/415) but get defaults `yaml` (line 85). A mutation
(`create`/`update`) defaulting to a *table* while `get` shows yaml means the
create → inspect loop changes shape mid-flow. Small fix, but it is an
instance of 7.1/7.4 and should follow that decision.

### 7.6 One-off format names + undocumented `agents` in every menu

`pretty` (`cmd/gcx/linter/lint.go:50`), `raw`
(`internal/datasources/loki/query.go:124`), `pprof`
(`internal/datasources/pyroscope/query.go:48`) are single-command format
names. `agents` appears in every `-o` menu but is documented only in
output.md §1.1.1 (not in the CLI reference conceptually). **Question:**
allow domain formats freely, or reserve a namespace (`raw`, `pprof` arguably
carry real semantics; `pretty` duplicates what `table`/`text` mean
elsewhere)? Should `agents` be hidden from menus or documented properly?

### 7.7 output.md §1.3 default-format table vs reality

§1.3 says list/get default `text` with table codec, and "push, pull, delete:
status messages only". Reality: §3 census (131 table / 40 json / 49 yaml /
27 text defaults), and pull has had a data-bearing `-o` for its file format
all along. The doc prescribes a policy the code never followed — either the
policy is right (then ~100 commands are defects) or the doc must be
rewritten to the real model. Policy decision, not a typo; ties to 7.1.

### 7.8 output.md §1.4 "status messages go to stdout" vs reality

§1.4 prescribes stdout for all `cmdio` status messages; the code splits
109/63 (§3). Whichever way the rule lands (kubectl convention would be:
data → stdout, diagnostics → stderr), ~40% of call sites move. Same ADR as
7.4.

### 7.9 `gcx config path`: agent mode flips default to `json`, not `agents`

Root cause (investigated): `cmd/gcx/config/path.go:20-76` predates/bypasses
the shared options — it hand-rolls its own `-o` flag (line 74) with a local
`defaultFormat := "table"` flipped to `"json"` under `agent.IsAgentMode()`
(lines 70-73), and encodes via a locally constructed
`cmdio.Options{OutputFormat: …}` (line 57). The `agents` codec is never
reachable (menu: `json, table, yaml`), so `json` is the intentional
hand-rolled stand-in. **Not** a trivial local bug: the proper fix is
migrating the command to `cmdio.Options.BindFlags`, which visibly changes
its surface (adds `--json`/`--jq`, the agents codec and spill, and a
different menu). Left as a finding.

### 7.10 `resources pull` on-disk default: json (shipped) vs yaml (designed)

§14's original text wanted yaml as the on-disk default. The implementation
has always defaulted to json, and §4.4 pinned that (unchanged) default.
Switching to yaml is a one-line change but breaks every workflow that
assumes `.json` files from a bare `gcx resources pull` (including
`push`-side globbing and any user tooling). **Question:** converge on yaml
(K8s-manifest convention, matches `resources get`'s yaml-ish leanings in
7.1(b)) or ratify json? Needs an owner call plus a migration note either way.

### 7.11 `alert groups status`: status exists only in the table codec

The status columns are synthesized inside the table codec only
(`internal/providers/alert/groups_commands.go:231` ff.,
`GroupsStatusTableCodec`); `-o json`/`-o yaml` output of `groups status` is
byte-identical to `groups list` — the very data the command exists to show
is absent from structured output. Pattern 13 violation ("codecs control
display, not data"); fixing it means adding the status field to the
underlying value (a payload shape change for existing json consumers).
Owner decision.

## 8. Doc-vs-code contract divergences (summary)

| Doc claim | Reality | Status |
|---|---|---|
| §14: pull has `--format`, default yaml | `-o`, default json | **Fixed** (doc rewritten, §4.6); yaml-default intent → 7.10 |
| §1.3: list/get default `text` | 131 table / 40 json / 49 yaml / 27 text | DECISION NEEDED (7.7) |
| §1.3: push/pull/delete "status messages only" | pull has data-bearing `-o`; IRM has MutationResult | DECISION NEEDED (7.4) |
| §1.4: status messages → stdout | 109 stdout vs 63 stderr | DECISION NEEDED (7.8) |
| §11: "register a `text` table codec, DefaultFormat(`text`)" | 156 register `table`, 28 `text` | DEFERRED (§4.5 — synonym reverted; broke non-tabular commands); canonical spelling → same ADR as 7.1 |
| §12: mutation summary tables + JSON summary shape | implemented almost nowhere | DECISION NEEDED (7.4) |

## 9. Validation record

- `gofmt` clean on all touched files.
- `GCX_AGENT_MODE=false go test -race ./...` — full suite green.
- `mise run lint` — 0 issues.
- `GCX_AGENT_MODE=false mise run reference` — regenerated, drift-clean
  (`gcx_resources_pull.md` / `gcx_resources_edit.md` pick up the hidden
  `agents` menu entry).
- Live-binary comparison (main binary vs this branch, identical args,
  same context): pull/edit reject `-o agents` and `--json`/`--jq`
  pre-connection; pull/edit `--help` under agent mode shows
  `(default "json")` and no `agents` menu entry; `resources get --limit N`
  emits `{"class":"hint","summary":"showing first N items per resource
  type; …"}` on stderr in agent mode and `hint: …` on a TTY, including
  under `--json <field>`; JSON stdout payloads byte-identical to main for
  every compared command.

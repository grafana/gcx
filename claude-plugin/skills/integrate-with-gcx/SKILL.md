---
name: integrate-with-gcx
description: >
  Guides a contributor and their coding agent through adding or extending a
  capability in the grafana/gcx codebase: deciding whether a new command is
  needed and where it belongs (provider, datasource kind, resource adapter,
  cloud command, or bundled skill), designing the command's agent-facing
  contract (naming, typed inputs, output protocol class, completeness,
  errors, token cost), implementing with shared-infrastructure reuse, and
  catching the defect classes that recur in gcx review before a human looks.
  Use when working inside a grafana/gcx checkout to add, extend, or review a
  gcx capability. Trigger on phrases like "integrate with gcx", "add my
  product to gcx", "new gcx command", "expose this API through gcx", or "get
  my gcx PR ready for review". NOT for operating a Grafana instance with gcx
  — use the gcx skill or a product skill (slo-manage, synth-manage-checks,
  create-dashboard). NOT for installing or configuring gcx — use setup-gcx.
---

# Integrate a Capability with gcx

**Work autonomously.** Inspect the repo, decide, show the decision, keep going.
This skill has no approval gates. Ask only when repository evidence genuinely
cannot settle something ([Asking](#asking)).

The premise everything follows from: **the commands you add are tools other
agents route on.** `Use`/`Short`/`Long`/`Example`, flag help, token cost and
hints are surfaced verbatim through `gcx commands` and `gcx help-tree` — that
metadata is an operational contract, not decoration. Names are frozen within a
major version, so the path you pick is the path forever.

| Mode | You are here when |
|---|---|
| [**Place**](#mode-place) | where this belongs is undecided |
| [**Build**](#mode-build) | placement is known — you decided it, the user stated it, or a sibling skill sent you |
| [**Review**](#mode-review) | there is already a branch or PR to get review-ready |

Start with `mise run build`, then `bin/gcx commands >/dev/null && echo ok`. Read
`AGENTS.md` — the entry point for the governing docs; paths cited below are cited
directly, including `docs/plans/` and `docs/research/`, which its map does not
list. Use `bin/gcx` for your own shell checks (exercise the binary you just
built, not a stale installed one); everything *user-facing* you author —
`Example:` fields, help text, docs — says `gcx`.

## Mode: Place

> Detail: [references/placement-and-readiness.md](references/placement-and-readiness.md)

Inventory the tree first — a new leaf competes for every agent's attention:

```bash
bin/gcx help-tree
bin/gcx commands --flat -o json
bin/gcx resources list-types
bin/gcx providers list
```

Settle four things and show them as a short **placement section** (bullets, not
a document):

- **Necessity** — reuse / extend / consolidate with a sibling / new leaf / not
  gcx. For a new leaf, name the nearest existing sibling and the one sentence an
  agent would use to choose between them. If a person who knows the tree can't
  say which command applies, an agent can't either.
- **Path** — from `docs/design/command-naming.md` plus precedent in the real tree
  and `docs/plans/list-subject-verdicts.md`.
- **Backend + wiring** — what serves the data, verified by a probe rather than
  assumed, and which wiring carries it. `gcx api` is a diagnostic fallback,
  never the integration target.
- **Readiness** — ready / backend prerequisite (named owner) / bounded bootstrap
  / not gcx. Product teams own their API shape, auth, limits and domain data
  reduction; gcx wraps APIs, it does not fix them.

Missing information does not stop you: discover it, or ask one targeted question.

**What happens next depends on the readiness outcome.** Two of the four are
terminal — Place is the whole deliverable and there is nothing to implement:

| Readiness outcome | Next |
|---|---|
| **ready** | continue |
| **bounded bootstrap** | continue, within the ceiling the outcome requires |
| **backend prerequisite** *with an explicitly viable read-only slice* | continue, on that slice only |
| **backend prerequisite** *without a viable slice* | **stop.** Report the boundary, the missing prerequisite and its named owner |
| **not gcx** | **stop.** Report the boundary and the owner; write no contract and no code |

Do not invent a contract or an implementation for a terminal outcome. Concluding
"not gcx" and then building anyway is the expensive version of getting placement
wrong.

For the outcomes that continue, the flow is the same four steps whether you
implement in Build or hand the work to a sibling skill:

```text
Place  →  contract (Build, sized to the change)  →  implementation  →  Review
```

Handing off after Place and skipping straight to a sibling's Stage 3 means the
naming, typed-input, output-class, completeness, error and test-quality guidance
never runs — which is most of what this skill is for. Cover the contract first,
pass it forward with the placement section, and come back for Review before the
work is called review-ready. None of those steps is a human gate.

## Mode: Build

> Detail: [references/contract-and-tests.md](references/contract-and-tests.md)

Cover the contract before writing code — purpose, stability, use signals and
when-NOT-to-use, routing metadata, every input typed with constraints and a
defaulted-for-a-reason value and explicit empty-value behavior, output protocol
class, request mapping, completeness, error recovery, token cost, reuse,
non-goals. Cover it at the size of the change: a new flag needs three lines, a
new provider needs all of it. **This is working knowledge, not a document to
produce** — what you show the human is decisions, questions and risks.

Search for the shared implementation before writing one:

```bash
grep -rn "LoadContextAndConfig" internal/datasources/query/
grep -rn "func NewClient" internal/query/grafanaquery/
grep -rn "BindListLimit\|AttachListMeta" internal/output/
grep -rn "ConfirmDestructive" internal/providers/
```

The rules that are enforced in review but written almost nowhere else — `Args:`
validators, explicitly-empty flags, sibling validation symmetry, dead codec
paths — are the same checks the review mode runs, so they live once, in
[references/self-review.md](references/self-review.md). Read it now, not after
you write the code.

Everything else follows the governing docs: `docs/reference/provider-guide.md`,
AGENTS.md Key Conventions (datasource reuse), `docs/design/output.md`,
`docs/design/safety.md`, `docs/design/errors.md`.

Close by reading your command back the way agents see it — `bin/gcx help-tree`,
`bin/gcx commands --flat -o json` — and re-check the routing metadata against
what renders.

## Mode: Review

Resolve the real base first (stacked branches: it is not necessarily `main`),
then run the diff-triggered checks in
[references/self-review.md](references/self-review.md) across the full diff, and
after a fix push across the incremental one too. Fix what is determinate;
report what is not. Fix commits are a top defect source — re-run after every
push, not only before the first review.

## Rules at their real strength

Never state proposed or conventional guidance as law.

| Rule | Strength |
|---|---|
| Output-class fixture entry, token cost | **CI-enforced** — `TestConsistency_AllLeafCommandsHaveOutputClass` / `HaveTokenCost` walk every leaf and fail on a missing entry |
| `llm_hint` whenever the worst case is medium/large | **Required, only partly CI-enforced.** `NonSmallCommandsHaveLLMHint` matches the annotation *exactly* against `"medium"`/`"large"`, so a qualified cost (`small (large with --all)`) evades the check. Write the hint anyway — the rule is about the worst case, not the annotation's spelling |
| Cloud-only availability, command→skill mapping | **NOT enforced in that direction.** `TestConsistency_CloudOnlyPathsResolveToCommands` and `SkillMappingResolvesToCommands` iterate the entries you *declared* and check each resolves to a real command — they catch a stale entry after a rename, never a missing one. Adding the entry is review-enforced |
| A `finite` leaf emits exactly one JSON value in agent mode | **CI-enforced** (`TestAgentConformance_*`) |
| One `init()`, one `providers.Register()`; no `adapter.Register()` outside it | **CONSTITUTION** § Architecture Invariants |
| Error summaries from the closed vocabulary | **Law, scoped to `cmd/gcx/fail/`** converters — not a constraint on arbitrary command error text |
| Exit codes 0-6 | Real and reachable when you set it. **Documented gap:** cobra's own flag/arg errors exit 1, not 2 (`docs/design/exit-codes.md` §2.3) — don't claim 2 for a path you didn't wire |
| `Args:` on every leaf | Strong convention; no CI check |
| `list_meta` truncation metadata | `docs/design/output.md` §15 is **PROPOSED** and opt-in, two exemplar commands. Not repo-wide, not required for every list command |
| Empty array serialized as `[]` not `null` | Convention with local test precedent; no doc rule |

**Completeness is the honesty rule underneath §15.** Review it whenever your
command has a `--limit`, slices a collection, stops paging early, or is bounded
by a source cap — those mechanisms, not a guess about whether truncation is
"likely". A caller handed a partial result with no signal reads a page as the
whole inventory. *Where* to disclose depends on the output shape: an envelope
carries `list_meta` via the shared helpers in `internal/output/listmeta.go`; a
**released bare array gets the stderr hint only** (`output.md` §15.2) — wrapping
it in an envelope is a breaking change, so file the migration instead of making
it. Status: `docs/research/2026-07-17-global-limit-investigation.md`.

**Empty results are schema fidelity, not a mode rule** — an array your schema
declares must not serialize as `null` when empty, in the machine formats your
command declares. Human codecs render their own empty state.

**Output rules are per protocol class** — the eight classes in
`docs/design/agent-mode.md` §6.4 do not share one JSON-document contract.
Examples and tests follow the class (table in contract-and-tests.md).

## Asking

Ask when the repo, the governing docs and a verified probe cannot settle
something that changes the work. Group related questions into one interruption,
carrying the evidence, a recommended option, and what changes per answer.

Worth asking: missing product/API facts that change behavior, auth, RBAC, limits
or completeness · unclear ownership or API stability · genuine VISION
uncertainty · governing docs that conflict · a novel verb or placement the naming
guide and precedent record don't cover · a scope choice producing a materially
different implementation. Never ask merely because the change is large or
introduces a provider.

Stop substantial work only when the request violates CONSTITUTION with no
compliant alternative preserving the intent · a waiver or governance decision is
required · a missing backend, auth or ownership prerequisite prevents an honest
implementation · an unresolved user choice would materially change what is built.

## Handoffs

For outcomes that continue (see [Place](#mode-place) — a terminal readiness verdict
hands off nothing), hand off **after** the contract, not after Place, and return
for Review:

```text
Place  →  contract  →  [add-provider | add-datasource]  →  Review (here)
```

| Skill | Owns | What you pass in | What you do after |
|---|---|---|---|
| `add-provider` | provider package, config keys, client, adapter, staging | the placement section **and** the contract — command paths, typed inputs and empty-value behaviour, output class, completeness decision, error summaries, token cost | run [Review](#mode-review) over the resulting diff |
| `add-datasource` | query client, per-kind constructor, `DatasourceProvider` registration, the generic-dispatch decision | same | same |

Say explicitly which decisions are settled so the sibling records them instead of
re-deriving them. If your harness does not expose these as skills, read the file —
you are in the checkout: `.claude/skills/{add-provider,add-datasource}/SKILL.md`.

**Migration is out of scope for this version.** Porting a provider from the legacy
CLI is not covered: `migrate-provider` documents its own naming collision with the
legacy tool (both were called `gcx`), which makes its side-by-side verification
steps ambiguous. Do not route a port through this skill autonomously — tell the
user it needs a human to drive, and point them at
`.claude/skills/migrate-provider/SKILL.md` and its status note.

## Wiring and gates

> Detail, plus the CI-failure lookup table: [references/distribution-and-gates.md](references/distribution-and-gates.md)

Per-leaf wiring CI will not let you skip is tabulated in the reference. The gap
CI does *not* catch: registration mounts the typed `datasources <kind>` subtree,
but the generic auto-detecting `datasources query` dispatches through a
hand-maintained switch in `cmd/gcx/datasources/query.go`. Entering that switch is
a **judgement, not a checkbox** — do it only if the generic
`<uid> <expr>` form can honestly carry your query. If it cannot, add an explicit
redirect there instead: CloudWatch does exactly that, because its query is
structured (namespace, metric, dimensions, region, statistic, period) and no
single `expr` string represents it. An honest redirect beats both a lossy generic
path and the bare "not supported" default.

Format the files you touched, then gate:

```bash
mise exec -- gofmt -w <the .go files you edited>
mise run gate                          # fast inner loop: lint + tests + build
GCX_AGENT_MODE=false mise run all      # before you push; subsumes the above + docs
```

`go` and `gofmt` are supplied by mise and are not necessarily on your `PATH` —
run them as `mise exec -- go …` / `mise exec -- gofmt …`, or use the `mise run`
tasks, so you get the toolchain version the repo pins.

Run `gate` while iterating and `all` once before pushing — `all` already
includes validate-skills, lint, tests, build and docs, so there is no third
command to add. `GCX_AGENT_MODE=false` is load-bearing: agent-mode detection
flips output defaults and corrupts generated docs. A gate you cannot run locally
is reported SKIPPED with the reason, never as green. Authoritative checklists:
AGENTS.md.

## Output Format

Report only what a human still owns:

```text
INTEGRATION SUMMARY — <capability>

Placement: <necessity + path + wiring> — <one-line rationale>
Changed: <what this diff does>
Non-goals: <out of scope; product-owned items with owners>

Unresolved decisions or risks: <what a reviewer must decide, or none>
Unverified assumptions: <claims not probed, and how to probe them, or none>
Checks failed or skipped: <gate: reason, or none>
Architecture deviations: <deviation + rationale, or none>
```

No pass/n-a checklists. An empty section is a claim — write `none` only when
it's true.

# Capability implementation

Use for command/capability changes after resolving the contribution route and
applicable maintainer decisions. Reuse accepted proposal, RFC and placement
decisions; inspect only gaps that change implementation. Metadata in `Use`,
`Short`, `Long`, `Example`, flags and hints is surfaced through `gcx commands`
and `gcx help-tree`: it is a routing contract. Command paths remain stable within
a major version.

Build with `mise run build` before binary checks. Use `bin/gcx` to exercise the
current build; user-facing examples say `gcx`.

## Placement and readiness


> Detail: [placement-and-readiness.md](placement-and-readiness.md)

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
  assumed, and which wiring carries it. A probe that is unavailable, inconclusive
  or outside the target you were placed in scope for is reported `UNVERIFIED` —
  never run, and never read as a negative result. `gcx api` is a diagnostic
  fallback, never the integration target.
- **Readiness** — ready / backend prerequisite (named owner) / bounded bootstrap
  / not gcx. Unknowns block by **material risk**, not by category: an unknown
  bearing on API ownership or stability, auth/RBAC, security, mutation safety,
  correctness (including an unseen route or payload) or bounded completeness
  means **backend prerequisite**, and for those there is no "ready, pending
  verification". Any other unknown is recorded `UNVERIFIED` with its probe and
  the work continues. Full list in
  [placement-and-readiness.md](placement-and-readiness.md).
  Product teams own their API shape, auth, limits and domain data reduction; gcx
  wraps APIs, it does not fix them.

Investigate missing facts; ask a targeted question when a consequential choice remains unresolved.

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


## Command contract and implementation


> Detail: [contract-and-tests.md](contract-and-tests.md)

Cover the contract before writing code — purpose, stability, use signals and
when-NOT-to-use, routing metadata, every input typed with constraints and a
defaulted-for-a-reason value and explicit empty-value behavior, output protocol
class, request mapping, completeness, error recovery, token cost, reuse,
non-goals. Cover it at the size of the change: a new flag needs three lines, a
new provider needs all of it. **This is working knowledge, not a document to
produce** — what you show the human is decisions, questions and risks.

Search for the shared implementation before writing one:

```bash
rg -n "LoadContextAndConfig" internal/datasources/query/
rg -n "func NewClient" internal/query/grafanaquery/
rg -n "BindListLimit|AttachListMeta" internal/output/
rg -n "ConfirmDestructive" internal/providers/
```

The rules that are enforced in review but written almost nowhere else — `Args:`
validators, explicitly-empty flags, sibling validation symmetry, dead codec
paths — are the same checks the review mode runs, so they live once, in
[self-review.md](self-review.md). Read it now, not after
you write the code.

Everything else follows the governing docs: `docs/reference/provider-guide.md`,
AGENTS.md Key Conventions (datasource reuse), `docs/design/output.md`,
`docs/design/safety.md`, `docs/design/errors.md`.

Close by reading your command back the way agents see it — `bin/gcx help-tree`,
`bin/gcx commands --flat -o json` — and re-check the routing metadata against
what renders.


## Rules at their real strength


Never state proposed or conventional guidance as law.

| Rule | Strength |
|---|---|
| Output-class fixture entry, token cost | **CI-enforced** — `TestConsistency_AllLeafCommandsHaveOutputClass` / `HaveTokenCost` walk every leaf and fail on a missing entry |
| `llm_hint` whenever the worst case is medium/large | **Required, only partly CI-enforced** — `NonSmallCommandsHaveLLMHint` matches `"medium"`/`"large"` exactly, so a qualified cost evades it. Write the hint anyway; the rule is about the worst case, not the spelling. Trade-off in [self-review.md](self-review.md) T1.3 |
| Cloud-only availability, command→skill mapping | **NOT enforced in that direction.** `TestConsistency_CloudOnlyPathsResolveToCommands` and `SkillMappingResolvesToCommands` iterate the entries you *declared* and check each resolves to a real command — they catch a stale entry after a rename, never a missing one. Adding the entry is review-enforced |
| A `finite` leaf emits exactly one JSON value in agent mode | **CI-enforced** (`TestAgentConformance_*`) |
| One `init()`, one `providers.Register()`; no `adapter.Register()` outside it | **CONSTITUTION** § Architecture Invariants |
| Error summaries from the closed vocabulary | **Law, scoped to `cmd/gcx/fail/`** converters — not a constraint on arbitrary command error text |
| Exit codes 0-6 | Real and reachable when you set it. **Documented gap:** cobra's own flag/arg errors exit 1, not 2 (`docs/design/exit-codes.md` §2.3) — don't claim 2 for a path you didn't wire |
| `Args:` on every leaf | Strong convention; no CI check |
| `list_meta` truncation metadata | `docs/design/output.md` §15 is **PROPOSED** and opt-in. Not repo-wide, not required for every list command |
| Empty array serialized as `[]` not `null` | Convention with local test precedent; no doc rule |

**Completeness is the honesty rule underneath §15.** A caller handed a partial
result with no signal reads a page as the whole inventory — wrong but plausible
instead of big but correct. The trigger is the mechanism (a `--limit`, a slice, an
early paging stop, a source cap), never a guess about whether truncation is
"likely". *Where* to disclose depends on the output shape: an envelope carries
`list_meta`, but a **released bare array gets the stderr hint only** — wrapping it
in an envelope is a breaking output change belonging to a deliberate
compatibility migration, not a feature PR. Shared helpers, §15's real status and
the full shape table: [self-review.md](self-review.md) T3.

**Empty results are schema fidelity, not a mode rule** — an array your schema
declares must not serialize as `null` when empty, in the machine formats your
command declares. Human codecs render their own empty state.

**Output rules are per protocol class** — the eight classes in
`docs/design/agent-mode.md` §6.4 do not share one JSON-document contract.
Examples and tests follow the class (table in contract-and-tests.md).

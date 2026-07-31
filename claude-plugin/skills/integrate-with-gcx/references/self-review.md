# Diff-Triggered Review

These are the defect classes that repeatedly consume gcx review rounds. Every
one is checkable before a human looks. **Run them by trigger, not as a
checklist:** look at what your diff actually does, run the checks that fire, fix
what is determinate.

**A trigger is a list, not a headline.** Every numbered check inside a fired
trigger runs — finding something on check 1 does not finish the trigger. This is
the measured failure mode: in replicated runs of this review, the checks that got
missed were consistently the *later* ones inside a trigger whose first check had
already produced a finding. Walk the numbers.

**Do not produce a pass/n-a table.** Report only what a human still owns:
unresolved decisions or risks · unverified assumptions · checks that failed or
had to be skipped, with the reason · architecture deviations needing PR
attention. Working every check is internal; reporting is only about what is
unresolved.

| If your diff… | Run |
|---|---|
| adds or changes any leaf command | [T1](#t1-any-new-or-changed-leaf) |
| adds or changes a flag or positional | [T2](#t2-inputs) |
| returns a collection, or has a `--limit`, slice, early paging stop, or source cap | [T3](#t3-completeness) |
| adds or changes an error path | [T4](#t4-errors) |
| writes a client, codec, config loader, or HTTP transport | [T5](#t5-shared-infrastructure) |
| changes behavior of an already-shipped command | [T6](#t6-description-rot-and-scope) |
| introduces or renames a command path | [T7](#t7-naming-against-the-frozen-surface) |
| adds tests | [T8](#t8-tests-that-cannot-fail) |
| filters, matches, or searches | [T9](#t9-filtering-semantics) |
| is a fix pushed in response to review | [T10](#t10-fix-pushes) |
| touches bundled skills or generated docs | [T11](#t11-skills-and-generated-docs) |

## T1: Any new or changed leaf

Four checks. Work all four.

1. **`Args:` validator declared?** `cobra.NoArgs` for a flags-only command. Root
  `ValidateArgs` already rejects stray positionals for *group* commands, so the
  gap is leaves specifically: without a validator, a flags-only leaf swallows
  positional args and answers a different question than the one asked. Strong
  convention — no CI check will tell you.
2. **Output protocol class declared** in `cmd/gcx/root/testdata/output_classes.json`,
  and the class is the honest one. Three questions: does it finish and print a
  result (`finite`)? are files on disk the real output (`artifact` — but a
  command that writes files as a side effect and answers with a result document
  is `finite`)? does it emit events until stopped (`stream`)? Everything else is
  one of the declared exempt classes.
3. **Token cost annotated**, with an `llm_hint` for medium/large that teaches
  *narrowing* — which flags reduce the result — not just describes cost. A
  `small` annotation on a command that can dump tens of thousands of rows is a
  recurring blocker. Where a flag changes the bound, say so:
  `small (large with --all)`.
4. **Codec registered in `setup(flags)` is reachable from `RunE`.** Trace the
  registration to an actual encode. A codec registered for format validation
  whose `Encode` never runs is dead code — delete one of the two paths. (Easy to
  skip once checks 1-3 have already produced findings; don't.)

## T2: Inputs

Five checks. The last three are the ones runs of this review keep dropping.

1. **Explicitly-empty string flags are usage errors.** Decide the behavior of
  `--flag ""` (an unset shell variable) *now*: detect it with
  `cmd.Flags().Changed(...)`, never `value != ""`. An empty filter that returns
  the unfiltered set is a correctness trap, not a no-op.
2. **Apply check 1 to every string flag on every command the diff touches** —
  including the sibling you only extended. Finding the trap on the new command
  does not mean you have checked the family; enumerate the flags and go one by
  one.
3. **Siblings agree on *where* validation happens.** Separate from checks 1-2:
  if this command parses a selector client-side and returns a quoted error, its
  siblings must not ship the same input to the server for an opaque 400 — in
  both directions. The same bad input should fail the same way whichever command
  the caller reached for.
4. **Numeric flags:** negatives and zero handled per the documented meaning.
5. **Every input typed** with constraints, a default that has a one-line
  rationale, an example, and a stated empty-value behavior. A default that dumps
  an unbounded collection is not safe; one that silently narrows is not honest.

Across all five: validate in `Options.Validate()` before any I/O, and make the
error carry the rejected value (never echoing secrets), the expected format or
allowed values, and a corrected invocation.

## T3: Completeness

The mechanisms are the trigger — a `--limit`, a client-side slice, an early
paging stop, or a source cap — not a judgment about whether truncation is likely.
That judgment is exactly what authors get wrong.

**The honesty rule:** a caller handed fewer items than exist, with no signal,
reads a page as the whole inventory — wrong but plausible instead of big but
correct. So the partiality has to be disclosed somewhere the caller will see it.

**Where, per output shape** — `docs/design/output.md` §15.2 settles this, and it
is not "always in the payload":

| Output shape | Disclosure |
|---|---|
| Envelope (items + sibling keys) | `list_meta` in the payload, via the shared helpers |
| **Bare array**, already released | **stderr hint only.** §15.2: bare arrays "cannot carry the signal; they get the stderr hint only and should migrate to an envelope when their consumers can absorb the shape change". Wrapping a released bare array in an envelope is a breaking output change — `CONSTITUTION.md` § public command surface forbids it inside a major version. Note the migration as a follow-up; do not do it in a feature PR |
| New command, your choice of shape | choose an envelope, so the signal has somewhere to live |

**The shared mechanism** (for the envelope case): `internal/output/listmeta.go` —
`BindListLimit` to bind the flag, the constructor matching your source shape
(`TruncateCompleteList` for cheaply-complete sources, `PagedListMeta` or
`TruncatePagedList` for paginated ones), `AttachListMeta` to finalize, and
`EmitListTruncationHint` after the payload.

**Its status, stated accurately:** `docs/design/output.md` §15 is marked
**PROPOSED** (#387 Track C) and is implemented as an *opt-in* contract with two
exemplar commands (`datasources list`, `irm oncall alert-groups list`). It is
**not** a repo-wide requirement and **not** mandatory for every new list
command. Note also that a client-side-capped source must not use the binder,
because "0 means all" would be dishonest there — it discloses the cap via
`ListMeta.Cap` and the cap-variant hint instead. Migration plan and open
questions: `docs/research/2026-07-17-global-limit-investigation.md`.

Also check the zero-result path: a field your success schema declares as an
array must not serialize as `null` when empty, in the machine formats your
command declares (a filtered path that allocates and an unfiltered path that
returns the input slice will diverge). Human codecs render their own empty
state — an empty table or a "no `<subject>` found" line — so don't force `[]`
there. Convention with local test precedent; no doc rule.

Do not pre-truncate for agent mode: the agents codec spills oversized results to
a file with a receipt, and pre-truncation defeats it.

## T4: Errors

- Summaries in `cmd/gcx/fail/` converters must come from the closed vocabulary
  in `docs/design/errors.md` §"Summary vocabulary". That scope is the converters
  — it is not a constraint on arbitrary command error text.
- Exit codes per `docs/design/exit-codes.md` (0-6). **Known gap:** cobra's own
  usage errors (bad flags, missing args) currently exit **1**, not 2; §2.3
  records overriding them as future work. Code 2 is reachable when you set it
  explicitly. Don't claim 2 for a path you didn't wire, and don't "fix" the
  global behavior in a feature PR — flag it if it bites you.
- Invalid input reports the rejected value, the expected format or allowed
  values, and a runnable corrected call. "invalid selector" alone strands both
  humans and agents.
- Note retryability where the backend rate-limits — suggest backoff, don't
  implement silent retry loops.

## T5: Shared infrastructure

Nothing in the diff should re-implement what already exists:

```bash
grep -rn "LoadContextAndConfig" internal/datasources/query/resolve.go
grep -rn "grafanaquery.NewClient" internal/query/
grep -rn "ConfirmDestructive" internal/providers/
```

Datasource-style commands use `dsquery.LoadContextAndConfig`; providers use
`providers.ConfigLoader`; unified-query clients use `internal/query/grafanaquery`
plus `internal/query/dataframe`; destructive confirmation uses
`providers.ConfirmDestructive` (`--force`, long-only); HTTP to non-stack hosts
uses `httputils.NewDefaultClient`.

Two duplication shapes reviewers reliably find and authors reliably miss: a
codec or formatter that is the same type as a sibling's with only the format call
swapped (extract and share it), and a hand-rolled config loader that misses
every bug fix living in the shared one.

## T6: Description rot and scope

When behavior changes — a flag added or renamed, a default changed, an error path
reworked, a schema field added — re-read `Use`, `Short`, `Long`, `Example`, flag
help and `llm_hint` **as a set**, next to the new behavior. Then look at it the
way an agent will:

```bash
bin/gcx commands --flat -o json
```

Treat metadata edits as part of the behavior change, not a docs chore.

Scope: every behavior change in the diff appears in the PR description —
including tightened validation on an already-shipped command, which is a
disclosure item under the frozen-surface rule. No unrelated files. Claims match
what the code does ("computes X over all items" vs over one page). Product-owned
concerns found along the way are listed as boundaries or follow-ups, never
silently half-implemented.

## T7: Naming against the frozen surface

Released names are frozen within the major version — a wrong name is not
fixable later, which is why this question has repeatedly consumed review rounds
when left to review time. It is answerable in minutes.

Read the placement rule (per operation, not per subject) and the catalog-facet
rule in `docs/design/command-naming.md`, then check the real tree:

```bash
bin/gcx help-tree
```

Is there a sibling with the same shape? Would this be the first bare `<verb>` at
this level? Does an existing family use an `<operation>-<subject>` compound for
the same kind of facet? Check the adjudicated precedent record at
`docs/plans/list-subject-verdicts.md` — it has settled dozens of borderline
cases (notably: filter-flag usage does NOT make a value "addressable", and
ID-less value enumerations take the compound).

If the guide plus the precedent record still leave your shape ambiguous, raise it
as a question rather than self-adjudicating — a wrong self-adjudication ships
forever. Cite rules by document path and section, never by PR or issue number
from memory; a wrong number sends reviewers chasing ghosts.

## T8: Tests that cannot fail

Root conformance suites in `cmd/gcx/root/` enforce wiring and protocol shape for
every leaf automatically. They do **not** prove your command talks to its backend
correctly — that is your package's job.

Apply the mutation question to each test: if a flag were bound to the wrong
field, if a param never reached the request, if the limit were ignored — would
the suite go red? A test that asserts a flag exists, or constructs opts without
driving the command, passes under all of those defects.

Plan per command: request mapping against an `httptest` capture server (method,
path, params asserted) · validation before I/O (bad input fails with no network
call) · explicitly-empty flag cases · pagination boundaries, has-more signals
and the cap path · the zero-result case · error paths mapping to the declared
summaries and exit codes · destructive confirmation where applicable (`--force`,
`GCX_AUTO_APPROVE`, agent mode declining without `--force`).

Output-mode tests follow the **protocol class** — there is no single
JSON-document contract across all eight (see contract-and-tests.md). Pin agent
mode with `internal/testutils` helpers *before* constructing the command; the
default resolves at flag-binding time.

Exemplar to copy: `internal/providers/metrics/adaptive/client_test.go`.

## T9: Filtering semantics

The help text must say **where** filtering happens and **how terms combine**.
Client-side filtering over a full fetch is disclosed (and ideally capped) —
note that it means the whole result set crosses the wire on every call. If the
backend treats repeated selectors as a union (OR) while the help reads like
narrowing (AND), a user, or worse an agent, walks away believing something false
about their data.

**Case-sensitivity is a separate check, not a footnote:** open the flag's help
string and confirm it says whether matching is case-sensitive, and that the
answer matches the siblings. A user narrowing on `alerts` and silently missing
`ALERTS` has nothing to go on. This is missed more often than the AND/OR
question above it.

Push filters server-side where the API allows, or file the push-down as an
explicit follow-up. Reject or re-fold contradictory combinations rather than
silently OR-ing them.

Treat API capability claims as assumptions until probed. Never assert "the
backend cannot X" as fact from reading one client file — label it an assumption
with a verification probe, or check the vendor's API docs. A false capability
claim disposes of real design options.

## T10: Fix pushes

Fix commits written to address one review round have repeatedly introduced the
next round's findings, including outright blockers. Merge artifacts on stacked
branches are the worst offenders — one fix push resurrected deleted help text
across three generated doc pages.

Resolve the actual base first. On a stacked branch it is not `main`:

```bash
gh pr view --json baseRefName
```

If `gh` is unavailable or there is no PR yet, use the branch you actually
branched from (`git merge-base`) and say which base you used. Then re-run the
triggers that fire, over **both** ranges:

```bash
git diff <base>...HEAD -- <files the fix touched>
git diff <last-reviewed-sha>...HEAD
```

Diff any shared or generated file a merge commit touched against the base, to
catch resurrected stale content.

## T11: Skills and generated docs

Regenerate references and inspect the generated page for your command:

```bash
GCX_AGENT_MODE=false mise run reference
git diff --stat docs/reference/
```

Underscore-prefixed identifiers (a metrics `__name__`) render as bold markdown
unless backticked. Help text carrying a removed config key or a stale default is
a regression multiplier — it lands on several generated pages at once. New
packages get a row in `docs/architecture/project-structure.md`.

Sweep the bundled skills for routing your change affects:

```bash
grep -rn "<your command or the path it replaces>" claude-plugin/skills/
```

The drift test only catches invocations of commands that no longer exist — it
does not catch a skill still steering agents to a now-suboptimal path (an
uncapped discovery route your new command supersedes). List affected skills even
if updating them is deferred.

Where a style guide and the entire surrounding command family disagree — a
punctuation or phrasing convention the family never adopted — raise the conflict
for the docs owner rather than silently following either side. The guide itself
may be the piece that is out of step.

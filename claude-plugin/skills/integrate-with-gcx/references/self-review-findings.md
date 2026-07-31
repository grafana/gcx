# Self-Review: the Recurring Review Findings

Run this before requesting human review, and re-run it scoped after every fix
push (SR-9). Each item is a claim to falsify, a concrete check, and the fix
pattern. These are the finding classes that have repeatedly consumed review
rounds across multiple contributors and reviewers — every one is checkable
before a human ever looks at the PR. Cite item IDs (SR-1…SR-11) in your
PR-ready summary's attestation table.

Contents: [SR-1](#sr-1-token-cost-and-limit-honesty) · [SR-2](#sr-2-list-truncation-and-list_meta) ·
[SR-3](#sr-3-input-validation) · [SR-4](#sr-4-shared-code-reuse) · [SR-5](#sr-5-docs-regeneration-and-help-rendering) ·
[SR-6](#sr-6-command-naming-against-the-frozen-surface) · [SR-7](#sr-7-tests-that-cannot-fail) ·
[SR-8](#sr-8-filtering-semantics-honesty) · [SR-9](#sr-9-scoped-re-review-after-every-fix-push) ·
[SR-10](#sr-10-scope-and-disclosure) · [SR-11](#sr-11-description-rot)

## SR-1: Token-cost and limit honesty

**Claim to falsify:** the declared `token_cost`/`llm_hint` matches what the
command can actually emit, and no cap is silent.

**Check:** if the annotation says `small`, confirm the output is genuinely
bounded (single resource, counted summary, or a bound the code enforces). If
the command can return an unbounded or source-capped collection, there must be
a `--limit` (or a disclosed cap) and the hint must teach narrowing. A hint
claiming "small" over a command that can dump tens of thousands of names is a
review blocker, repeatedly.

**Fix pattern:** bind `--limit` via `BindListLimit`, disclose caps via
`ListMeta.Cap`, set the cost to the qualified form where a flag changes the
bound (e.g. `small (large with --all)`).

## SR-2: List truncation and list_meta

**Claim to falsify:** every path that returns fewer items than exist attaches
`list_meta`; absence of `list_meta` always means complete.

**Check:**

```bash
grep -rn "BindListLimit\|AttachListMeta\|TruncateCompleteList\|TruncatePagedList" internal/output/
```

Then inspect your command: if it slices a slice, applies a limit, or stops
paging early WITHOUT attaching `list_meta`, agents will read a page as the
whole inventory — "wrong but plausible instead of big but correct". A
stderr-only hint is not the contract; the metadata must ride in the payload,
and it must survive `--json` field selection.

**Fix pattern:** `docs/design/output.md` §15; exemplar `cmd/gcx/datasources/list.go`.
Also confirm the new leaf has its entry in
`cmd/gcx/root/testdata/output_classes.json` (CI fails without it).

## SR-3: Input validation

**Claim to falsify:** no input silently changes the question being answered.

**Check, per leaf:**
- `Args:` validator declared? A flags-only command without `cobra.NoArgs`
  swallows positional args and answers as if they weren't there.
- Every string filter flag: what happens on `--flag ""` (unset shell
  variable)? It must be a usage error via `cmd.Flags().Changed(...)` — an
  empty filter that returns the unfiltered set is a correctness trap.
- Sibling consistency: if this command validates a selector client-side,
  do its siblings? Same input, same failure shape, both directions.
- Numeric flags: negatives and zero handled per the documented meaning.

**Fix pattern:** validate in `Options.Validate()` before any I/O; error text
carries the rejected value, the expected format, and a corrected example.

## SR-4: Shared-code reuse

**Claim to falsify:** nothing in the diff re-implements shared infrastructure.

**Check:**

```bash
grep -rn "LoadContextAndConfig" internal/datasources/query/resolve.go
grep -rn "grafanaquery.NewClient" internal/query/
grep -rn "ConfirmDestructive" internal/providers/
```

Compare against your diff: hand-rolled config loading misses the bug-fix
families that live in the shared loader; duplicated formatters/codecs drift
apart; a second copy of the query transport re-introduces solved problems.
Also check the reverse: a codec you registered but never route to from `RunE`
is dead code that reviewers will find.

**Fix pattern:** datasource-style commands use `dsquery.LoadContextAndConfig`;
providers use `providers.ConfigLoader`; unified-query clients use
`internal/query/grafanaquery` + `internal/query/dataframe`; destructive
confirmation uses `providers.ConfirmDestructive` (`--force`, long-only);
HTTP to non-stack hosts uses `httputils.NewDefaultClient`.

## SR-5: Docs regeneration and help rendering

**Claim to falsify:** generated docs are current and render correctly.

**Check:** run the reference regeneration and inspect the generated page for
your command: underscore-prefixed identifiers (like a metrics `__name__`)
render as bold markdown unless backticked; help text with a removed config key
or a stale default is a regression multiplier because it lands on several
generated pages at once.

```bash
GCX_AGENT_MODE=false mise run reference
git diff --stat docs/reference/
```

**Fix pattern:** backtick identifiers in `Long`/flag help; regenerate and
commit; `docs/architecture/project-structure.md` gets the new package row
(AGENTS.md PR checklist step 4).

## SR-6: Command naming against the frozen surface

**Claim to falsify:** the proposed path complies with
`docs/design/command-naming.md` and has been checked against the real tree.

**Check:** read the placement rule (per operation, not per subject) and the
catalog-facet rule; then check the tree for precedent:

```bash
gcx help-tree
```

Ask: is there any sibling with the same shape? would this be the first bare
`<verb>` at this level? does an existing family use a compound
(`<operation>-<subject>`) for the same kind of facet? Released names are
frozen within the major version — a wrong name is not fixable later, which is
why this question has repeatedly consumed multiple review rounds when left to
review time. It is answerable in minutes from the doc plus the tree.

**Fix pattern:** derive the name before writing code; record the derivation in
the worksheet; if the guide is silent on your shape, ask for maintainer review
explicitly rather than inventing precedent.

## SR-7: Tests that cannot fail

**Claim to falsify:** every test would fail if the behavior it names broke.

**Check:** for each test, apply the mutation question — if a flag were bound
to the wrong field, if a param never reached the request, if the limit were
ignored — would the suite go red? Tests that assert a flag exists, or that
construct opts without driving the command, pass under all of those defects.

**Fix pattern:** drive the real command function against an `httptest` capture
server and assert method/path/params; test the explicitly-empty flag cases;
test both human and agent output modes (pin agent mode before command
construction). Exemplar: `internal/providers/metrics/adaptive/client_test.go`.

## SR-8: Filtering semantics honesty

**Claim to falsify:** the help text says where filtering happens and how terms
combine.

**Check:** client-side filtering over a full fetch must be disclosed (and
ideally capped); if the backend treats repeated selectors as a union (OR)
while the help reads like narrowing (AND), a user — or worse, an agent — walks
away believing something false about their data. Case-sensitivity: stated, and
consistent with siblings.

**Fix pattern:** disclose in flag help; push filters server-side where the API
allows (or file the push-down as an explicit follow-up); reject or re-fold
contradictory combinations rather than silently OR-ing them.

## SR-9: Scoped re-review after every fix push

**The single highest-leverage habit in this list.** In recent integrations,
fix commits written to address one review round have repeatedly introduced the
next round's findings — including outright blockers. Merge artifacts on
stacked branches are the worst offenders (a fix push resurrected deleted help
text across three generated doc pages).

**Check, after EVERY push:**

```bash
gh pr view --json baseRefName
```

Resolve the actual PR base (stacked branches: the base is not necessarily the
default branch), then re-run SR-1…SR-8 scoped to BOTH ranges:

```bash
git diff <base>...HEAD -- <files the fix touched>
git diff <last-reviewed-sha>...HEAD
```

Pay special attention to merge commits in the fix push: diff any shared or
generated file they touched against the base branch to catch resurrected
stale content.

## SR-10: Scope and disclosure

**Claim to falsify:** the PR description matches the diff, and nothing rides
along.

**Check:** every behavior change in the diff appears in the description
(including tightened validation on already-shipped commands — under the
frozen-surface rule that is a disclosure item); no unrelated files; claims in
the description ("computes X over all items") match what the code does
(computes X over one page?). Product-owned concerns discovered during the work
are listed as boundaries or follow-ups, not silently half-implemented.

**Fix pattern:** the PR-ready summary template (SKILL.md Phase F) makes this
mechanical: decided / deferred / open-questions sections, and an explicit
non-goals list.

## SR-11: Description rot

**Claim to falsify:** the routing metadata still tells the truth after the
latest change.

**Check:** whenever behavior changes (a flag added/renamed, a default changed,
an error path reworked, a schema field added), re-read the command's `Use`,
`Short`, `Long`, `Example`, flag help, and `llm_hint` as a set, next to the
new behavior. Then look at it the way an agent will:

```bash
gcx commands --flat -o json
```

**Fix pattern:** treat metadata edits as part of the behavior change, not a
docs chore; regenerate references (SR-5). During rollout, collect misroutes
and malformed calls against the command as feedback into this item.

---
name: review-pr
description: Review someone else's gcx pull request, produce a ranked report with a verdict, and optionally post it to GitHub as line-anchored inline comments. Covers what to report, in what order, when to stop, how to rank over-engineering findings, how to judge whether a large diff is justified, and how to turn the report into review comments with an APPROVE/COMMENT/REQUEST_CHANGES recommendation. Trigger on "review this PR", "review #NNNN", "code review this branch", "is this diff over-engineered", "post the review", "add review comments to this PR". NOT for self-review before pushing your own work — use integrate-with-gcx for that.
---

# Reviewing a gcx pull request

Use this skill to review work you did not write. It owns the **report**: what
goes in it, in what order, and when to stop.

Run the correctness pass below and the repository checks where they already live:

- `.claude/skills/integrate-with-gcx/references/self-review.md`. Read
  **Evidence discipline** first. It sets what a finding may conclude. Then run
  the triggers that fire for the diff.
  [T12](../integrate-with-gcx/references/self-review.md#t12-over-engineering) is
  the over-engineering rubric.
  [T5](../integrate-with-gcx/references/self-review.md#t5-shared-infrastructure)
  covers code that repeats something the repo already has.
- AGENTS.md, for the compliance hierarchy. Check all four levels in order.

Authors run the same triggers before they push. Assume they did. Treat what you
find as missed, not as dismissed.

## The correctness pass

**You owe every PR a correctness pass, and you run it yourself.** Read the diff
for the defects a reviewer is here to catch: a nil or error path that returns
the wrong value, a boundary the change moved, a resource that leaks on the
failure branch, concurrent access the diff introduced, and a call whose
behaviour the diff changed without changing its callers.

Then run **the triggers above** that fire for this diff, plus the compliance
hierarchy.

`/code-review` is an optional second opinion. Before invoking it, read its
instructions and confirm it can return findings without publishing. If it is
unavailable, declines the PR, or only supports publication, complete your own
pass. The CI workflow uses that pass and installs no review plugin.

Never describe an incomplete correctness pass as a clean review. Report the
limitation if the diff or necessary code was unavailable.

Reconcile the sets before you report anything. Remove duplicates. Keep whichever
version of a finding states the failure more precisely.

Your pass and the supplement can disagree. One calls a line a bug and the other
calls the same line correct. Settle it against the code and report one
conclusion. Never report both and leave the author to decide.

The same skill runs on a developer's machine and in the review workflow. A PR
gets the same treatment either way.

## What blocks a merge here

The severity split matters more than any single finding. It decides what the
author must act on. Reserve the blocking tier for these, and nothing else.

**A correctness bug.** The change returns the wrong value, drops an error,
leaves a resource open on the failure path, or calls a function, field, or
option that does not exist. This is what your correctness pass is looking for.

**A violation of `CONSTITUTION.md` or `DESIGN.md`.** Two cases recur. The agent
output contract: a command declared `finite` in
`cmd/gcx/root/testdata/output_classes.json` must write exactly one JSON value to
stdout **when agent mode supplies the default** — that is, no explicit `-o`,
`--json`, or `--jq`. Under those flags the command follows the flag, so `--jq`
emitting NDJSON and `--json list` emitting one field per line are both correct
and neither is a finding. A flag that writes a file and leaves stdout empty
breaks the contract. Stream routing: a status line or a fallback notice on
stdout lands inside a user's redirected output.

**A regression to a released command surface.** A removed or narrowed flag value,
a changed exit code, or a changed output shape that existing `--json` or `--jq`
callers depend on. The surface is stable within a major version.

**Behaviour that changes inside a diff the author describes as a refactor.**

**A credential that escapes.** An error message that quotes a token, an
authorization code, or a password. A prompt that leaves one in the terminal input
queue when the flow exits early, where the shell then reads it. Where a diff adds
a control for this, check that it runs on every exit path and not only on
success.

**What does not block on its own.** Naming, structure, duplication, test shape,
and anything [T12](../integrate-with-gcx/references/self-review.md#t12-over-engineering)
or [T5](../integrate-with-gcx/references/self-review.md#t5-shared-infrastructure)
turns up. These block only when the same finding independently lands in one of
the tiers above — a name that breaks the frozen command surface is a
DESIGN violation and blocks as one. "It concerns naming" and "T12 fired" are not
reasons on their own. Everything else in this class goes in Other findings.

## Do not report

- Duplicate diagnostics already reported by CI. Report an independently
  established defect even when CI also covers that area; a green check does not
  prove the changed behavior is correct. Cite the mechanism and consequence,
  rather than repeating lint or test output.
- Generated files under `docs/reference/cli/`, and anything in `vendor/`.
- Missing test coverage in files the diff did not touch.

## Report shape

Use bold text for headings in the report, not markdown headings. Five sections,
in this order, referred to by name everywhere below.

**Intent.** The problem, and how the change solves it. State it and stop —
whether the approach is sound becomes obvious from the rest of the report, so do
not give a verdict on it here. Two sentences at most.

**Blocking.** Fix before merge — exactly the tiers in *What blocks a merge here*
above, and nothing beyond them. Omit this section when nothing blocks.

**Other findings.** Everything that does not block, worst first — see *Ordering*
below for what breaks a tie. Include documentation that contradicts the code it
describes, and everything from *What does not block on its own*. Give each
finding the space its argument needs and no more. Some take a paragraph. Most
take a line. The label shows the difference, so a one-line nit and a
worth-fixing defect can share the list.

**Fix summary.** One combined remedy. Omit when Blocking and Other findings are
both empty.

**Verdict.** Approve or request changes. Name the findings that decide it. If
you would merge a reduced version, say so, and say how much smaller. Two
sentences at most.

## Rules that keep the report honest

**Group findings by code unit, not by check.** Name the symbol, file, or flag.
Combine repeated diagnoses of the same defect. If a unit has distinct defects,
keep each mechanism and consequence visible within its grouped entry. Place the
entry in the section for its highest severity and retain the individual labels;
a shared symbol does not make every issue blocking.

**Ordering: worst first, lock-in breaks the tie.** Worst means the consequence
if it ships unfixed. Where two findings are equally consequential — and
over-engineering findings usually are — order them by lock-in, what the thing
costs to remove after release:

1. Exported API with fewer than two callers
2. User-visible surface: flags, output shape, command paths
3. Internal structure: duplicate types, thin wrappers, copied code
4. Tests and unreachable branches

A finding that fits no tier — stale documentation, say — ranks on consequence
alone. Break remaining ties by affected line count, then file path and line.
In Other findings, give at most six entries a full argument and summarize the
rest without changing their severity. The tie-break is mechanical on purpose,
so two reviewers produce the same order.

**Give one combined remedy.** Fix summary lists the smallest changes that
resolve the findings. Prefer deletions and consolidation when they are
sufficient; fixes may need added code or tests. Quantify a proposed reduction
only when it supports a simplification finding and the diff provides evidence.

**Say what would overturn each blocking finding.** This makes you look for the
author's reasoning before you write. It also gives the author something specific
to answer instead of a verdict to argue with. A comment at the call site does not
settle the question. Reviewing as though that comment is absent wastes a review
round.

**A large diff is not always an unjustified one.** A three-command feature with
generated reference docs and real client tests is legitimately large. Say so.
Judge the diff against its problem, not against a line count.

**Order the blocking findings by severity too.** A regression to shipped
behaviour outranks a violation a maintainer can waive in the PR. Say which is
which. Otherwise five blocking findings look heavier than the change deserves.

## When a workflow invoked this

Five things change when no human is present. The rest of the review is the same,
including the severity policy — a PR gets the same treatment either way.

- **Deliver the requested payload.** The configured workflow trigger authorizes
  publication. Follow *Workflow delivery*; the workflow publishes the review.
- **Never approve or request changes.** The workflow publishes a `COMMENT` review.
  Whether a finding is cheap enough to merge over is a judgement about the
  author's time. An unattended run cannot make it.
- **Drop what you could not establish.** Do not label it unverified. No author
  can answer a speculative finding here, and a wrong one makes them disprove it
  in public.
- **Use tighter caps**: at most three blocking entries, and eight comments in
  total. Drop the lowest-ranked entries; do not downgrade their severity.
- **Close the summary with this exact line**:

  > Comment `@claude review` for a fresh review.

Silence is a valid result. If there are no findings, say so in one line and
deliver that line for publication. Never invent a finding.

Silence is not the same as a pass you did not run. If your correctness pass
could not complete, say that instead — never post "no findings" on a review that
did not happen.

### Workflow delivery

When the caller requests a structured review payload, return
`{"review":{"body":"...","comments":[...]}}`. Use the report's summary and
inline-comment mapping from *Mechanics* below. Each inline comment carries
`path`, `line`, `side`, and `body`; ranges also carry `start_line` and
`start_side`. With no findings, use the no-findings summary and an empty comments
array. If the correctness pass could not complete, return `{"review":null}`.

**Do not publish through `gh`, a plugin, or an inline-comment tool in this
mode.** The workflow submits the payload as one `COMMENT` review from
`github-actions[bot]` on the supplied head commit, appends its run marker, and
validates GitHub's returned review ID,
author, submission state, marker, and commit before marking the PR reviewed.
It also checks the PR head before and after publication. An incomplete payload,
failed submission, or changed head fails the workflow; retry with `@claude review`.
The normal human-facing report and consent flow apply when no structured payload
was requested.

## Offering to post the review

After the report, offer to post it to the PR as inline comments if publication
has not already been explicitly authorized. A request to review alone does not
authorize posting; a request to post the review does. Reuse an event choice the
invoker has already made.

Present the event choice with a recommendation and the reason for it:

| Report state | Recommend |
|---|---|
| Blocking empty, Other findings empty | `APPROVE` |
| Blocking empty, Other findings has findings | `APPROVE` with the comments attached |
| Blocking has findings, all cheap to fix, none a regression to shipped behaviour | `COMMENT` |
| Blocking has a regression, a safety defect, or anything expensive | `REQUEST_CHANGES` |

State the recommendation and obtain the event choice if it is not settled. The
report's verdict judges the code. The event judges a colleague's work, and that
choice is theirs.

### Mechanics

One `POST` creates the whole review, both the summary body and every inline
comment. The author then gets one notification instead of ten. Pass the JSON
directly on stdin. A quoted heredoc keeps backticks, dollar signs, and
suggestions literal:

```bash
gh api repos/grafana/gcx/pulls/{n}/reviews -X POST --input - <<'REVIEW_JSON'
{
  "event": "COMMENT",
  "commit_id": "<reviewed head SHA>",
  "body": "<review summary>",
  "comments": []
}
REVIEW_JSON
```

Replace the placeholders, use the chosen event for a human-invoked review,
and put the inline findings in `comments`. Leave it empty for a clean review.
Keep the endpoint first, unquoted, and followed by `-X POST`: that matches the
repository's narrow review permission in `.claude/settings.json`. Record the
returned review ID. If publication is denied, report the failed command; do not
retry through scripts, post test comments, or switch to a regular PR comment.

Each comment needs `path`, `line`, and `side`. Use `RIGHT` for the file after
the change. A range also needs `start_line` and `start_side`.

**Check every line number against the PR head.** Diff line numbers are
different, and a wrong anchor puts the comment on unrelated code:

```bash
git fetch origin pull/{n}/head:refs/pr/{n}
git show "refs/pr/{n}:path/to/file.go" | grep -n "<the code you are commenting on>"
```

The line must fall inside a diff hunk. Context lines inside a hunk are valid.
GitHub rejects lines outside every hunk. A `suggestion` block replaces exactly
the lines you anchor it to, so match the indentation of the target. Go files use
tabs.

Blocking and Other findings become inline comments at the symbol each entry
names. Intent, Fix summary, and Verdict become the summary body.

### Rewrite the findings as comments

Never paste report prose into a thread. A finding that fills four paragraphs in
a document is too long to read in a review thread.

- Write two to four sentences. Start with the mechanism. End with the ask.
- Turn "what would overturn this" into a question the author can answer.
- Start with a bold label that says what the author must do. The summary names
  only the blocking findings. Without a label, the author has to work out which
  of the other comments they must act on.
- Use a `suggestion` block for any concrete one-line change.
- State conclusions, not process. "The extraction preserves every check in both
  paths: state → … → all 8 `Result` fields" helps the author. "I checked this
  line by line" describes you instead.
- Do not open with a compliment. Where the work deserves praise, one specific
  line at the end says more.

When one finding covers several sites, anchor it at one site and name the others
in the text (`also flow.go:160, gcom.go:145`). Ten comments make a review.
Thirty make noise.

### Labels

Labels follow each defect's severity. A grouped entry can contain both required
and advisory defects; label them individually:

| Section | Label | Means |
|---|---|---|
| Blocking | `**required**` | fix before merge |
| Other findings | `**recommended**` | should fix, does not block the merge |
| Other findings | `**nit**` | take it or leave it |
| Other findings | `**followup**` | fine to defer to its own PR |

Written out:

> **required** — this returns on every `readLine` error, not just the `Close`
> one the comment describes. …

In Other findings, use `**followup**` for work that merits a separate PR,
`**recommended**` for a nonblocking improvement, and `**nit**` for an optional
minor change. A cheap fix can still be required when its consequence blocks.

Say what the labels mean once, in the summary body, with the counts. The author
wants the shape of the review before the detail:

> comments below are prefixed **required** (2), **recommended** (4),
> **followup** (2), **nit** (2)

Only label a defect `**required**` when it meets the Blocking criteria. One
inflated label teaches the author to ignore every label in every later review.

## Language

Write the report and the comments in plain English:

- Short sentences. One idea in each.
- Active voice.
- Plain words instead of figurative ones.
- No idiom.
- Avoid phrases or terms that hyphenate words together. Find simpler alternatives.

This applies to prose. It does not apply to identifiers, code comments,
suggested diffs, or quoted output.

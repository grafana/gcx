---
name: review-pr
description: Review someone else's gcx pull request, produce a ranked report with a verdict, and optionally post it to GitHub as line-anchored inline comments. Covers what to report, in what order, when to stop, how to rank over-engineering findings, how to judge whether a large diff is justified, and how to turn the report into review comments with an APPROVE/COMMENT/REQUEST_CHANGES recommendation. Trigger on "review this PR", "review #NNNN", "code review this branch", "is this diff over-engineered", "post the review", "add review comments to this PR". NOT for self-review before pushing your own work — use integrate-with-gcx for that.
---

# Reviewing a gcx pull request

Use this skill to review work you did not write. It owns the **report**: what
goes in it, in what order, and when to stop.

It defines no checks of its own. Run the checks where they already live:

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

## Two passes, one set of findings

Run both passes, then combine them:

1. **`/code-review`**, for correctness bugs. Do not pass `--comment`. The
   findings must return to you instead of going straight to the PR, or you have
   nothing left to combine.
2. **The triggers above** that fire for this diff, plus the compliance
   hierarchy.

Reconcile the two sets before you report anything. Remove duplicates. Keep
whichever version of a finding states the failure more precisely.

The two passes can disagree. One calls a line a bug and the other calls the same
line correct. Settle it against the code and report one conclusion. Never report
both and leave the author to decide.

The same skill runs on a developer's machine and in the review workflow. A PR
gets the same treatment either way.

## What blocks a merge here

The severity split matters more than any single finding. It decides what the
author must act on. Reserve the blocking tier for four things.

**A violation of `CONSTITUTION.md` or `DESIGN.md`.** Two cases recur. The agent
output contract: a command declared `finite` in
`cmd/gcx/root/testdata/output_classes.json` must write exactly one JSON value to
stdout. A flag that writes a file and leaves stdout empty breaks that contract.
Stream routing: a status line or a fallback notice on stdout lands inside a
user's redirected output.

**A regression to a released command surface.** A removed or narrowed flag value,
a changed exit code, or a changed output shape that existing `--json` or `--jq`
callers depend on. The surface is stable within a major version.

**Behaviour that changes inside a diff the author describes as a refactor.**

**A credential that escapes.** An error message that quotes a token, an
authorization code, or a password. A prompt that leaves one in the terminal input
queue when the flow exits early, where the shell then reads it. Where a diff adds
a control for this, check that it runs on every exit path and not only on
success.

Report findings about naming, structure, duplication, and test shape as nits.
Do that even when the argument for them is strong.

## Do not report

- Anything CI already enforces: `mise run lint`, the test suite,
  `reference-drift`, `validate-skills`, and the conformance suites in
  `cmd/gcx/root/`.
- Generated files under `docs/reference/cli/`, and anything in `vendor/`.
- Missing test coverage in files the diff did not touch.

## Report shape

1. **Intent.** The problem, and how the change solves it. Say whether the
   approach is sound before you list what is wrong with it.
2. **Blocking.** Fix before merge: correctness bugs, regressions to shipped
   behaviour, safety defects, and violations of CONSTITUTION.md or DESIGN.md.
   Put two more things here. Behaviour that changes inside a described refactor.
   A call to a function, field, or option that does not exist.
3. **Other findings.** Everything that does not block, worst first. Include
   documentation that contradicts the code it describes. Give each finding the
   space its argument needs and no more. Some take a paragraph. Most take a
   line. The label shows the difference, so a one-line nit and a
   worth-fixing defect can share the list.
4. **Over-engineering.** Findings from T12 and T5. Rank and cap them as below.
5. **The smaller version.** One combined remedy. Omit this section when section
   4 is empty.
6. **Verdict.** Approve or request changes. Name the findings that decide it. If
   you would merge a reduced version, say so, and say how much smaller.

## Rules that keep the report honest

**One finding per code unit, not per check.** Name the symbol, file, or flag.
Two findings that name the same symbol are one finding. Combine them and list
every check the unit trips. State the count as evidence. Never split one unit
across sections. A split unit turns one problem into five complaints about the
same file.

**Rank section 4 by lock-in.** Lock-in is what the thing costs to remove after
release:

1. Exported API with fewer than two callers
2. User-visible surface: flags, output shape, command paths
3. Internal structure: duplicate types, thin wrappers, copied code
4. Tests and unreachable branches

Break ties by line count. Cap the section at six findings. Move the
lowest-ranked ones to section 3 as one-line nits. The ranking is mechanical on
purpose, so two reviewers produce the same order.

**Give one remedy, not one per finding.** Section 5 is an ordered list of
deletions and merges. Together they resolve everything in section 4. State the
resulting size change against the real diff size. The size is evidence for the
whole set. It is not a finding, and it does not belong in section 4.

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

Five things change when no human is present. The rest of the review is the same.

- **Post without asking.** The offer below applies when a human can answer. In
  CI nobody can, and the trigger is the consent.
- **Never approve or request changes.** Post whatever you found as a comment.
  Whether a finding is cheap enough to merge over is a judgement about the
  author's time. An unattended run cannot make it.
- **Drop what you could not establish.** Do not label it unverified. No author
  can answer a speculative finding here, and a wrong one makes them disprove it
  in public.
- **Use tighter caps**: at most three blocking findings, and eight comments in
  total. Drop the lowest-ranked findings rather than relabelling them. Say in
  the summary that you dropped some.
- **Close the summary with this exact line**, so an author who pushes a fix
  knows how to ask again:

  > Comment `@claude review` for a fresh review.

Silence is a valid result. When nothing meets the bar, say in one line that the
diff looked clean, and stop. Never invent a finding.

## Offering to post the review

After the report, offer to post it to the PR as inline comments. Do not post
without an explicit yes. The comments are public, they carry the invoker's name,
they land on someone else's PR, and you cannot undo them quietly.

Present the event choice with a recommendation and the reason for it:

| Report state | Recommend |
|---|---|
| Section 2 empty, section 3 empty | `APPROVE` |
| Section 2 empty, section 3 has findings | `APPROVE` with the comments attached |
| Section 2 has findings, all cheap to fix, none a regression to shipped behaviour | `COMMENT` |
| Section 2 has a regression, a safety defect, or anything expensive | `REQUEST_CHANGES` |

State the recommendation, then let the invoker choose. The report's verdict
judges the code. The event judges a colleague's work, and that choice is theirs.

### Mechanics

One `POST` creates the whole review, both the summary body and every inline
comment. The author then gets one notification instead of ten:

```bash
gh api repos/{owner}/{repo}/pulls/{n}/reviews -X POST --input review.json
```

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

Report sections split like this. Sections 2, 3, and 4 become inline comments at
the symbol each one names. Sections 1, 5, and 6 have no line to attach to, so
they become the summary body.

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

The label follows from the section. That makes it mechanical instead of a second
judgement:

| Section | Label | Means |
|---|---|---|
| 2 Blocking | `**required**` | fix before merge |
| 3 Other findings | `**recommended**` | should fix, does not block the merge |
| 3 Other findings | `**nit**` | take it or leave it |
| 4 Over-engineering | `**followup**` | fine to defer to its own PR |

Written out:

> **required** — this returns on every `readLine` error, not just the `Close`
> one the comment describes. …

Two adjustments. Label a section 4 finding `**nit**` when it is small enough to
fix in the same sitting. Where the author may ignore a `**nit**`, say so in the
text as well as the label.

Say what the labels mean once, in the summary body, with the counts. The author
wants the shape of the review before the detail:

> comments below are prefixed **required** (2), **recommended** (4),
> **followup** (2), **nit** (2)

Only label a comment `**required**` when it sits in section 2 of the report. One
inflated label teaches the author to ignore every label in every later review.

## Language

Write the report and the comments in plain English:

- Short sentences. One idea in each.
- Active voice.
- Plain words instead of figurative ones.
- No idiom.

This applies to prose. It does not apply to identifiers, code comments,
suggested diffs, or quoted output.

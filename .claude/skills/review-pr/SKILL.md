---
name: review-pr
description: Review someone else's gcx pull request, produce a ranked report with a verdict, and optionally post it to GitHub as line-anchored inline comments. Covers what to report, in what order, when to stop, how to rank over-engineering findings, when a large diff is honest rather than padded, and how to turn the report into review comments with an APPROVE/COMMENT/REQUEST_CHANGES recommendation. Trigger on "review this PR", "review #NNNN", "code review this branch", "is this diff over-engineered", "post the review", "add review comments to this PR". NOT for self-review before pushing your own work — use integrate-with-gcx for that.
---

# Reviewing a gcx pull request

This skill is for reviewing work you did not write. It owns the **report**: what
goes in it, in what order, and when to stop.

It defines no checks of its own. Run the checks where they already live:

- `.claude/skills/integrate-with-gcx/references/self-review.md` — read
  **Evidence discipline** first, since it governs what any finding may conclude,
  then run the triggers that fire for the diff. [T12](../integrate-with-gcx/references/self-review.md#t12-over-engineering)
  is the over-engineering rubric; [T5](../integrate-with-gcx/references/self-review.md#t5-shared-infrastructure)
  covers reimplementation of something the repo already has.
- AGENTS.md — the compliance hierarchy, checked in order.

Authors are asked to run the same triggers before pushing. Assume they did, and
that anything you find there was missed rather than dismissed.

Generate findings with `/code-review`, then rank and report them as below. The
same skill runs from a developer's machine and from the review workflow, so a PR
gets the same treatment either way.

## What blocks a merge here

The severity split matters more than any single finding, because it decides what
the author has to act on. Reserve the blocking tier for:

- Violations of `CONSTITUTION.md` or `DESIGN.md`. This includes the agent output
  contract — a command declared `finite` in
  `cmd/gcx/root/testdata/output_classes.json` must emit exactly one JSON value on
  stdout, so a flag that writes to a file and leaves stdout empty is a violation,
  not a nit — and stdout/stderr routing, where a status or fallback notice on
  stdout lands inside a user's redirected output.
- Regressions to a released command surface: a removed or narrowed flag value, a
  changed exit code, or an output shape existing `--json` or `--jq` callers
  depend on. The surface is stable within a major version.
- Behaviour that changes inside a diff described as a refactor or an extraction.
- A credential — token, authorization code, password — that an error message
  echoes back, or that a prompt leaves in the terminal's input queue when the
  flow exits early, where the shell reads it after gcx exits. Where a diff adds a
  control for this, check that it fires on every exit path and not only on
  success.

Naming, structure, duplication, and test-shape findings are nits at most, even
when the argument for them is strong.

## Do not report

- Anything CI already enforces: `mise run lint`, the test suite,
  `reference-drift`, `validate-skills`, and the conformance suites in
  `cmd/gcx/root/`.
- Generated files under `docs/reference/cli/`, and anything in `vendor/`.
- Missing test coverage in files the diff did not touch.

## Report shape

1. **Intent** — the problem and how the change solves it. Say whether the
   approach is sound before listing what is wrong with it.
2. **Blocking** — must be fixed before merge: correctness bugs, regressions to
   shipped behaviour, safety problems, and violations of CONSTITUTION.md or
   DESIGN.md. Behaviour that changes silently inside a change described as a
   refactor belongs here, as does a call to a function, field, or option that
   does not exist.
3. **Other** — should be fixed, does not block. Includes documentation that
   contradicts the code it describes.
4. **Over-engineering** — findings from T12 and T5, ranked and capped below.
5. **The smaller version** — one consolidated remedy. Omit when section 4 is
   empty.
6. **Smaller points** — one line each, no discussion.
7. **Verdict** — approve or request changes, naming the findings that drive it.
   If you would merge a reduced version, say so and say how much smaller.

## Rules that keep the report honest

**One finding per code unit, not per check.** Name the symbol, file, or flag. If
two findings would name the same symbol, they are one finding: merge them and
list every check it trips. The count is evidence — state it — but do not split
one unit across sections. This is the failure mode that makes a review read as
five complaints about one file.

**Rank section 4 by lock-in** — how costly the thing is to remove after release:

1. Exported API with fewer than two callers
2. User-visible surface: flags, output shape, command paths
3. Internal structure: duplicate types, thin wrappers, copied code
4. Tests and unreachable branches

Break ties by line count. Cap the section at six findings; the rest go to
section 6, one line each. The ranking is mechanical on purpose, so two reviewers
produce the same order.

**Give one remedy, not one per finding.** Section 5 is an ordered list of
concrete deletions and merges resolving everything in section 4, with the
resulting size change stated against the real diff size. Size is evidence for
the whole set. It is not a finding of its own and does not belong in section 4.

**State what would overturn each blocking finding.** It forces you to look for
the author's reasoning before you write, and gives them something specific to
answer instead of a verdict to argue with. A call-site comment explaining a
decision does not settle the question, but reviewing as though it is not there
wastes a round.

**Large is not the same as padded.** A three-command feature with generated
reference docs and real client tests is legitimately big. Say so. Judge the diff
against the problem, not against a line count.

**Blocking findings are ordered by severity too.** A regression to shipped
behaviour outranks a violation a maintainer can waive by writing the waiver into
the PR. Say which is which — otherwise five blocking findings read as a heavier
verdict than the change deserves.

## When a workflow invoked this

Unattended, four things change. Everything else is the same review.

- **Post without asking.** The offer below exists because a human is present.
  In CI nobody can answer, and the trigger is the consent.
- **Never approve or request changes.** Post as a comment whatever you found.
  Whether a finding is cheap enough to merge over is a judgement about the
  author's time that an unattended run cannot make.
- **Drop what you could not establish** rather than labelling it unverified.
  There is no author in the loop to answer a speculative finding, and a wrong
  one costs them a public round of disproof.
- **Tighter caps**: at most three blocking findings and eight comments in total.
  Drop the tail rather than downgrading it, and say in the summary that you did.
- **Close the summary with exactly this line**, so an author who pushes a fix
  knows how to ask again:

  > Comment `@claude review` for a fresh review.

Silence is a valid result. If nothing meets the bar, say the diff looked clean
in one line and stop. Do not manufacture a finding.

## Offering to post the review

After the report, offer to post it to the PR as inline comments. Do not post
without an explicit yes. This is outward-facing, it goes out under the
invoker's identity on someone else's PR, and it cannot be quietly undone.

Present the event choice with a recommendation and the reason for it:

| Report state | Recommend |
|---|---|
| Section 2 empty, section 3 empty | `APPROVE` |
| Section 2 empty, section 3 has findings | `APPROVE` with the comments attached |
| Section 2 has findings, all cheap to fix, none a regression to shipped behaviour | `COMMENT` |
| Section 2 has a regression, a safety defect, or anything expensive | `REQUEST_CHANGES` |

State the recommendation, then let the invoker choose. The report's verdict
judges the code; the event is a social act on a colleague's work, and that
call is theirs.

### Mechanics

One `POST` creates the whole review — summary body and every inline comment —
so the author gets one notification instead of ten:

```bash
gh api repos/{owner}/{repo}/pulls/{n}/reviews -X POST --input review.json
```

Each comment needs `path`, `line`, `side` (`RIGHT` for the post-change file).
A range needs `start_line` and `start_side` as well.

**Verify every line number against the PR head**, not against diff line
numbers — they are not the same, and a wrong anchor puts a comment on
unrelated code:

```bash
git fetch origin pull/{n}/head:refs/pr/{n}
git show "refs/pr/{n}:path/to/file.go" | grep -n "<the code you are commenting on>"
```

The line must fall inside a diff hunk. Context lines within a hunk are fine;
lines outside every hunk are rejected. A `suggestion` block replaces exactly
the anchored lines, so match the target's indentation — tabs in Go — or it
applies wrong.

Report sections map like this: sections 2, 3, 4 and 6 become inline comments
at the symbol each names; sections 1, 5 and 7 have no line to attach to and
become the summary body.

### Rewrite the findings as comments

Do not paste report prose into a thread. A finding that runs four paragraphs
in a document is unreadable in a review thread.

- Two to four sentences. Lead with the mechanism, end with the ask.
- Turn "what would overturn this" into a real question the author can answer.
- Open with a bold label saying what the author has to do about it. The
  summary names only the blocking findings, so without a label the author has
  to work out which of the remaining comments they are obliged to act on.
- Use a `suggestion` block for anything that is a concrete one-line change.
- State conclusions, not process. "the extraction preserves every check in
  both paths: state → … → all 8 `Result` fields" is useful. "I checked this
  line by line rather than take it on trust" is about you.
- No opening compliment. If the work deserves praise, one specific line at
  the end carries more than a warm opener.

When one finding covers several sites, anchor it at one and name the others
inline (`also flow.go:160, gcom.go:145`). Ten comments is a review; thirty is
noise.

### Labels

The label follows from the section, so it is mechanical rather than a second
judgement call:

| Section | Label | Means |
|---|---|---|
| 2 Blocking | `**required**` | fix before merge |
| 3 Other | `**recommended**` | should fix, does not block the merge |
| 4 Over-engineering | `**followup**` | fine to defer to its own PR |
| 6 Smaller points | `**nit**` | take it or leave it |

Written out:

> **required** — this returns on every `readLine` error, not just the `Close`
> one the comment describes. …

Two adjustments. A section 4 finding small enough to fix in the same sitting
can be `**nit**` instead. A `**nit**` the author is free to ignore should say
so in the text, not only in the label.

Say what the labels mean once, in the summary body, with the counts — the
shape of the review is useful before the author reads any of it:

> comments below are prefixed **required** (2), **recommended** (4),
> **followup** (2), **nit** (2)

Never label a comment `**required**` unless it is in section 2 of the report.
An inflated label is the fastest way to make every future review's labels
worth ignoring.

## Language

Write prose in simple English, as specified by ASD-STE100. Short sentences,
active voice, one idea per sentence. This applies to prose only — not to
identifiers, code comments, suggested diffs, or quoted output.

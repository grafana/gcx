---
name: review-pr
description: Review someone else's gcx pull request, produce a ranked report with a verdict, and optionally post it to GitHub as line-anchored inline comments. Covers what to report, in what order, when to stop, how to rank over-engineering findings, how to judge whether a large diff is justified, and how to turn the report into review comments with an APPROVE/COMMENT/REQUEST_CHANGES recommendation. Trigger on "review this PR", "review #NNNN", "code review this branch", "is this diff over-engineered", "post the review", "add review comments to this PR". NOT for self-review before pushing your own work — use contribute for that.
---

# Reviewing a gcx pull request

Use this skill to review work you did not write. It owns the **report**: what
goes in it, in what order, and when to stop.

It defines no checks of its own. Run the checks where they already live:

- `.agents/skills/contribute/references/self-review.md`. Read
  **Evidence discipline** first. It sets what a finding may conclude. Then run
  the triggers that fire for the diff.
  [T12](../contribute/references/self-review.md#t12-over-engineering) is
  the over-engineering rubric.
  [T5](../contribute/references/self-review.md#t5-shared-infrastructure)
  covers code that repeats something the repo already has.
- AGENTS.md, for the compliance hierarchy. Check all four levels in order.

Authors run the same triggers before they push. Read their reasoning and earlier
review replies before deciding whether a finding was missed or deliberately
deferred.

## The correctness pass

**You owe every PR a correctness pass, and you run it yourself.** Read the diff
for the defects a reviewer is here to catch: a nil or error path that returns
the wrong value, a boundary the change moved, a resource that leaks on the
failure branch, concurrent access the diff introduced, and a call whose
behaviour the diff changed without changing its callers.

Then run **the triggers above** that fire for this diff, plus the compliance
hierarchy. Together they are the whole review. There is no second reviewer to
defer to and no plugin to invoke, on a developer's machine or in the review
workflow.

The same skill runs on a developer's machine and in the review workflow. A PR
gets the same treatment either way.

## What blocks a merge here

These criteria determine severity in both local and unattended reviews. Reserve
the blocking tier for demonstrated correctness or safety defects, regressions,
and violations of the repository's hard rules:

**A correctness or safety defect in reachable behaviour.** Show the input, call
path and materially incorrect result, failed operation or unsafe effect. A hypothetical
future server response or refactor is not evidence of a current defect; follow
the evidence discipline above.

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

Naming, structure, duplication and test shape alone do not block a merge,
including findings from T5 and T12. Label small optional improvements `nit` and
larger optional work `followup`. Use `recommended` for a demonstrated issue worth
fixing that does not meet the blocking criteria. If a structural concern also
causes a blocking defect, require the smallest fix for that defect and keep any
broader cleanup optional. Cite the specific rule for a claimed CONSTITUTION or
DESIGN violation.

## Do not report

- Anything CI already enforces: `mise run lint`, the test suite,
  the `Documentation` build, `validate-skills`, and the conformance suites in
  `cmd/gcx/root/`.
- The generated release snapshot `docs/sources/cli-reference.md`, and anything in `vendor/`.
- Missing test coverage in files the diff did not touch.

## Report shape

Use bold text for headings in the report, not markdown headings. Keep Intent and
Verdict to at most two sentences each; give each finding only the explanation
needed to establish its impact.

1. **Intent.** The problem and how the change solves it. Save the judgement for
   the verdict.
2. **Blocking.** Findings that meet [What blocks a merge here](#what-blocks-a-merge-here),
   worst first. If nothing blocks the merge, omit this section.
3. **Other findings.** Everything that does not block, worst first. Include
   documentation that contradicts the code it describes. Give each finding the
   space its argument needs and no more. Some take a paragraph. Most take a
   line. The label shows the difference, so a one-line nit and a
   worth-fixing defect can share the list.
4. **Fix summary.** One combined remedy. Omit this section when sections
   2 and 3 are empty.
5. **Verdict.** Approve when no blocking findings remain; otherwise request
   changes and name the findings that decide it. In an unattended run, omit the
   approval verdict and state only which findings block and which are optional.

## Rules that keep the report honest

**One finding per code unit, not per check.** Name the symbol, file, or flag.
Combine related concerns about the same symbol and explain their shared cause.
Keep the required remedy distinct from optional cleanup; sharing a function
does not give every suggestion the highest severity.

**Rank structural findings in Other findings by lock-in.** Lock-in is what the
thing costs to remove after release; it orders optional work, not its severity:

1. Exported API with fewer than two callers
2. User-visible surface: flags, output shape, command paths
3. Internal structure: duplicate types, thin wrappers, copied code
4. Tests and unreachable branches

Break ties by line count. Cap Other findings at six and omit the lowest-ranked
optional items. Caps limit the report, not the correctness pass, and are never
quotas to fill.

**Give one combined fix summary.** List the smallest changes that address the
findings, separating required fixes from optional work. When proposing a
reduction, state the resulting size change against the real diff size. Size is
supporting evidence, not a finding by itself.

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

## Reviewing another round

Read earlier reviews and author replies, including resolved discussions when
available. The workflow's previous-findings list contains only excerpts from
unresolved inline threads; it omits resolved decisions and summary-only findings.
Use it as a starting point, not a complete history. State any material gap in
the history you could read.

Compare the previous reviewed commit with the current PR head using
`git diff <previous-review-commit>..<review-head>`. Keep the full correctness
pass, but distinguish a regression introduced by a fix from an older issue
newly noticed. Identify the introducing change before blaming a fix.

For earlier findings, say which are addressed, still present, withdrawn as
incorrect, or explicitly deferred. Summarize a still-open finding with a link
to its existing thread instead of posting it again. Preserve an agreed deferral
unless new evidence meets the blocking criteria; explain that evidence before
changing the severity. A resolved thread alone does not prove a bug was fixed.

If the author implemented the previous remedy and it was insufficient,
acknowledge that the advice was incomplete and show the remaining failure.
Recheck the full call path, existing tests and author rationale before replacing
the remedy. Do not require a broader refactor merely because it is now preferred.

## When a workflow invoked this

Six things change when no human is present. The rest of the review is the same.

- **Deliver without asking.** The offer below applies when a human can answer.
  In CI nobody can, and the trigger is the consent. Deliver through *Workflow
  delivery* below. You do not publish anything yourself.
- **Never approve or request changes.** Post whatever you found as a comment.
  Whether a finding is cheap enough to merge over is a judgement about the
  author's time. An unattended run cannot make it.
- **Drop what you could not establish.** Do not label it unverified. No author
  can answer a speculative finding here, and a wrong one makes them disprove it
  in public.
- **Use tighter comment caps**: at most three blocking inline comments, and
  eight inline comments in total. Summarize any additional established blockers
  in the review body so the author can address them in the same round. Drop
  excess optional findings.
- **Close the summary with this exact line**:

  > Comment `@claude review` for a fresh review.

- **Keep the same severity labels.** `recommended`, `followup` and `nit` remain
  non-blocking. Running unattended does not increase a finding's impact.

Silence is a valid result. If there are no findings, say so in one line; on a
later round, also summarize prior findings' dispositions. Never invent a finding.

Silence is not the same as a pass you did not run. If your correctness pass
could not complete, say that instead. Never deliver "no findings" for a review
that did not happen.

Follow [Reviewing another round](#reviewing-another-round) whenever earlier
reviews exist.

### Workflow delivery

Return the review as this run's structured output:

```json
{"review": {"body": "<the summary>", "comments": []}}
```

`body` is the summary. `comments` carries the inline findings, mapped exactly as
in *Mechanics* below: `path`, `line`, `side` and `body` on each one, plus
`start_line` and `start_side` for a range. A complete review with no new inline
findings keeps an empty `comments` array and reports any prior findings' status
in `body`. If your correctness pass could not complete, return `{"review": null}`;
that is not a clean review, and the workflow fails the run instead of publishing one.

Anchor every line against the **PR head**, not the tree you are standing in.
A workflow run checks out the merge ref, and its line numbers can differ from
the head's. *Mechanics* below has the commands for checking.

**Publish nothing in this mode** — not through `gh`, not through an
inline-comment tool, not as a PR comment. The workflow submits the payload as
one `COMMENT` review against the commit it gave you, appends its own run marker,
and checks GitHub's response before the job goes green. There is no label to
set and no marker for you to carry.

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
comment. The author then gets one notification instead of ten. Pass the JSON
directly on stdin; the tracked allowlist grants no file-write permission. A
quoted heredoc keeps backticks, dollar signs, and suggestions literal:

```bash
gh api repos/grafana/gcx/pulls/{n}/reviews -X POST --input - <<'REVIEW_JSON'
{
  "event": "COMMENT",
  "commit_id": "<reviewed head SHA>",
  "body": "<the summary>",
  "comments": []
}
REVIEW_JSON
```

Replace the placeholders, use the chosen event for a human-invoked review,
and put the inline findings in `comments`. Leave it empty for a clean review.
Keep the endpoint first, unquoted, and followed by `-X POST`, matching the review
permission in `.claude/settings.json`. Record the returned review ID. If
publication is denied, stop and report the failed command. Do not retry through
scripts, post test comments, or switch to a regular PR comment.

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

Blocking and Other findings become inline comments at the symbols they name,
subject to the workflow comment caps. Intent, Fix summary and Verdict become
the summary body.

### Rewrite the findings as comments

Never paste report prose into a thread. A finding that fills four paragraphs in
a document is too long to read in a review thread.

- Write two to four sentences. Start with the mechanism. End with the ask.
- Turn "what would overturn this" into a question the author can answer.
- Start with a bold label that says what the author must do. The summary names
  the blocking findings and prior findings' dispositions. Without a label, the
  author has to work out which of the other comments they must act on.
- Use a `suggestion` block for any concrete one-line change.
- State conclusions, not process. "The extraction preserves every check in both
  paths: state → … → all 8 `Result` fields" helps the author. "I checked this
  line by line" describes you instead.
- Do not open with a compliment. Where the work deserves praise, one specific
  line at the end says more.

When one finding covers several sites, anchor it at one site and name the others
in the text (`also flow.go:160, gcom.go:145`).

### Labels

The label follows from the section. That makes it mechanical instead of a second
judgement:

| Section | Label | Means |
|---|---|---|
| 2 Blocking | `**required**` | fix before merge |
| 3 Other findings | `**recommended**` | should fix, does not block the merge |
| 3 Other findings | `**nit**` | take it or leave it |
| 3 Other findings | `**followup**` | fine to defer to its own PR |

Written out:

> **required** — this returns on every `readLine` error, not just the `Close`
> one the comment describes. …

Where the author may ignore a `**nit**` or defer a `**followup**`, say so in the
text as well as the label.

Say what the labels mean once, in the summary body, with the counts. The author
wants the shape of the review before the detail:

> comments below are prefixed **required** (2), **recommended** (2),
> **followup** (1), **nit** (1)

Only label a comment `**required**` when it sits in section 2 of the report. One
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

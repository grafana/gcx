---
name: independent-review
description: Get your own draft gcx PR reviewed by fresh, independent claude and codex processes (no shared context with this session), then triage and fix their findings. Use after creating a draft PR and CI is green, before it would be marked ready for review, or when asked for an independent/second-opinion review. NOT for reviewing someone else's PR — use review-pr for that.
argument-hint: (none — run from the PR's worktree)
allowed-tools: Bash(.agents/skills/independent-review/run-reviews *)
---

# Independent review

The point is reviewers that did NOT write the code. Never substitute an Agent/fork
subagent for these — they inherit this session's assumptions.

Requires the `claude`, `codex` and `gh` CLIs on `PATH`, authenticated.

## 1. Run the reviewers

From the repo root of the PR's worktree, with everything pushed:

```bash
.agents/skills/independent-review/run-reviews <scratchpad>/review-<pr>
```

Run it in the background — both reviews together take 5–20 min. Outputs:
`claude.md`, `codex.md` (+ `.err`, `status`). A non-zero exit or empty `.md` for one
reviewer: read its `.err`, report it, carry on with the other.

## 2. Triage

Reviewers are wrong often. For each finding, read the code and decide:

- **Real** — fix it.
- **Not real** — note why in one line (it goes in the report).
- **Judgment call** (design, scope, behaviour change) — don't fix; ask the user.

Findings both reviewers raised independently are the strongest signal.

## 3. Fix and push

Fix real findings as normal commits following the
[Mandatory Pre-Commit Checklist](../../../AGENTS.md#mandatory-pre-commit-checklist),
push, and watch CI until green. Don't re-run the reviewers after fixes unless the
fixes were large.

## 4. Report

Leave the PR as a draft — marking it ready is the user's call. Report: PR link, what
was fixed, what was dismissed (one line each with reason), and open judgment calls.

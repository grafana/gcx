---
name: propose-feature
description: Turn an idea or rough notes into a reviewable gcx feature proposal through a short interview. Use when asked to propose, draft, or file a feature or improvement.
---

# Propose Feature

Produce one proposal that a reader with no access to the conversation can understand and assess. Use the common [Feature proposal form](../../../.github/ISSUE_TEMPLATE/2-feature-proposal.yml) for requests, improvements and proposed commands. This skill ends with a reviewable draft or an authorized, filed issue.

Read the [contribution workflow](../../../CONTRIBUTING.md#contribution-workflow) for when a proposal is needed and what follows acceptance. Use [`contribute`](../contribute/SKILL.md) when selecting the contribution route or preparing implementation. Follow that policy; a user may also request a proposal draft for work whose route does not require one.

The repository is public. Follow the anonymization rules in the GitHub Issues section of [CLAUDE.md](../../../CLAUDE.md). Use public evidence or independently developed material suitable for public review. Keep restricted research outside the checkout; removing links or attribution does not make internal findings publishable.

## Ground the idea

Establish the concrete user problem and investigate enough context to make the direction credible:

- Check [VISION.md](../../../VISION.md) for product fit and [CONSTITUTION.md](../../../CONSTITUTION.md) for invariants.
- Read relevant code and existing command help to understand the current behavior and nearby capabilities.
- Search existing issues and PRs for duplicates or decisions to reuse.
- Investigate relevant public APIs or dependencies where they affect feasibility. Scale research to the proposal; state unknown API readiness, permissions, data availability or other feasibility gaps explicitly.

Separate observed facts, user-reported needs and proposed behavior. When evidence contradicts the idea or an invariant prevents it, explain the conflict and reshape the proposal before drafting. A proposal can carry unresolved feasibility questions for maintainer review.

## Interview briefly

Reuse decisions and evidence already available. Ask only what changes the proposal: who has the problem, the scenario that shows it, where scope stops and what completion looks like. Use a small batch of questions with recommended answers and tradeoffs where useful; continue independent research while answers are pending.

Stop interviewing when the problem, proposed UX and checkable acceptance criteria are clear. Record unresolved design choices as open questions.

## Draft

Cover the form's three required sections. Add optional sections only where they help review:

```markdown
## Problem
<who encounters what today, and why current behavior or workarounds are insufficient>

## Proposed UX
<the user experience, illustrated at a scale appropriate to the change>

## Acceptance criteria
- [ ] <observable outcome a reviewer can check>

## Scope and open questions
<boundaries, alternatives and unresolved feasibility or design questions>

## Implementation notes
<relevant constraints, verified dependencies and willingness to implement, where useful>
```

Write a title that names the outcome. Use a short before/after for a small improvement; add command, output or error examples when they make a larger proposal concrete. [Issue #1469](https://github.com/grafana/gcx/issues/1469) illustrates a detailed UX proposal, rather than a required length or design checklist.

Check every existing command, flag and file path against the code or command help. Mark new commands and behavior as proposed. Each acceptance criterion must be checkable without the conversation. Label feasibility assumptions and user-reported claims so readers can distinguish them from verified facts.

## Review and file

Show the full draft with its title, issue type (`Feature`), labels (`action/needs-triage`) and any proposed assignees. Revise until it is ready for approval.

Publish only when the user's instructions authorize the concrete issue and destination; reuse authorization already given for the same action. Until then, hand back the private draft. Apply the same authorization boundary to assignments, links and changes to other shared records.

When filing is authorized:

- Create the issue with `gh issue create --body-file`, applying the form's label.
- Set the type to Feature with the GraphQL `updateIssueIssueType` mutation, looking up the existing type ID from the organization's `issueTypes`. Preserve organization-wide type administration.
- Read back the issue body and metadata, then report its URL and any other authorized changes.

The next step is maintainer review. The accepting maintainer identifies any needed design or implementation planning under the [contribution workflow](../../../CONTRIBUTING.md#contribution-workflow); reuse accepted issue decisions in those artifacts.

---
name: propose-feature
description: Turn an idea or rough notes into a feature proposal, filed as a GitHub issue for grafana/gcx, through a short interview. Use when asked to propose, draft, or file a feature idea; use create-rfc once a proposal is accepted and needs a design.
---

# Propose Feature

Produce one feature proposal: a GitHub issue that a reader with no access to the conversation can understand and act on. A proposal is the first step of the [contribution flow](../../../CONTRIBUTING.md#proposing-features). Maintainers accept or reject it. An accepted proposal leads to an RFC when it needs a separate design, then implementation; smaller changes proceed directly to implementation. It describes a problem worth solving and a direction to solve it. It doesn't settle the design. This skill ends once the proposal is filed and linked, or once the draft is handed back.

The repository is public. Follow the anonymization rules in the GitHub Issues section of [CLAUDE.md](../../../CLAUDE.md). Use public evidence or independently developed material suitable for public review. Keep restricted research outside the checkout; removing links or attribution does not make internal findings publishable.

Read the [issue-first policy](../../../CONTRIBUTING.md#new-commands-need-an-issue-first) before applying the flow. Preserve its exemptions; do not require a proposal issue for exempt work. The user may still explicitly request a proposal draft for that work. Use the [New Command Proposal template](../../../.github/ISSUE_TEMPLATE/2-new-command-proposal.yml) when the user plans to build a command or change its interface, and the [Feature Request template](../../../.github/ISSUE_TEMPLATE/4-feature-request.yml) when they are asking for a capability.

## Two gates

Every proposal must pass both gates before it is drafted:

- **Value**: a named user (a person, an agent or a contributor) hits a concrete problem today, and the proposal improves their work. [VISION.md](../../../VISION.md) confirms the proposal belongs in gcx.
- **Achievable**: the APIs and data it depends on exist, it fits [CONSTITUTION.md](../../../CONSTITUTION.md), and its scope can land as a sequence of PRs.

Establish the facts behind both gates yourself:
- Read the relevant code and `--help` output.
- Search existing issues and PRs with `gh` for duplicates.
- Read the server API source where it's available.

Ask the user only for intent, priority and scope. When a gate fails, say which one and why, then reshape the idea, point to the existing issue, or stop.

## Interview briefly

Ask in rounds, with numbered questions and a recommended answer for each. Ask only what changes the proposal:
- who has the problem
- the scenario that shows it
- where the scope stops
- what "done" looks like

Two or three rounds are usually enough. Leave implementation choices, edge cases and API shapes to the RFC; record them as open questions instead of resolving them.

## Draft

Start from the selected issue template. For command proposals, cover every required field: the user outcome, why an existing command cannot provide it, proposed path/arguments/flags/output, stable or experimental, the serving API and its readiness/auth/limits, and whether it modifies anything. Keep the command sketch provisional; detailed design belongs in the RFC. For feature requests, cover the idea, existing workarounds and priority. Add scope and acceptance criteria where useful.

Use this structure when no existing template fits, and drop any section that would be empty:

```markdown
## Problem
<who hits what, today; 2-4 short bullets or a small table>

## Proposed UX
<the direction in 2-4 bullets, then a short command block. Label every command that doesn't exist yet as a proposal>

## Implementation notes
<only constraints a reader would otherwise miss: shared code to reuse, safety rules, slicing>

Out of scope: <one line>

## Acceptance criteria
- [ ] <observable outcome a reviewer can check>
```

Write for a human skimming the issue:
- The title names the outcome.
- Each bullet makes one point; when a list has several bullets, lead each with its point in bold.
- Use commands and tables where they replace paragraphs.

Check every existing command, flag and file path in the draft against the code or `--help`. Each acceptance criterion must be checkable without the conversation.

## Review and file

Show the full draft with its title, type (Feature), labels and assignees. Name every claim that rests only on the user's word. Revise until the user approves.

Filing, assigning and linking happen only on explicit request:
- Create the issue with `gh issue create --body-file`.
- Set the type to Feature with the GraphQL `updateIssueIssueType` mutation. Get the type ID from the organization's `issueTypes`.
- When the proposal follows existing work, link it with `Related: #<issue>` in that PR's description, not a closing keyword.

Report the issue URL and anything changed elsewhere. Remind the user that the next step is maintainer review of the proposal. Use `create-rfc` after acceptance when the work needs a separate design; otherwise, start a per-PR OpenSpec change.

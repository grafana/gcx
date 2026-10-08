---
name: propose-feature
description: Turn an idea or rough notes into a feature proposal, filed as a GitHub issue for grafana/gcx, through a short interview. Use when asked to propose, draft, or file a feature idea; use create-rfc once a proposal is accepted and needs a design.
---

# Propose Feature

Produce one feature proposal: a GitHub issue that a reader with no access to the conversation can understand and act on. A proposal is the first step of the [contribution flow](../../../CONTRIBUTING.md#proposing-features). Maintainers accept or reject it, and an accepted proposal leads to RFCs and then implementation. It describes a problem worth solving and a direction to solve it. It doesn't settle the design. This skill ends once the proposal is filed and linked, or once the draft is handed back.

The repository is public. Follow the anonymization rules in the GitHub Issues section of [CLAUDE.md](../../../CLAUDE.md). Leave out private trackers, internal documents, meeting notes and closed-source internals; restate any reasoning from them that the reader needs.

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

Use this structure, and drop any section that would be empty:

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

Report the issue URL and anything changed elsewhere. Remind the user that the next step is maintainer review of the proposal. `create-rfc` comes after it is accepted.

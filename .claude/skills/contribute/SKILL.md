---
name: contribute
description: >-
  Prepare a contribution to the gcx repository: report or fix a bug, make a
  maintenance change, or propose and implement a feature. Use inside a gcx
  checkout for contribution routing, planning, implementation and self-review;
  operating a Grafana instance uses the portable gcx/product skills.
---

# Contribute to gcx

Read [AGENTS.md](../../../AGENTS.md) and the authoritative
[contribution workflow](../../../CONTRIBUTING.md#contribution-workflow).
Use its route and ownership guidance; the contributor does not need to choose a
provider, datasource, RFC or OpenSpec workflow upfront.

## 1. Establish the current step

Identify the requested outcome and the existing issue, accepted decisions, RFC
or implementation branch. Inspect those records and relevant code before asking
questions. State the applicable route, current step and missing maintainer
decision. Reuse settled scope and design at each handoff.

Work within the request and existing authorization. A draft-only or review-only
request ends with that artifact. An implementation request continues through
authorized work once applicable maintainer decisions are settled. Prepare the
concrete result before requesting publication permission; reuse authorization
already given for the same destination and scope.

## 2. Prepare the issue when the route needs one

Reuse an existing issue. When its maintainer decision is settled, continue to
the needed planning or implementation rather than drafting another proposal.

- **Bug:** use the [Bug report form](../../../.github/ISSUE_TEMPLATE/1-bug-report.yml).
  Establish actual and expected behavior, reproduction or evidence, and relevant
  version/environment. Search for an existing report and continue there. Record
  verified findings separately from assumptions. The maintainer's acknowledgement
  is judgment, not a checklist or a promised deadline.
- **Feature:** use [propose-feature](../propose-feature/SKILL.md) to research and
  draft the common proposal. Show a concrete problem, proposed UX and checkable
  acceptance criteria; scale examples to the work and identify unresolved facts.

Follow AGENTS.md privacy rules. File or change a shared issue within publication
authorization. Follow the maintainer decision recorded on the issue; keep
additional design for a bug on that same issue.

## 3. Load only the planning that is needed

The accepting maintainer identifies necessary planning under CONTRIBUTING.
Use [create-rfc](../create-rfc/SKILL.md) when a separate design is warranted,
[review-rfc](../review-rfc/SKILL.md) for design review, and
[update-rfc](../update-rfc/SKILL.md) when explicitly asked to reconcile existing
implementation evidence. Reuse the issue's accepted behavior and scope.

When a tracked implementation plan is warranted, use
[openspec-propose](../openspec-propose/SKILL.md) for **one PR**. Link the issue and
any RFC; scope the change to that PR's work and criteria. Use the OpenSpec
apply/update/sync/archive workflows as needed. Get artifact IDs, paths and
requirements from the CLI's active schema. An RFC can describe the overarching
design and PR sequence; straightforward slices can be tracked on the issue.

## 4. Implement with the appropriate references

For a bug, inspect the relevant subsystem and add appropriate regression
coverage. For documentation or test-only work, inspect the affected material.

For a capability or command change, read
[capability implementation](references/capability-implementation.md) for placement,
backend readiness and the command contract. Carry decisions into the existing
planning record rather than producing another mandatory document. Then load:

- [Provider implementation](references/provider-implementation.md) for a Cloud
  product provider, including conditional resource adapters and smoke tests.
- [Datasource implementation](references/datasource-implementation.md) for a new
  datasource kind, including shared queries, Explore links, codecs and routing.
- [Distribution and gates](references/distribution-and-gates.md) when resource or
  datasource wiring, command annotations, or bundled skill coverage changes.

Use the existing subsystem implementation for other changes. Resolve routine
choices from repository evidence; ask about unresolved choices that materially
change scope or behavior. Preserve CONSTITUTION, DESIGN and architecture rules.
Legacy CLI ports use the deliberately human-driven
[migration workflow](../migrate-provider/SKILL.md); its unaudited steps are not an
automatic implementation path from this entrypoint.

## 5. Verify and prepare review

Read [self-review](references/self-review.md) and apply checks triggered by the
actual diff, including incremental fixes. Run AGENTS.md quality checks and
relevant domain verification using the built binary. Report failed or unavailable
checks with their reason; a test or probe that did not run is not a pass.

When OpenSpec is used, keep task state consistent with implementation and
verification evidence and finish/archive the PR-scoped change through its
workflow. RFC reconciliation is a separate requested task. Implementation PRs
reference the issue; the final completing PR closes it. Summarize the change,
checks, unresolved assumptions and documented deviations for review.

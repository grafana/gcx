---
type: bugfix-spec
title: "{Title}"
status: draft
beads_id: # optional — linked bead issue ID
created: YYYY-MM-DD
---

# {Title}

## Current Behavior

<!-- What happens now? Include specific steps to trigger the bug,
error messages, and observable symptoms. Be precise enough that
anyone can reproduce it. -->

## Expected Behavior

<!-- What should happen instead? Describe the correct behavior
after the fix is applied. -->

## Unchanged Behavior

<!-- What existing behavior MUST remain unaffected by this fix?
This is the contract that prevents regressions. Be explicit about
adjacent functionality that should not change.

- Feature X continues to ...
- API endpoint Y still returns ...
- Performance of Z is not degraded
-->

## Steps to Reproduce

<!-- Numbered steps to reproduce the bug. Include:
1. Starting state / preconditions
2. Exact actions
3. Observed result vs expected result
-->

1. ...
2. ...
3. ...

## Root Cause Analysis

<!-- Optional — fill in during investigation if known.
What is the underlying cause? Why does the current code
produce the wrong behavior? -->

## Acceptance Criteria

<!-- Given/When/Then format. At minimum:
- The bug is fixed (primary criterion)
- Unchanged behaviors are preserved (regression criteria)

- GIVEN {precondition}
  WHEN {action}
  THEN {expected outcome}
-->

<!-- NOTE: Bugfix specs produce spec.md only — no plan.md or tasks.md.
The fix is typically small enough to implement directly from the spec. -->

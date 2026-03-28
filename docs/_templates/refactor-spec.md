---
type: refactor-spec
title: "{Title}"
status: draft
beads_id: # optional — linked bead issue ID
created: YYYY-MM-DD
---

# {Title}

## Current Structure

<!-- Describe the existing code architecture. Include:
- Key files and modules involved
- Current data flow / control flow (ASCII diagram recommended)
- Pain points that motivate this refactor
- Technical debt being addressed
-->

## Target Structure

<!-- Describe the desired architecture after refactoring. Include:
- New file/module organization
- Updated data flow / control flow (ASCII diagram recommended)
- What improves and why
- Key differences from current structure
-->

## Behavioral Contract

<!-- This is the CRITICAL section. A refactor MUST preserve behavior
unless explicitly stated otherwise.

### Invariants (MUST hold after refactor)
- Public API unchanged (function signatures, types, exports)
- Existing test suite passes without modification
- No behavior change unless explicitly listed below
- Performance characteristics maintained (no regressions)

### Intentional Changes (if any)
- List any deliberate behavior changes here
- Each change needs explicit justification
- If none: "No intentional behavior changes."
-->

### Invariants

- Public API unchanged
- Existing test suite passes without modification
- No behavior change unless listed under Intentional Changes
- Performance characteristics maintained

### Intentional Changes

<!-- "No intentional behavior changes." OR list specific changes with rationale -->

## Migration Steps

<!-- Ordered steps for the refactoring. Each step should be
independently verifiable — the system should work after each step.

1. Step 1: ...
   Verify: tests pass, no behavior change
2. Step 2: ...
   Verify: ...
-->

## Acceptance Criteria

<!-- Given/When/Then format. Must cover:
- Behavioral contract invariants hold
- Target structure achieved
- Any intentional changes work correctly

- GIVEN {precondition}
  WHEN {action}
  THEN {expected outcome}
-->

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| | | |

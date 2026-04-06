---
type: feature-tasks
title: "{Title}"
status: draft
spec: # path to spec.md
plan: # path to plan.md
created: YYYY-MM-DD
---

# Implementation Tasks

## Dependency Graph

<!-- ASCII diagram showing task dependencies. Use arrows to show
blocking relationships.

```
T1 (foundation task)
 ├──→ T2 (depends on T1)
 └──→ T3 (depends on T1)
       └──→ T4 (depends on T3)
```
-->

## Wave 1: {Wave Title}

### T1: {Task Title}

**Priority**: P0-P4
**Effort**: Small | Medium | Medium-Large | Large
**Depends on**: {task IDs or "none"}
**Type**: task | chore

{Description of what this task implements.}

**Deliverables:**
- {file path or artifact}

**Acceptance criteria:**
- GIVEN {precondition} WHEN {action} THEN {outcome}

---

<!-- Repeat Wave/Task pattern for each wave.
Group tasks by dependency wave — tasks in the same wave
can execute in parallel. -->

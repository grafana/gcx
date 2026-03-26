---
type: feature-tasks
title: "Three-Stage Blackbox Verification for /migrate-provider Skill"
status: draft
spec: spec/feature-three-stage-blackbox-verification/spec.md
plan: spec/feature-three-stage-blackbox-verification/plan.md
created: 2026-03-24
---

# Implementation Tasks

## Dependency Graph

```
T1 (SKILL.md core structure) ──► T2 (artifact templates + envelopes)
                                          │
                                          ▼
                               T3 (orchestration + checklists + red flags)
                                          │
                                          ▼
                               T4 (recipe cross-reference update)
                                          │
                                          ▼
                               T5 (review walkthrough + final verification)
```

## Wave 1: Core Structure

### T1: Write SKILL.md 3-stage skeleton with gates and frontmatter

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Replace the current SKILL.md with the new 3-stage structure. Write the frontmatter, skill description trigger, prerequisites section (carried from current SKILL.md), and the three stage headers (Audit, Build, Verify) with gate definitions between each pair. Each stage gets a brief prose description of its purpose and the gate condition that must pass before the next stage begins. This task establishes the document skeleton that subsequent tasks fill in.

**Deliverables:**
- `.claude/skills/migrate-provider/SKILL.md` -- rewritten with 3-stage skeleton, gate definitions, prerequisites

**Acceptance criteria:**
- GIVEN the rewritten SKILL.md
  WHEN an agent reads it
  THEN it finds exactly three stages (Audit, Build, Verify) with explicit gates between each pair.
  (Traces to AC-01)

- GIVEN the SKILL.md
  WHEN an agent reads the Audit gate
  THEN it states that user approval of all three artifacts (parity table, architectural mapping, verification plan) is required before proceeding to Build.
  (Traces to FR-001, Negative Constraint: no skipping gates)

- GIVEN the SKILL.md
  WHEN an agent reads the Build gate
  THEN it states that `GRAFANACTL_AGENT_MODE=false make all` must exit 0 before proceeding to Verify.
  (Traces to FR-009, AC-07)

- GIVEN the SKILL.md
  WHEN an agent reads the Verify gate
  THEN it states that user review of the comparison report is required, with all discrepancies justified or fixed.
  (Traces to FR-012, AC-09)

---

## Wave 2: Artifact Templates and Sealed Envelopes

### T2: Add artifact format templates and sealed envelope sections

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T1
**Type**: task

Fill in the Audit stage with the three artifact format templates (parity table, architectural mapping, verification plan) as fenced code blocks agents copy during execution. Add the sealed envelope sections for Build envelope and Verify envelope, each with Description, Receives, Produces, and Enforcement subsections. Add the comparison report format template to the Verify stage. This is the highest-density content task -- it defines exactly what each stage produces and what each downstream stage receives.

**Deliverables:**
- `.claude/skills/migrate-provider/SKILL.md` -- Audit section with parity table template, architectural mapping template, verification plan template; Build envelope section; Verify envelope section; Verify stage with comparison report template

**Acceptance criteria:**
- GIVEN the Audit stage section
  WHEN an agent reads the parity table template
  THEN the template contains columns for cloud CLI command, grafanactl equivalent, status (Implemented / Deferred / N/A), and notes, with the instruction that every cloud CLI subcommand MUST have a row.
  (Traces to FR-003, AC-02)

- GIVEN the Audit stage section
  WHEN an agent reads the architectural mapping template
  THEN the template contains explicit entries for all five pattern pairs: (a) flat client to TypedCRUD[T], (b) CLI flags to Options struct, (c) output formatting to codec registry with K8s envelope, (d) types to Go structs with omitzero, (e) provider registration to adapter.Register() with blank import.
  (Traces to FR-004, AC-03)

- GIVEN the Audit stage section
  WHEN an agent reads the verification plan template
  THEN the template has sections for automated tests (with specific test names), smoke test commands (with concrete argument placeholders), and build gate checkpoints.
  (Traces to FR-005, AC-04)

- GIVEN the Build envelope section
  WHEN an agent reads its "receives" list
  THEN the list contains exactly: parity table, architectural mapping, recipe reference. The list MUST NOT mention verification plan.
  (Traces to FR-006, AC-05, Negative Constraint: no verification plan in Build)

- GIVEN the Verify envelope section
  WHEN an agent reads its "receives" list
  THEN the list contains exactly: verification plan. The list MUST NOT mention Build-stage implementation decisions.
  (Traces to FR-007, AC-06, Negative Constraint: no implementation details in Verify)

- GIVEN the Verify stage section
  WHEN an agent reads the comparison report template
  THEN the template contains sections for per-command pass/fail, diff output for list/get comparisons, and format check results for table/wide/json/yaml.
  (Traces to FR-010, AC-08)

---

## Wave 3: Orchestration, Checklists, and Red Flags

### T3: Add orchestration table, file ownership table, per-stage checklists, and red flags table

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T2
**Type**: task

Add the orchestration table specifying agent strategy per stage (Audit = main, Build = team, Verify = subagent) with the small-provider footnote. Add the file ownership table mapping each recipe phase to the Build-Core or Build-Commands teammate. Add per-stage checklists with checkboxes. Add the Red Flags table with at minimum the four entries from the spec plus entries for envelope isolation violations. Write the Build teammate spawn prompt templates (verbatim text the lead copies) and the Verify subagent spawn prompt template. Document Build-Core to Build-Commands ordering via TaskList.

**Deliverables:**
- `.claude/skills/migrate-provider/SKILL.md` -- orchestration table, file ownership table, per-stage checklists, red flags table, spawn prompt templates, teammate ordering instructions

**Acceptance criteria:**
- GIVEN the SKILL.md
  WHEN an agent reads the orchestration table
  THEN it finds one row per stage: Audit = main context, Build = agent team (Build-Core + Build-Commands), Verify = subagent.
  (Traces to FR-013, AC-11)

- GIVEN the SKILL.md
  WHEN an agent reads the file ownership table
  THEN it finds an explicit mapping of each recipe phase to the teammate (Build-Core or Build-Commands) that owns that phase's files.
  (Traces to FR-019, FR-024, AC-18)

- GIVEN the SKILL.md
  WHEN an agent reads the Build-Core spawn prompt template
  THEN the template contains only Build envelope content (parity table, architectural mapping, recipe reference) and does not contain any verification plan content.
  (Traces to FR-020, AC-13, Negative Constraint: no verification plan in spawn prompts)

- GIVEN the SKILL.md
  WHEN an agent reads the Build-Commands spawn prompt template
  THEN the template contains only Build envelope content and does not contain any verification plan content.
  (Traces to FR-020, AC-13)

- GIVEN the SKILL.md
  WHEN an agent reads the Verify subagent spawn prompt template
  THEN the template contains only Verify envelope content and does not contain Build envelope or implementation details.
  (Traces to FR-022, AC-16)

- GIVEN the SKILL.md
  WHEN an agent reads the teammate ordering instructions
  THEN it finds that Build-Core MUST complete and signal via TaskList before Build-Commands begins.
  (Traces to FR-021, AC-17)

- GIVEN the SKILL.md
  WHEN an agent reads the per-stage checklists
  THEN each stage (Audit, Build, Verify) has its own checklist with checkboxes that agents mark during execution.
  (Traces to FR-014)

- GIVEN the SKILL.md
  WHEN an agent reads the Red Flags table
  THEN it finds at least four entries covering: copying the cloud CLI client verbatim, skipping parity audit, guessing endpoint names, skipping smoke tests, plus entries for reading files outside spawn prompt and merging envelopes.
  (Traces to FR-015, AC-10)

- GIVEN the Build stage instructions
  WHEN the lead orchestrator follows them
  THEN the instructions specify: TeamCreate, spawn Build-Core teammate, wait for Build-Core completion, spawn Build-Commands teammate, wait for Build-Commands completion, BUILD GATE (make all), TeamDelete.
  (Traces to FR-018, FR-023)

- GIVEN the Build stage file ownership table
  WHEN Build-Core teammate checks its ownership
  THEN it owns types.go, client.go, adapter.go, resource_adapter.go, client_test.go and does NOT own provider.go, commands.go, or CLI command files.
  (Traces to FR-019, AC-14, AC-15, Negative Constraint: no cross-file-ownership)

- GIVEN the Audit stage instructions
  WHEN the lead orchestrator reads them
  THEN the instructions state that Audit runs in the lead's main context and MUST NOT be delegated to a subagent or teammate.
  (Traces to FR-017, Negative Constraint: no delegated Audit)

---

## Wave 4: Recipe Cross-Reference

### T4: Update provider-migration-recipe.md with skill structure reference

**Priority**: P1
**Effort**: Small
**Depends on**: T3
**Type**: task

Add a brief "Skill Structure" note near the top of `provider-migration-recipe.md` (after the Overview section) that references the 3-stage model in SKILL.md. This note tells agents reading the recipe that orchestration and verification are governed by SKILL.md, not the recipe itself. Also add a note in the Verify stage of SKILL.md that the recipe status tracker and gotchas sections MUST be updated during verification (FR-011). Do NOT modify recipe Steps 1-8.

**Deliverables:**
- `.claude/skills/migrate-provider/provider-migration-recipe.md` -- "Skill Structure" cross-reference note added after Overview
- `.claude/skills/migrate-provider/SKILL.md` -- Verify stage includes recipe update instruction

**Acceptance criteria:**
- GIVEN the recipe file
  WHEN an agent reads it
  THEN it finds a "Skill Structure" note referencing SKILL.md's 3-stage model and stating that orchestration is governed by SKILL.md.

- GIVEN the recipe file
  WHEN an agent inspects Steps 1-8
  THEN the mechanical steps are unchanged from the current version.
  (Traces to Negative Constraint: no recipe mechanical changes)

- GIVEN the Verify stage in SKILL.md
  WHEN an agent reads the recipe update instruction
  THEN it finds that the Verify stage MUST update the recipe's status tracker entry and gotchas section with new discoveries.
  (Traces to FR-011)

---

## Wave 5: Final Verification

### T5: Review walkthrough and structural verification

**Priority**: P1
**Effort**: Small
**Depends on**: T4
**Type**: chore

Read the completed SKILL.md end-to-end and verify structural completeness against all 24 functional requirements and 18 acceptance criteria from the spec. Verify that no verification plan content appears in Build envelope sections or spawn prompts. Verify that no implementation details appear in Verify envelope sections or spawn prompts. Verify that the file ownership table covers all recipe phases. Verify that the Red Flags table has the minimum required entries. Run a diff against the current SKILL.md to confirm the 3-stage structure replaced the 5-phase structure.

**Deliverables:**
- Verification that SKILL.md satisfies all spec FRs and ACs (no file changes expected unless defects found)

**Acceptance criteria:**
- GIVEN the completed SKILL.md
  WHEN each of the 18 spec acceptance criteria (AC-01 through AC-18) is checked against the document
  THEN every criterion is satisfied.

- GIVEN the completed SKILL.md
  WHEN each of the 11 negative constraints is checked
  THEN no constraint is violated.

- GIVEN the completed SKILL.md and the updated recipe
  WHEN `provider-migration-recipe.md` Steps 1-8 are diffed against the pre-change version
  THEN the mechanical steps are identical (only the Skill Structure note and any status tracker format changes differ).

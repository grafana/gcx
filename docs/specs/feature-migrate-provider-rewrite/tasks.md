---
type: feature-tasks
title: "Rewrite /migrate-provider Skill: 5-Phase Pipeline"
status: approved
spec: docs/specs/feature-migrate-provider-rewrite/spec.md
plan: docs/specs/feature-migrate-provider-rewrite/plan.md
created: 2026-03-28
---

# Implementation Tasks

## Dependency Graph

```
T1 (conventions.md update)──────────────┐
T2 (commands-reference.md update)───────┤
T3 (comparison-report.md update)────────┼──→ T6 (SKILL.md rewrite)──→ T8 (recipe rewrite)──→ T9 (final review + cleanup)
T4 (builder-prompts.md)────────────────┤
T5 (verifier-prompts.md)──────────────┘
                                        T7 (delete old templates)──→ T9
```

## Wave 1: Update Supporting Files

### T1: Update conventions.md with lint compliance checklist and debug logging

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore

Add two sections to `conventions.md`: (1) a lint compliance checklist that Phase 0 fills out per provider, listing CONSTITUTION.md, design-guide.md, provider-guide.md, and provider-discovery-guide.md with section references and applicability fields; (2) debug logging guidance from D-013 covering `slog` usage patterns in provider clients.

**Deliverables:**
- `.claude/skills/migrate-provider/conventions.md`

**Acceptance criteria:**
- GIVEN the updated conventions.md
  WHEN its compliance checklist section is reviewed
  THEN it lists all four compliance documents (CONSTITUTION.md, design-guide.md, provider-guide.md, provider-discovery-guide.md) with fields for section reference and applicability notes
- GIVEN a Phase 0 execution references conventions.md
  WHEN compliance notes are produced
  THEN every applicable rule from the four docs is listed with its section reference (traces to spec AC: "every applicable rule from CONSTITUTION.md, design-guide.md, provider-guide.md, and provider-discovery-guide.md is listed with its section reference")

---

### T2: Update commands-reference.md with HTTP client reference template

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore

Add an "HTTP Client Reference Section" template to `commands-reference.md`. The template MUST include: (a) endpoint table with columns for method, path, purpose, and notes; (b) auth helper signature template; (c) client construction pattern with exact field names. This section is what Phase 2 plan.md production copies and fills in per provider.

**Deliverables:**
- `.claude/skills/migrate-provider/commands-reference.md`

**Acceptance criteria:**
- GIVEN the updated commands-reference.md
  WHEN the HTTP client reference section is reviewed
  THEN it contains an endpoint table template (method, path, purpose, notes columns), an auth helper signature template, and a client construction pattern with exact field names (traces to spec FR-013)

---

### T3: Update comparison-report.md with mandatory smoke test and adapter sections

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore

Update `templates/comparison-report.md` to add mandatory sections for: (a) per-command pass/fail with all four output formats (json, table, wide, yaml) as separate rows, (b) adapter smoke results (`resources schemas` visibility + `resources get` envelope verification), (c) discrepancy summary with verdict and rationale. The existing sections (per-command pass/fail, list ID comparison, get field comparison, output format check, discrepancy summary) are retained and extended.

**Deliverables:**
- `.claude/skills/migrate-provider/templates/comparison-report.md`

**Acceptance criteria:**
- GIVEN the updated comparison report template
  WHEN its sections are reviewed
  THEN it includes mandatory sections for per-command pass/fail with all four output formats, adapter smoke results, and discrepancy summary with verdict and rationale (traces to spec FR-031)
- GIVEN a show/list command entry in the template
  WHEN its format columns are reviewed
  THEN all four formats (json, table, wide, yaml) appear as separate verification rows

---

### T4: Create builder-prompts.md

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore

Create `templates/builder-prompts.md` containing Build-Core and Build-Commands spawn prompt templates. The prompts MUST include the "Do NOT infer response envelope shapes" instruction (FR-018). The prompts MUST NOT include verification task details — no smoke commands, no expected comparison outputs, no pass/fail criteria from the comparison report template (FR-022, NC-003). The prompts reference spec.md (what to build), plan.md (how to build), and assigned implementation tasks — not verification tasks. Remove all sealed envelope language.

**Deliverables:**
- `.claude/skills/migrate-provider/templates/builder-prompts.md`

**Acceptance criteria:**
- GIVEN the builder-prompts.md file
  WHEN Build-Core and Build-Commands prompts are reviewed
  THEN both contain the instruction "Do NOT infer response envelope shapes. Copy deserialization code verbatim from the grafana-cloud-cli source." (traces to spec FR-018)
- GIVEN the builder-prompts.md file
  WHEN searched for verification task details
  THEN no smoke commands, expected comparison outputs, or pass/fail criteria from the comparison report template appear anywhere in the file (traces to spec FR-022, NC-003)
- GIVEN the builder-prompts.md file
  WHEN searched for "sealed envelope", "Build envelope", or "Verify envelope"
  THEN zero matches are found (traces to spec NC-001)

---

### T5: Create verifier-prompts.md

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore

Create `templates/verifier-prompts.md` containing the Verify agent spawn prompt template. The prompt references the comparison report template, mandates smoke tests for all four output formats per show/list command (FR-024), mandates adapter smoke via `resources schemas` and `resources get` (FR-025), and mandates recipe update with status tracker entry and gotchas (FR-026). Remove all sealed envelope language.

**Deliverables:**
- `.claude/skills/migrate-provider/templates/verifier-prompts.md`

**Acceptance criteria:**
- GIVEN the verifier-prompts.md file
  WHEN the Verify prompt is reviewed
  THEN it mandates smoke tests for every show/list command with all four output formats (json, table, wide, yaml) (traces to spec FR-024)
- GIVEN the verifier-prompts.md file
  WHEN the Verify prompt is reviewed
  THEN it mandates adapter smoke via `resources schemas` and `resources get` for every TypedCRUD resource (traces to spec FR-025)
- GIVEN the verifier-prompts.md file
  WHEN the Verify prompt is reviewed
  THEN it mandates recipe update with status tracker entry and gotchas (traces to spec FR-026)
- GIVEN the verifier-prompts.md file
  WHEN searched for "sealed envelope", "Build envelope", or "Verify envelope"
  THEN zero matches are found (traces to spec NC-001)

---

## Wave 2: Core Skill Files

### T6: Rewrite SKILL.md for 5-phase pipeline

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T1, T2, T3, T4, T5
**Type**: task

Rewrite `SKILL.md` from scratch to define the 5-phase pipeline (Phase 0–4). The document MUST include:

- Pipeline overview showing five phases with gates between each phase and no sealed envelope references (FR-028, NC-001)
- Phase 0: Requirements Gathering — autonomous context bundle production (FR-001 through FR-005, NC-006)
- Phase 1: Design Discovery — progressive disclosure with stages 1A–1D, each with user approval gate (FR-006 through FR-011)
- Phase 1: "Small provider shortcut" collapsing 1B–1D for providers with 3 or fewer subcommands
- Phase 2: Spec Planning — spec.md + plan.md + tasks.md production, optional `/plan-spec` usage (FR-012 through FR-016, NC-002, NC-005)
- Phase 3: Build — wave execution with lint checkpoints, agent team orchestration, optional `/build-spec` usage (FR-017 through FR-022, NC-002, NC-003)
- Phase 4: Verification — 4A through 4E in order, mandatory smoke tests, mandatory recipe update (FR-023 through FR-027, NC-004, NC-007)
- Updated red flags table: Phase 0–4 references, no envelope-peeking flags, new "Inferring response envelope shapes" flag (FR-029)
- File ownership table retained for Phase 3 agent team parallelism (FR-030)

**Deliverables:**
- `.claude/skills/migrate-provider/SKILL.md`

**Acceptance criteria:**
- GIVEN the rewritten SKILL.md
  WHEN its pipeline overview is reviewed
  THEN it shows five phases (0–4) with gates between each phase and no references to sealed envelopes (traces to spec AC: "it shows five phases (0–4) with gates between each phase and no references to sealed envelopes")
- GIVEN the rewritten SKILL.md
  WHEN searched for "sealed envelope", "Build envelope", or "Verify envelope"
  THEN zero matches are found (traces to spec NC-001)
- GIVEN the rewritten SKILL.md
  WHEN its red flags table is reviewed
  THEN it references Phase 0–4 (not Stage 1–3), contains no envelope-peeking red flags, and includes a red flag for "Inferring response envelope shapes" (traces to spec FR-029)
- GIVEN the rewritten SKILL.md
  WHEN its file ownership table is reviewed
  THEN it matches the current table structure (Recipe Phase | Files | Teammate) with no ownership changes (traces to spec FR-030)
- GIVEN the rewritten SKILL.md
  WHEN Phase 0 is reviewed
  THEN it is fully autonomous with no user interaction required (traces to spec NC-006)
- GIVEN the rewritten SKILL.md
  WHEN Phase 1 stage transitions are reviewed
  THEN each stage (1A–1D) requires explicit user approval before proceeding (traces to spec FR-006)
- GIVEN the rewritten SKILL.md
  WHEN Phase 2 and Phase 3 references to /plan-spec and /build-spec are reviewed
  THEN both are described as optional accelerators with manual fallback paths (traces to spec NC-002)
- GIVEN the rewritten SKILL.md
  WHEN Phase 4 smoke test requirements are reviewed
  THEN smoke tests are marked MANDATORY and not "optional" or "if live instance available" (traces to spec NC-007)

---

### T7: Delete obsolete template files

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore

Delete `templates/audit-artifacts.md` and `templates/spawn-prompts.md`. These are replaced by spec-format guidance in the recipe (audit-artifacts) and by `templates/builder-prompts.md` + `templates/verifier-prompts.md` (spawn-prompts).

**Deliverables:**
- `templates/audit-artifacts.md` deleted
- `templates/spawn-prompts.md` deleted

**Acceptance criteria:**
- GIVEN the skill directory after deletion
  WHEN `templates/audit-artifacts.md` is checked
  THEN the file does not exist (traces to spec NC-008)
- GIVEN the skill directory after deletion
  WHEN `templates/spawn-prompts.md` is checked
  THEN the file does not exist

---

## Wave 3: Recipe Rewrite

### T8: Rewrite gcx-provider-recipe.md for 5-phase alignment

**Priority**: P0
**Effort**: Medium
**Depends on**: T6
**Type**: task

Rewrite `gcx-provider-recipe.md` to align with the 5-phase pipeline. Changes:

- Update "Skill Structure" section to reference 5-phase pipeline (Phase 0–4) instead of 3-stage pipeline
- Replace references to "Stage 1/2/3" with "Phase 0/1/2/3/4" throughout
- Add spec-format guidance: where the old recipe referenced producing parity tables and architectural mappings, guide users to produce spec.md, plan.md, and tasks.md instead
- Add a section describing what the Phase 2 spec documents must contain (FR-012, FR-013, FR-014)
- Update smoke test section (Step 8) to reference Phase 4 verification steps 4A–4E and the mandatory all-four-formats requirement
- Retain all mechanical steps (types → client → adapter → resource_adapter → provider → commands)
- Retain Gotchas & Lessons Learned section
- Retain Provider Status Tracker
- Retain Tips for Complex Providers
- Integration/wiring step MUST explicitly include import-cycle fixup, variable name collision fixup, and `make lint` (FR-019)

**Deliverables:**
- `.claude/skills/migrate-provider/gcx-provider-recipe.md`

**Acceptance criteria:**
- GIVEN the rewritten recipe
  WHEN searched for "Stage 1", "Stage 2", "Stage 3" as pipeline references
  THEN zero matches are found (all replaced with Phase 0–4 references)
- GIVEN the rewritten recipe
  WHEN searched for "sealed envelope", "Build envelope", or "Verify envelope"
  THEN zero matches are found (traces to spec NC-001)
- GIVEN the rewritten recipe
  WHEN the integration/wiring step is reviewed
  THEN it explicitly includes: wire Commands() and TypedRegistrations(), add blank import, fix import cycles, fix variable name collisions, run `make lint` (traces to spec FR-019)
- GIVEN the rewritten recipe
  WHEN its spec-format guidance is reviewed
  THEN it describes producing spec.md, plan.md, and tasks.md with FR-NNN numbering and Given/When/Then acceptance criteria (traces to spec FR-012)
- GIVEN the rewritten recipe
  WHEN a provider migration is executed using the new pipeline
  THEN `GCX_AGENT_MODE=false make all` remains the build gate command (traces to spec backward compatibility AC)

---

## Wave 4: Final Verification

### T9: Final review — file inventory and cross-reference audit

**Priority**: P0
**Effort**: Small
**Depends on**: T6, T7, T8
**Type**: chore

Verify the complete skill directory matches FR-028's required file list. Cross-check that no stale references remain across all files. Verify all negative constraints hold.

**Deliverables:**
- Verified file inventory matching FR-028
- Zero stale cross-references

**Acceptance criteria:**
- GIVEN the rewritten skill directory
  WHEN its files are listed
  THEN it contains exactly: SKILL.md, gcx-provider-recipe.md, conventions.md, commands-reference.md, templates/comparison-report.md, templates/builder-prompts.md, templates/verifier-prompts.md (traces to spec AC: "it contains exactly: SKILL.md, gcx-provider-recipe.md, conventions.md, commands-reference.md, templates/comparison-report.md, templates/builder-prompts.md, templates/verifier-prompts.md")
- GIVEN all skill files
  WHEN searched for "sealed envelope", "Build envelope", "Verify envelope", "audit-artifacts.md", "spawn-prompts.md"
  THEN zero matches are found across all files (traces to spec NC-001, NC-008)
- GIVEN all skill files
  WHEN searched for "Stage 1", "Stage 2", "Stage 3" as pipeline phase references
  THEN zero matches are found (all replaced with Phase 0–4)
- GIVEN all skill files referencing `/plan-spec` or `/build-spec`
  WHEN the dependency language is reviewed
  THEN both are described as optional with manual fallback — never as required dependencies (traces to spec NC-002)

---
type: feature-spec
title: "Rewrite /migrate-provider Skill: 5-Phase Pipeline"
status: done
beads_id: grafanactl-experiments-sgje
created: 2026-03-27
---

# Rewrite /migrate-provider Skill: 5-Phase Pipeline

## Problem Statement

The current `/migrate-provider` skill uses a 3-stage pipeline (Audit → Build → Verify) with sealed envelopes to enforce isolation between Build and Verify agents. After completing five provider migrations (incidents, k6, fleet, kg, adaptive), several structural problems have emerged:

1. **Sealed envelope isolation is theatrical.** The skill uses spawn prompts to control what each agent sees, but any agent can read files on disk. The isolation provides no real guarantee and adds orchestration complexity (two envelope construction steps, three spawn prompt templates, seven red-flag rules about "peeking").

2. **Custom audit artifacts duplicate the spec format.** The skill produces three bespoke artifacts (parity table, architectural mapping, verification plan) that map cleanly to the project's standard spec documents (spec.md, plan.md, tasks.md). Maintaining two artifact formats creates friction and prevents reuse by `/plan-spec` and `/build-spec`.

3. **No design discovery phase exists.** The current skill jumps from "read gcx source" directly into artifact production. Complex providers (adaptive: 3 signal areas, mixed CRUD/action patterns) require iterative brainstorming with the user before committing to a design. The adaptive migration (D-002, D-004) demonstrated that progressive disclosure catches design misalignment early.

4. **Compliance doc checking is implicit.** The skill assumes builders know project conventions (CONSTITUTION, design-guide, provider-guide, discovery-guide). The adaptive migration (D-001) showed that explicit compliance checking in Phase 0 prevents violations that surface late in Build.

5. **No mandatory smoke tests.** The current Verify stage runs smoke tests only if the verification plan includes them. The adaptive migration (D-012) proved that three classes of bugs (response envelope hallucination, auth method substitution, missing wide codec) are invisible to unit tests with mocked HTTP servers. Smoke tests MUST be mandatory.

6. **No spec document output.** The current skill produces code and a comparison report but no persistent design documentation. The project requires ADRs for non-obvious decisions and spec packages for implementation planning.

The workaround today is manual deviation from the skill (as documented in D-001 through D-013 in the decision log), which defeats the purpose of having a codified skill.

## Scope

### In Scope

- Rewrite of `SKILL.md` to define the 5-phase pipeline (Phase 0–4)
- Rewrite of `gcx-provider-recipe.md` to align with the new phase structure
- Replacement of `templates/audit-artifacts.md` with spec-format guidance
- Replacement of `templates/spawn-prompts.md` with new builder/verifier prompt templates (no sealed envelopes)
- Update of `templates/comparison-report.md` to include mandatory smoke test sections
- Update of `conventions.md` to include the lint compliance checklist (D-010) and debug logging guidance (D-013)
- Update of `commands-reference.md` to include HTTP client reference section template (D-007)
- Retention and adaptation of the red flags table to the new phase structure
- Retention of the file ownership table for agent team parallelism
- Retention of the comparison report template
- Retention of the recipe as the mechanical source of truth
- Integration of all decisions from the decision log (D-001 through D-013)

### Out of Scope

- Implementation of `/plan-spec` or `/build-spec` skills — the rewritten skill MUST work without them (using them when available is optional optimization)
- Changes to the provider code itself (internal/providers/*) — this spec covers only skill files
- Changes to CONSTITUTION.md, design-guide.md, or provider-guide.md — those are upstream references, not skill-owned files
- Automated tooling for smoke test execution — smoke tests remain manual CLI commands run by the agent
- New provider migrations — this spec covers only the skill rewrite, not its first use

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Drop sealed envelopes | Spec format provides natural separation | Envelope enforcement is theatrical (agents read disk); spec ACs are the shared contract; no adversarial incentive exists between builder/verifier agents | D-006 |
| Replace custom audit artifacts with spec documents | spec.md + plan.md + tasks.md | Eliminates duplicate artifact format; enables reuse by /plan-spec and /build-spec; parity table → spec ACs, arch mapping → plan, verification plan → tasks | D-005 |
| Add Phase 0 (Requirements Gathering) | Autonomous context gathering before interactive design | Separates mechanical source reading from design decisions; context bundle is reusable across downstream phases | D-004, D-007 |
| Add Phase 1 (Design Discovery) with progressive disclosure | Interactive brainstorming in stages 1A–1D, each with user approval | Catches design misalignment early; demonstrated superiority over monolithic artifact production in adaptive migration | D-002, D-004 |
| Mandatory compliance doc checking in Phase 0 | First-class checklist item | The adaptive migration found violations that would have been caught by reading CONSTITUTION and design-guide upfront | D-001 |
| Mandatory smoke tests in Phase 4 | Per-command output format verification against live instance | Three bug classes (envelope hallucination, auth substitution, missing codec) are invisible to unit tests | D-012 |
| Integration task includes import-cycle + lint fixup | Explicit scope expansion for wiring task | Parallel wave execution produces import cycles that the integration task must resolve | D-008, D-011 |
| HTTP client reference section in plan.md | Mandatory template section | Endpoint tables, auth signatures, and client construction patterns prevent agent hallucination of response shapes | D-007, D-012 |
| Builder prompt: "Do NOT infer response envelope shapes" | Explicit instruction in builder spawn prompts | Response envelope hallucination was the most impactful bug class in adaptive migration | D-012 |

## Functional Requirements

### Phase 0: Requirements Gathering

**FR-001**: Phase 0 MUST read the grafana-cloud-cli source for the target provider and identify every subcommand, API endpoint, type definition, and auth mechanism.

**FR-002**: Phase 0 MUST read the following project compliance documents and record which rules apply to the target provider: CONSTITUTION.md, docs/reference/design-guide.md, docs/reference/provider-guide.md, docs/reference/provider-discovery-guide.md.

**FR-003**: Phase 0 MUST identify and read the closest existing gcx provider as a pattern reference (e.g., fleet for cloud APIs with separate URLs, SLO for plugin APIs, incidents for gRPC-style POST APIs).

**FR-004**: Phase 0 MUST produce a context bundle containing: (a) source summary with all subcommands mapped, (b) compliance notes listing applicable rules per doc, (c) pattern reference identifying which existing provider to follow.

**FR-005**: Phase 0 MUST be autonomous — no user interaction required. The output is a context bundle, not a design proposal.

### Phase 1: Design Discovery

**FR-006**: Phase 1 MUST use progressive disclosure with four stages: 1A (CLI UX), 1B (Resource adapters), 1C (Auth & config), 1D (Architecture). Each stage MUST receive explicit user approval before the next stage begins.

**FR-007**: Stage 1A MUST propose a command tree with naming and grammar compliance validated against CONSTITUTION.md's CLI Grammar section.

**FR-008**: Stage 1B MUST specify which resources get TypedCRUD adapters, which remain provider-only commands, the GVK mapping for each adapter resource, and the verb choice rationale (list vs show).

**FR-009**: Stage 1C MUST specify ConfigKeys, ConfigLoader usage, environment variable names, and any GCOM/instance lookup requirements.

**FR-010**: Stage 1D MUST specify package layout, client construction pattern, shared helpers, and the auth subpackage structure (if the provider uses multiple subpackages per D-008).

**FR-011**: Phase 1 MUST produce an ADR documenting all design decisions. The ADR MUST be written to `docs/adrs/{provider}/` and approved by the user before proceeding to Phase 2.

### Phase 2: Spec Planning

**FR-012**: Phase 2 MUST produce three documents: spec.md (functional requirements + acceptance criteria), plan.md (architecture + design decisions), and tasks.md (dependency graph + waves + per-task deliverables).

**FR-013**: plan.md MUST include an HTTP client reference section containing: (a) endpoint table with method, path, purpose, and notes per endpoint, (b) auth helper signature, (c) client construction pattern with exact field names.

**FR-014**: tasks.md MUST include smoke test design as explicit verification tasks, not as an afterthought. Each show/list command MUST have a smoke test task entry specifying all four output formats (json, table, wide, yaml).

**FR-015**: Phase 2 MUST use `/plan-spec` when available. When `/plan-spec` is not available, Phase 2 MUST produce the same document format manually.

**FR-016**: The spec package MUST be approved by the user before proceeding to Phase 3.

### Phase 3: Build

**FR-017**: Phase 3 MUST execute tasks.md waves in order, with `make lint` as a checkpoint between each task.

**FR-018**: Builder spawn prompts MUST include the instruction: "Do NOT infer response envelope shapes. Copy deserialization code verbatim from the grafana-cloud-cli source. If the source does `json.Unmarshal(body, &slice)`, the new client MUST do the same — never wrap in a struct unless the source does."

**FR-019**: The integration/wiring task MUST explicitly include: (a) wire Commands() and TypedRegistrations(), (b) add blank import, (c) fix import cycles introduced by subpackage references, (d) fix variable name collisions from package aliasing, (e) run `make lint` and fix all new issues.

**FR-020**: Phase 3 MUST NOT proceed to Phase 4 until `GCX_AGENT_MODE=false make all` exits 0 with no lint errors and all tests passing.

**FR-021**: Phase 3 MUST use `/build-spec` when available. When `/build-spec` is not available, Phase 3 MUST use the skill's own agent team orchestration (file ownership table, Build-Core → Build-Commands sequencing).

**FR-022**: Builder spawn prompts MUST NOT include verification task details (specific smoke commands, expected comparison outputs, or pass/fail criteria from the comparison report template). Builders receive spec.md (what), plan.md (how), and their assigned implementation tasks — not verification tasks.

### Phase 4: Verification

**FR-023**: Phase 4 MUST execute in this order: 4A (make all), 4B (smoke tests), 4C (adapter smoke), 4D (spec compliance), 4E (recipe update).

**FR-024**: Step 4B smoke tests are MANDATORY for every show/list command. Each command MUST be tested with all four output formats: `-o json`, `-o table`, `-o wide`, `-o yaml`.

**FR-025**: Step 4C adapter smoke is MANDATORY for every TypedCRUD resource. Each adapter resource MUST be verified via `resources schemas` (registration visible) and `resources get` (envelope + deserialization working).

**FR-026**: Step 4E recipe update is MANDATORY. The verifier MUST update `gcx-provider-recipe.md` with: (a) a status tracker entry for the ported provider, (b) gotchas discovered during smoke tests (or explicit "No new gotchas" if none), (c) pattern corrections if any recipe step was unclear or incorrect.

**FR-027**: Phase 4 MUST NOT be declared complete until the user has reviewed and approved the comparison report.

### Skill File Structure

**FR-028**: The rewritten skill MUST consist of exactly these files: SKILL.md (main orchestration), gcx-provider-recipe.md (mechanical recipe), conventions.md (Go conventions + lint checklist), commands-reference.md (command patterns + HTTP client template), templates/comparison-report.md (verification report template), templates/builder-prompts.md (builder spawn prompts without envelope isolation), templates/verifier-prompts.md (verifier spawn prompts).

**FR-029**: The red flags table MUST be retained and adapted to reference the new phase numbers (Phase 0–4 instead of Stage 1–3). Red flags about "peeking across envelopes" MUST be removed. A new red flag MUST be added: "Inferring response envelope shapes instead of copying from source."

**FR-030**: The file ownership table MUST be retained for agent team parallelism in Phase 3.

**FR-031**: The comparison report template MUST include mandatory sections for: (a) per-command pass/fail with all four output formats, (b) adapter smoke results, (c) discrepancy summary with verdict and rationale.

## Acceptance Criteria

### Phase 0: Requirements Gathering

- GIVEN a target provider name
  WHEN Phase 0 executes
  THEN a context bundle is produced containing source summary, compliance notes, and pattern reference without any user interaction

- GIVEN the context bundle
  WHEN compliance notes are reviewed
  THEN every applicable rule from CONSTITUTION.md, design-guide.md, provider-guide.md, and provider-discovery-guide.md is listed with its section reference

- GIVEN a target provider with subcommands in grafana-cloud-cli
  WHEN the source summary is produced
  THEN every gcx subcommand for that provider appears in the mapping with proposed gcx equivalent or "Deferred" with rationale

### Phase 1: Design Discovery

- GIVEN Phase 0 context bundle is complete
  WHEN Phase 1 begins stage 1A
  THEN a command tree proposal is presented to the user with grammar compliance validated against CONSTITUTION.md CLI Grammar

- GIVEN user has approved stage 1A
  WHEN Phase 1 proceeds to stage 1B
  THEN a resource adapter proposal is presented specifying adapter vs provider-only classification, GVK mapping, and verb rationale for each resource

- GIVEN user has approved stages 1A through 1D
  WHEN Phase 1 completes
  THEN an ADR exists in `docs/adrs/{provider}/` documenting all design decisions from stages 1A–1D

- GIVEN user has NOT approved a stage
  WHEN the skill attempts to proceed to the next stage
  THEN the skill blocks and re-presents the current stage for feedback

### Phase 2: Spec Planning

- GIVEN an approved ADR from Phase 1
  WHEN Phase 2 completes
  THEN spec.md, plan.md, and tasks.md exist with YAML frontmatter, FR-NNN numbering, and Given/When/Then acceptance criteria

- GIVEN plan.md is produced
  WHEN its contents are reviewed
  THEN it contains an HTTP client reference section with endpoint table (method, path, purpose, notes), auth helper signature, and client construction pattern

- GIVEN tasks.md is produced
  WHEN its verification tasks are reviewed
  THEN every show/list command has a smoke test task entry specifying all four output formats (json, table, wide, yaml)

### Phase 3: Build

- GIVEN tasks.md defines waves
  WHEN Phase 3 executes
  THEN tasks within a wave execute in parallel and waves execute sequentially with `make lint` between each task

- GIVEN a builder agent is spawned
  WHEN its prompt is reviewed
  THEN the prompt contains the "Do NOT infer response envelope shapes" instruction and does NOT contain verification task details (smoke commands, expected comparison outputs)

- GIVEN an integration/wiring task executes
  WHEN it completes
  THEN import cycles are resolved, variable name collisions are fixed, and `make lint` passes with zero provider-related warnings

- GIVEN all tasks in all waves are complete
  WHEN `GCX_AGENT_MODE=false make all` is run
  THEN it exits 0 with no lint errors and all tests passing

### Phase 4: Verification

- GIVEN Phase 3 build gate passes
  WHEN step 4B executes for a show/list command
  THEN the command is run with `-o json`, `-o table`, `-o wide`, and `-o yaml` against a live Grafana instance and each format produces valid output

- GIVEN Phase 3 build gate passes
  WHEN step 4C executes for an adapter resource
  THEN `resources schemas` shows the resource's registration and `resources get {alias}` returns valid K8s-enveloped output

- GIVEN Phase 4 completes
  WHEN `gcx-provider-recipe.md` is reviewed
  THEN it contains a new status tracker row for the ported provider and a gotchas entry (either specific gotchas or "No new gotchas")

- GIVEN Phase 4 produces a comparison report
  WHEN the user has NOT approved the report
  THEN the skill blocks and does NOT declare the migration complete

### Skill File Structure

- GIVEN the rewritten skill directory
  WHEN its files are listed
  THEN it contains exactly: SKILL.md, gcx-provider-recipe.md, conventions.md, commands-reference.md, templates/comparison-report.md, templates/builder-prompts.md, templates/verifier-prompts.md

- GIVEN the rewritten SKILL.md
  WHEN its red flags table is reviewed
  THEN it references Phase 0–4 (not Stage 1–3), contains no envelope-peeking red flags, and includes a red flag for "Inferring response envelope shapes"

- GIVEN the rewritten SKILL.md
  WHEN its pipeline overview is reviewed
  THEN it shows five phases (0–4) with gates between each phase and no references to sealed envelopes

### Backward Compatibility

- GIVEN the rewritten recipe
  WHEN a provider migration is executed using the new pipeline
  THEN `GCX_AGENT_MODE=false make all` remains the build gate command

- GIVEN the rewritten skill
  WHEN the file ownership table is reviewed
  THEN it matches the current table structure (Recipe Phase | Files | Teammate) with no ownership changes

## Negative Constraints

- **NC-001**: The skill MUST NOT reference "sealed envelopes", "Build envelope", or "Verify envelope" anywhere in any file.

- **NC-002**: The skill MUST NOT require `/plan-spec` or `/build-spec` to function. These are optional accelerators, not dependencies.

- **NC-003**: Builder spawn prompts MUST NOT include verification task details (specific smoke commands, expected comparison outputs, or pass/fail criteria from the comparison report template).

- **NC-004**: The skill MUST NOT skip any phase gate. Phase transitions MUST require the specified gate condition (user approval, `make all` exit 0, etc.).

- **NC-005**: The skill MUST NOT produce custom artifact formats (parity table, architectural mapping, verification plan as standalone documents). All structured output MUST use the spec document format (spec.md, plan.md, tasks.md) or the comparison report template.

- **NC-006**: Phase 0 MUST NOT require user interaction. It is fully autonomous.

- **NC-007**: Phase 4 smoke tests MUST NOT be marked "optional" or "if live instance available". Smoke tests are mandatory. If no live instance is available, Phase 4 MUST block and report the blocker to the user.

- **NC-008**: The `templates/audit-artifacts.md` file MUST NOT exist after the rewrite. It is replaced by spec-format guidance in the recipe.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Removing sealed envelopes causes builders to overfit to verification expectations | Builders write code that passes smoke tests but does not handle edge cases | Builder prompts explicitly exclude verification task details (FR-022, NC-003); spec ACs are the shared contract, not smoke commands |
| Progressive disclosure in Phase 1 takes significantly longer than monolithic artifact production for simple providers | Increased wall-clock time for straightforward ports (1-2 subcommands) | SKILL.md includes a "small provider shortcut" that collapses Phase 1 stages 1B–1D into a single proposal when the provider has 3 or fewer subcommands |
| `/plan-spec` and `/build-spec` skills may not exist when the rewritten skill is first used | Phase 2 and Phase 3 must work without them | FR-015 and FR-021 require manual fallback paths that produce identical output |
| Mandatory smoke tests require a live Grafana instance that may not be available | Phase 4 blocks indefinitely | NC-007 requires the skill to report the blocker and block rather than skip; the user decides how to proceed |
| The decision log (D-001 through D-013) may not capture all lessons from future migrations | The rewritten skill becomes stale | FR-026 mandates recipe updates after every migration, ensuring the recipe evolves |
| HTTP client reference section in plan.md adds planning overhead | Phase 2 takes longer | The overhead is justified: response envelope hallucination (D-012) was the most impactful bug class; 5 minutes of planning prevents 30+ minutes of debugging |

## Open Questions

- [RESOLVED] Should the skill support both the old 3-stage and new 5-phase pipeline? — No. The rewrite fully replaces the old pipeline. The old pipeline files are deleted and replaced.

- [RESOLVED] Should Phase 1 ADRs follow a specific template? — The ADR format is project-convention (already exists in `docs/adrs/`). The skill references but does not redefine the ADR template.

- [RESOLVED] Should the comparison report template change? — Yes. It gains mandatory smoke test sections for all four output formats per command and adapter smoke results (FR-031).

- [DEFERRED] Should the skill integrate debug logging guidance (D-013) into builder prompts or leave it as a conventions.md reference? — Deferred to first use of the rewritten skill. Adding it to conventions.md is the minimum; builder prompt integration is an optimization.

- [DEFERRED] Should the skill define a "small provider shortcut" threshold? — The ADR suggests 3 or fewer subcommands. Validate during the first migration using the rewritten skill and adjust if needed.

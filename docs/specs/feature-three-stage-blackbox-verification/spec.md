---
type: feature-spec
title: "Three-Stage Blackbox Verification for /migrate-provider Skill"
status: done
beads_id: grafanactl-experiments-zn8
created: 2026-03-24
---

# Three-Stage Blackbox Verification for /migrate-provider Skill

## Problem Statement

The `/migrate-provider` skill produces incomplete, unreliable provider ports because its structure enables agents to skip verification gates and leak context between implementation and testing.

**Who is affected:** Agents executing provider migrations from gcx to grafanactl, and the human reviewer who must catch defects the skill fails to prevent.

**Current problems observed during incidents, kg, and fleet migrations:**

1. **No human gates** -- agents declared "done" without structured smoke diffs, forcing the reviewer to re-verify from scratch.
2. **Flat checklist** -- the 5-phase structure (Pre-flight, Parity Audit, Core Adapter, Schema, Commands, Smoke Test) has no enforced ordering. Agents cherry-picked steps, skipping the parity audit or deferring smoke tests indefinitely.
3. **No architectural mapping** -- agents copied gcx patterns verbatim (embedded base clients, flat CLI flags, raw JSON output) instead of translating to grafanactl patterns (TypedCRUD[T], Options structs, codec registry with K8s envelope wrapping).
4. **Verification as afterthought** -- smoke tests ran in the same agent context as implementation, producing confirmation bias. The agent tested what it believed it built, not what was actually required.

**Current workaround:** The reviewer manually re-runs smoke test commands, cross-references gcx output, and catches pattern violations during PR review. This defeats the purpose of having a skill.

## Scope

### In Scope

- Rewrite of `.claude/skills/migrate-provider/SKILL.md` to a 3-stage structure (Audit, Build, Verify)
- Definition of sealed envelope contents for each stage (what each stage receives and produces)
- Dual blackbox isolation rules: Build MUST NOT see the verification plan; Verify MUST NOT see implementation decisions
- **Sealed envelope enforcement via agent session isolation** -- each downstream stage runs as a separate agent (subagent or teammate) that receives only its envelope contents in the spawn prompt, achieving real context isolation rather than honor-system instructions
- **Build stage split into Build-Core and Build-Commands teammates** via agent teams, with explicit file ownership boundaries to prevent merge conflicts
- Artifact specifications for each stage: parity table format, architectural mapping format, verification plan format, comparison report format
- Gate definitions: what constitutes passing each gate
- Orchestration table: which agent strategy applies to each stage (main context, subagent, or agent team)
- Update of `gcx-provider-recipe.md` to reference the new skill structure
- Checklist format for each stage

### Out of Scope

- **Changes to `gcx-provider-recipe.md` mechanical steps** -- the recipe's step-by-step port instructions remain unchanged. This spec restructures the skill wrapper, not the recipe content. Rationale: the recipe is evergreen and updated per-migration.
- **Changes to `/add-provider` skill** -- no structural alignment between the two skills. Rationale: migrations and greenfield providers have fundamentally different workflows (ADR 001).
- **Changes to `conventions.md` or `commands-reference.md`** -- these reference documents are unchanged. Rationale: they are already accurate and referenced by the recipe.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Number of stages | 3 (Audit, Build, Verify) | Migrations inherit design decisions from gcx; a 4th Design stage would be a rubber-stamp gate that teaches agents to skip gates | ADR 001 |
| Blackbox isolation model | Dual: Build sealed from verification plan, Verify sealed from implementation | Stage-level TDD -- prevents overfitting (Build) and confirmation bias (Verify) | ADR 001 |
| Audit produces two sealed envelopes | Build envelope (parity table + arch mapping + recipe ref) and Verify envelope (test list + smoke commands + pass criteria) | Each downstream stage receives only what it needs; enables session boundaries and agent handoffs | ADR 001 |
| Verification plan written before implementation | Yes, during Audit alongside parity table and arch mapping | Catches specification gaps early (e.g., missing subcommands in the plan) | ADR 001 |
| Build stage internal structure | Agent team with two teammates: Build-Core (types/client/adapter/resource_adapter) and Build-Commands (provider registration/commands) | Prevents flat-checklist regression; teammates own disjoint file sets; each receives only the Build envelope in its spawn prompt | User feedback on ADR 001 |
| Sealed envelope enforcement mechanism | Agent session isolation -- each stage runs as a separate agent session (subagent or teammate) that receives only its envelope in the spawn prompt | Real context isolation: downstream agents physically cannot access envelope contents they were not given, eliminating honor-system reliance | User feedback on ADR 001 |
| Audit stage orchestration | Runs in main (lead) context, not delegated | Interactive stage requiring user review and approval of artifacts; lead orchestrator needs to inspect outputs before sealing envelopes | User feedback |
| Build stage orchestration | Agent team (TeamCreate + Agent tool with team_name) with Build-Core and Build-Commands teammates | Two teammates can work in parallel on disjoint file sets; shared TaskList enables coordination on interface boundaries | User feedback |
| Verify stage orchestration | Subagent (Agent tool, fire-and-forget) | Single focused task; only the result (comparison report) matters to the lead; no need for teammate coordination | User feedback |
| Skill file organization | SKILL.md rewritten; recipe, conventions, commands-reference unchanged | Separates orchestration (SKILL.md) from mechanical reference (recipe + conventions) | Current skill structure |

## Functional Requirements

- FR-001: The skill MUST define exactly three stages: Audit, Build, and Verify, executed in strict sequential order with a human approval gate between each pair.
- FR-002: The Audit stage MUST produce three artifacts before any provider code is written: (a) parity table, (b) architectural mapping, (c) verification plan.
- FR-003: The parity table MUST contain one row per gcx subcommand with columns: gcx command, grafanactl equivalent, status (Implemented / Deferred / N/A), and notes. Every gcx subcommand MUST appear -- no silent omissions.
- FR-004: The architectural mapping MUST contain explicit translations for each of these gcx-to-grafanactl pattern pairs: (a) gcx flat client to TypedCRUD[T] adapter, (b) gcx CLI flags to Options struct with setup/Validate, (c) gcx output formatting to codec registry with K8s envelope wrapping, (d) gcx types to Go structs with omitzero for struct fields, (e) gcx provider registration to adapter.Register() in init() with blank import.
- FR-005: The verification plan MUST list: (a) automated tests to write (client httptest, adapter round-trip, TypedCRUD interface compliance), (b) smoke test commands to run against a live instance (list/get/create per resource, structured jq diffs, format checks for table/wide/json/yaml), (c) build gate checkpoints specifying when `GRAFANACTL_AGENT_MODE=false make all` MUST run.
- FR-006: The Build stage MUST receive only the Build envelope (parity table, architectural mapping, recipe reference). The Build stage MUST NOT receive or reference the verification plan.
- FR-007: The Verify stage MUST receive only the Verify envelope (verification plan). The Verify stage MUST NOT receive or reference Build-stage implementation decisions (e.g., internal function names, error handling approach, test structure chosen by the builder).
- FR-008: The Build stage MUST follow `gcx-provider-recipe.md` internal phases (types, client, adapter, resource_adapter, provider, commands) with a `make lint` checkpoint between each phase.
- FR-009: The Build stage gate MUST require `GRAFANACTL_AGENT_MODE=false make all` to pass before proceeding to Verify.
- FR-010: The Verify stage MUST execute every item in the verification plan and produce a structured comparison report containing: (a) per-command pass/fail with captured output, (b) diff output for list ID comparisons and get field comparisons, (c) format check results for all four output modes.
- FR-011: The Verify stage MUST update `gcx-provider-recipe.md` with any new discoveries (gotchas, pattern corrections, status tracker entry).
- FR-012: The Verify stage gate MUST require user review of the comparison report. All discrepancies MUST be either justified with a written rationale or fixed before the gate passes.
- FR-013: The SKILL.md MUST include an orchestration table specifying agent strategy per stage: Audit in main context, Build as agent team (Build-Core + Build-Commands teammates), Verify as subagent.
- FR-014: The SKILL.md MUST include a per-stage checklist with checkboxes that agents mark as they complete each item.
- FR-015: The SKILL.md MUST include a "Red Flags -- STOP and Check" table listing common agent mistakes with their corrective actions, covering at minimum: copying gcx client verbatim, skipping parity audit, guessing endpoint names, and skipping smoke tests.
- FR-016: Each sealed envelope MUST be described in SKILL.md as a named section with explicit "receives" and "produces" lists, so agents know exactly what context they have.
- FR-017: The Audit stage MUST run in the lead orchestrator's main context (not delegated to a subagent or teammate), because it requires interactive user review and approval of all three artifacts before envelopes are sealed.
- FR-018: The Build stage MUST use an agent team (TeamCreate) with exactly two teammates: Build-Core and Build-Commands. The lead orchestrator MUST spawn both teammates using the Agent tool with team_name after creating the team.
- FR-019: The Build-Core teammate MUST own and modify only files in the provider's types, client, adapter, and resource_adapter packages (e.g., `internal/providers/{name}/types.go`, `client.go`, `adapter.go`, `resource_adapter.go`). The Build-Commands teammate MUST own and modify only files in the provider registration and CLI command packages (e.g., `internal/providers/{name}/provider.go`, `cmd/grafanactl/providers/{name}/`).
- FR-020: Each Build teammate MUST receive only the Build envelope contents (parity table, architectural mapping, recipe reference) in its spawn prompt. The spawn prompt MUST NOT include any verification plan content.
- FR-021: The Build-Core teammate MUST complete its work (types, client, adapter, resource_adapter) and signal completion via the shared TaskList before Build-Commands begins command implementation, because commands depend on the adapter interfaces.
- FR-022: The Verify stage MUST run as a subagent (Agent tool without team_name). The lead orchestrator MUST pass only the Verify envelope contents (verification plan) in the subagent's spawn prompt. The spawn prompt MUST NOT include any Build envelope or implementation details.
- FR-023: The lead orchestrator MUST manage all gates between stages: inspecting Audit artifacts before sealing envelopes, waiting for Build team completion, and reviewing the Verify subagent's comparison report.
- FR-024: The SKILL.md MUST specify file ownership boundaries between Build-Core and Build-Commands teammates in a table or list, mapping each recipe phase to the teammate that owns it.

## Acceptance Criteria

- AC-01: GIVEN the rewritten SKILL.md
  WHEN an agent reads it
  THEN it finds exactly three stages (Audit, Build, Verify) with explicit gates between each pair.

- AC-02: GIVEN the Audit stage is executing
  WHEN the agent completes the parity table
  THEN every gcx subcommand for the target provider has a row with status and notes -- no subcommand is missing.

- AC-03: GIVEN the Audit stage is executing
  WHEN the agent completes the architectural mapping
  THEN each of the five gcx-to-grafanactl pattern pairs (FR-004 a through e) has an explicit translation entry.

- AC-04: GIVEN the Audit stage is executing
  WHEN the agent produces the verification plan
  THEN the plan lists specific automated test names, specific smoke test commands with concrete arguments (not placeholders), and specific build gate checkpoints.

- AC-05: GIVEN the Build stage is executing
  WHEN an observer inspects the Build agent's available context
  THEN the context contains only the Build envelope (parity table, architectural mapping, recipe reference) and does not contain the verification plan.

- AC-06: GIVEN the Verify stage is executing
  WHEN an observer inspects the Verify agent's available context
  THEN the context contains only the Verify envelope (verification plan) and does not contain Build-stage implementation decisions.

- AC-07: GIVEN the Build stage has completed
  WHEN `GRAFANACTL_AGENT_MODE=false make all` is run
  THEN it exits 0 with no lint errors and all tests passing.

- AC-08: GIVEN the Verify stage is executing
  WHEN the agent runs smoke test commands from the verification plan
  THEN every command's output is captured in the comparison report with pass/fail status.

- AC-09: GIVEN the Verify stage has produced a comparison report
  WHEN the user reviews it
  THEN every discrepancy is either marked "justified" with a written rationale or marked "fix required".

- AC-10: GIVEN the SKILL.md
  WHEN an agent reads the Red Flags table
  THEN it finds at least four entries covering: copying gcx client verbatim, skipping parity audit, guessing endpoint names, skipping smoke tests.

- AC-11: GIVEN the SKILL.md
  WHEN an agent reads the orchestration table
  THEN it finds one row per stage specifying: Audit = main context, Build = agent team (Build-Core + Build-Commands), Verify = subagent.

- AC-12: GIVEN the SKILL.md
  WHEN an agent reads a sealed envelope section
  THEN the section contains explicit "receives" and "produces" lists with no ambiguity about what context the stage has access to.

- AC-13: GIVEN the Build stage is executing
  WHEN the lead orchestrator spawns the Build team
  THEN exactly two teammates are created (Build-Core and Build-Commands), each receiving only the Build envelope in its spawn prompt, and neither receiving any verification plan content.

- AC-14: GIVEN the Build-Core teammate is executing
  WHEN it modifies files
  THEN it modifies only files within its ownership boundary (types, client, adapter, resource_adapter) and does not modify command or provider registration files.

- AC-15: GIVEN the Build-Commands teammate is executing
  WHEN it modifies files
  THEN it modifies only files within its ownership boundary (provider registration, CLI commands) and does not modify types, client, or adapter files.

- AC-16: GIVEN the Verify stage is executing
  WHEN the lead orchestrator spawns the Verify subagent
  THEN the subagent's spawn prompt contains only the Verify envelope contents and no Build envelope or implementation details.

- AC-17: GIVEN the Build stage is executing
  WHEN Build-Core completes its work
  THEN Build-Commands begins only after Build-Core signals completion via the shared TaskList.

- AC-18: GIVEN the SKILL.md
  WHEN an agent reads the file ownership table
  THEN it finds an explicit mapping of each recipe phase to the teammate (Build-Core or Build-Commands) that owns that phase's files.

## Negative Constraints

- NEVER allow the Build stage to reference or contain verification plan content (smoke test commands, expected comparison outputs, pass criteria from the Verify envelope).
- NEVER allow the Verify stage to reference or contain Build-stage implementation details (internal function names chosen, error handling approach, test structure, architectural shortcuts).
- NEVER allow an agent to skip the parity audit. The Build stage MUST NOT begin without an approved parity table.
- NEVER allow an agent to declare a stage complete without its gate passing. Specifically: Audit requires user approval of all three artifacts; Build requires `make all` passing; Verify requires user review of the comparison report.
- DO NOT add a fourth "Design" stage. Migrations inherit design decisions from gcx; the architectural mapping in Audit handles translation.
- DO NOT modify `gcx-provider-recipe.md` mechanical steps (Steps 1-8) as part of this feature. Only the skill structure reference and status tracker are updated.
- DO NOT merge the verification plan into the Build envelope. The separation is the mechanism that prevents overfitting.
- NEVER allow Build-stage unit tests to be written based on knowledge of what smoke tests will be run. Unit tests MUST be derived from requirements (parity table + arch mapping), not from the verification plan.
- NEVER include verification plan content in a Build teammate's spawn prompt. The spawn prompt is the enforcement boundary -- any content included there becomes available to the teammate.
- NEVER allow Build-Core and Build-Commands teammates to modify each other's files. File ownership boundaries MUST be respected to prevent merge conflicts and maintain clear responsibility.
- DO NOT run the Audit stage as a delegated agent. Audit requires interactive user review and approval; delegating it would bypass the human gate.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Agent session isolation is imperfect -- teammates can read files on disk written by other stages | Agent in a teammate session could `cat` an envelope file it was not given in its spawn prompt | Envelope files use stage-prefixed names (e.g., `verify-envelope.md`); SKILL.md Red Flags table explicitly forbids reading files outside the spawn prompt's listed artifacts; lead orchestrator writes Verify envelope to disk only after Build completes |
| Build teammate coordination failure -- Build-Commands starts before Build-Core finishes | Commands reference adapter interfaces that do not yet exist, causing compile errors | FR-021 requires Build-Core to signal completion via shared TaskList before Build-Commands begins; `make lint` checkpoint after Build-Core catches interface gaps |
| Build teammates modify overlapping files | Merge conflicts or silent overwrites between teammates | FR-019 defines explicit file ownership boundaries; SKILL.md includes ownership table; teammates operate on disjoint package directories |
| Parity table completeness depends on agent thoroughness | Missing gcx subcommands lead to incomplete ports | Gate requires user review of parity table; table format mandates "every gcx subcommand gets a row" |
| Structural divergence from /add-provider | Agents cannot pattern-match across skills; maintenance burden of two different structures | Shared conventions (gate format, orchestration tables, checklist format) reduce surface divergence |
| Verification plan quality depends on Audit agent | Poor verification plan leads to shallow Verify stage | Gate requires user approval of verification plan; plan must include specific commands with concrete values, not placeholders |
| Agent team overhead for small providers | Providers with few resources (1-2 subcommands) may not justify the teammate coordination cost | SKILL.md SHOULD note that for trivially small providers, the lead MAY collapse Build-Core and Build-Commands into a single subagent, documented as an exception in the orchestration table |

## Open Questions

- [RESOLVED]: Should Build and Verify run in the same session or different sessions? -- They run in different agent sessions. Build runs as an agent team (Build-Core + Build-Commands teammates), and Verify runs as a separate subagent. Each receives only its envelope in the spawn prompt, achieving real context isolation.
- [RESOLVED]: Should the Audit stage also produce the provider directory structure? -- No. Directory creation is a Build concern following the recipe. Audit produces only the three planning artifacts.
- [RESOLVED]: Should Build be split into Build-Core (adapter) and Build-Commands (CLI)? -- Yes. Build uses an agent team with two teammates: Build-Core (types, client, adapter, resource_adapter) and Build-Commands (provider registration, CLI commands). Build-Core completes first because commands depend on adapter interfaces. Teammates own disjoint file sets to prevent conflicts.
- [RESOLVED]: Can sealed envelope enforcement be automated (e.g., separate agent sessions per stage)? -- Yes. Each downstream stage runs as a separate agent session (teammate or subagent) that receives only its envelope contents in the spawn prompt. The lead orchestrator constructs spawn prompts containing exclusively the designated envelope, achieving real context isolation rather than honor-system instructions. Teammates physically cannot access context they were not given in their spawn prompt (they have their own context window, not the lead's).

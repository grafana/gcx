---
type: feature-plan
title: "Three-Stage Blackbox Verification for /migrate-provider Skill"
status: draft
spec: spec/feature-three-stage-blackbox-verification/spec.md
created: 2026-03-24
---

# Architecture and Design Decisions

## Pipeline Architecture

The rewritten SKILL.md replaces the current flat 5-phase structure with a 3-stage pipeline enforced by sealed envelopes and agent session isolation.

```
                          SKILL.md (orchestration document)
                                    |
                     +--------------+--------------+
                     |                             |
              Unchanged files                 Rewritten file
         (conventions.md,                    (SKILL.md)
          commands-reference.md,
          provider-migration-recipe.md
          mechanical steps)

============================================================================
RUNTIME EXECUTION MODEL (what SKILL.md instructs agents to do)
============================================================================

  ┌─────────────────────────────────────────────────────────────────┐
  │  LEAD ORCHESTRATOR (main context)                               │
  │                                                                 │
  │  Stage 1: AUDIT (runs inline)                                   │
  │  ├── Read cloud CLI source                                     │
  │  ├── Produce: parity table                                      │
  │  ├── Produce: architectural mapping                             │
  │  ├── Produce: verification plan                                 │
  │  ├── USER GATE: approve all 3 artifacts                         │
  │  └── Seal envelopes:                                            │
  │       ├── Build envelope = parity table + arch mapping + recipe │
  │       └── Verify envelope = verification plan                   │
  │                                                                 │
  │  Stage 2: BUILD (agent team)                                    │
  │  ├── TeamCreate("build-{provider}")                             │
  │  ├── Spawn Build-Core teammate ──────────┐                      │
  │  │   (receives: Build envelope only)     │                      │
  │  │   Owns: types.go, client.go,          │                      │
  │  │         adapter.go, resource_adapter,  │                      │
  │  │         client_test.go                 │ signals done         │
  │  │                                        ▼ via TaskList         │
  │  ├── Spawn Build-Commands teammate ──────┘                      │
  │  │   (receives: Build envelope only)                            │
  │  │   Owns: provider.go, commands.go,                            │
  │  │         blank import, command tests                           │
  │  ├── Wait for both teammates                                    │
  │  ├── BUILD GATE: GRAFANACTL_AGENT_MODE=false make all           │
  │  └── TeamDelete                                                 │
  │                                                                 │
  │  Stage 3: VERIFY (subagent)                                     │
  │  ├── Spawn Verify subagent ──────────────┐                      │
  │  │   (receives: Verify envelope only)    │                      │
  │  │   Runs smoke tests from plan          │                      │
  │  │   Produces comparison report          │                      │
  │  │   Updates recipe gotchas/status       │                      │
  │  │                                        │                      │
  │  ├── VERIFY GATE: user reviews report    ◄┘                     │
  │  └── Final: GRAFANACTL_AGENT_MODE=false make all                │
  └─────────────────────────────────────────────────────────────────┘
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| SKILL.md is a single self-contained markdown file with all three stages, envelopes, tables, and checklists inline | Agents read one file; no cross-file navigation needed for orchestration. Matches FR-001, FR-013, FR-014, FR-015, FR-016, FR-024. |
| Artifact format templates are embedded in SKILL.md as fenced code blocks, not separate files | Agents copy-paste templates inline during execution. Separate template files would require reading additional paths and risk desynchronization. Supports FR-003, FR-004, FR-005, FR-010. |
| Sealed envelope sections use a consistent structure: Description, Receives (bulleted), Produces (bulleted), Enforcement (how isolation works) | Uniform format makes it unambiguous for agents. Directly implements FR-016. |
| Build teammate spawn prompt templates are written out verbatim in SKILL.md | The lead orchestrator copies the prompt template and fills in provider-specific values. No room for interpretation about what to include. Implements FR-020. |
| Red Flags table preserved and expanded from current SKILL.md | Current table has 10 entries; new table retains relevant entries and adds entries specific to the 3-stage model (e.g., reading files outside spawn prompt). Implements FR-015. |
| File ownership table maps recipe phases (1-6) to teammates | Each recipe phase produces specific files; mapping phase-to-teammate makes ownership unambiguous. Implements FR-019, FR-024. |
| Recipe update is minimal: add a "Skill Structure" section referencing the 3-stage model, update status tracker format | Mechanical steps (Steps 1-8) remain untouched per spec Out of Scope. FR-011 is satisfied by the Verify stage updating gotchas/status inline. |
| Small-provider escape hatch documented in orchestration table footnote | Risk mitigation for providers with 1-2 subcommands where team overhead is not justified. Referenced in Risks section of spec. |
| Audit stage may delegate research to subagents while keeping the gate inline | The lead must run the user approval gate (only the lead can call AskUserQuestion), but cloud CLI source exploration and artifact drafting can be delegated to Explore/Plan subagents. The lead reviews subagent outputs, assembles final artifacts, and runs the gate. This keeps FR-017 satisfied (Audit in main context) while reducing lead context consumption. |

## Compatibility

**Unchanged:**
- `provider-migration-recipe.md` mechanical steps (Steps 1-8) -- referenced by the Build stage but not modified
- `conventions.md` -- Go conventions document, unchanged
- `commands-reference.md` -- command patterns document, unchanged
- `/add-provider` skill -- separate skill, no changes

**Updated:**
- `SKILL.md` -- fully rewritten (the core deliverable)
- `provider-migration-recipe.md` -- minor additions only: a "Skill Structure" cross-reference note near the top, and the status tracker format remains compatible with existing entries

**Newly available:**
- Sealed envelope model with explicit receives/produces per stage
- Agent team orchestration pattern for Build with file ownership boundaries
- Artifact format templates (parity table, arch mapping, verification plan, comparison report) embedded in SKILL.md

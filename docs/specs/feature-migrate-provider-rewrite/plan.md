---
type: feature-plan
title: "Rewrite /migrate-provider Skill: 5-Phase Pipeline"
status: approved
spec: docs/specs/feature-migrate-provider-rewrite/spec.md
created: 2026-03-28
---

# Architecture and Design Decisions

## Pipeline Architecture

The rewrite replaces a 3-stage sealed-envelope pipeline with a 5-phase gated pipeline. All changes are confined to `.claude/skills/migrate-provider/`.

```
Current (3-stage):                          New (5-phase):

Stage 1: Audit                              Phase 0: Requirements Gathering (autonomous)
  → 3 custom artifacts                        → context bundle (source + compliance + pattern ref)
  → sealed Build envelope                        ↓ [no gate — feeds Phase 1]
  → sealed Verify envelope                  Phase 1: Design Discovery (interactive, 1A–1D)
       ↓ [user approval gate]                 → ADR in docs/adrs/{provider}/
Stage 2: Build                                   ↓ [user approval gate]
  → agent team (Core + Commands)            Phase 2: Spec Planning
  → code files                                → spec.md + plan.md + tasks.md
       ↓ [make all gate]                          ↓ [user approval gate]
Stage 3: Verify                             Phase 3: Build
  → comparison report                         → agent team (Core + Commands)
  → recipe update                             → code files
       ↓ [user approval gate]                     ↓ [make all gate]
                                            Phase 4: Verification (4A–4E)
                                              → make all + smoke tests + adapter smoke
                                              → comparison report + recipe update
                                                  ↓ [user approval gate]
```

### File Map (Before → After)

```
.claude/skills/migrate-provider/
├── SKILL.md                          ← REWRITE (3-stage → 5-phase)
├── gcx-provider-recipe.md            ← REWRITE (align with 5-phase, remove stage refs)
├── conventions.md                    ← UPDATE (add lint checklist, debug logging)
├── commands-reference.md             ← UPDATE (add HTTP client reference template)
├── templates/
│   ├── audit-artifacts.md            ← DELETE (replaced by spec-format guidance in recipe)
│   ├── spawn-prompts.md              ← DELETE (split into two files below)
│   ├── builder-prompts.md            ← NEW (no envelope isolation, includes NC-003 guardrails)
│   ├── verifier-prompts.md           ← NEW (references comparison report template)
│   └── comparison-report.md          ← UPDATE (add mandatory smoke + adapter sections)
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Rewrite SKILL.md from scratch rather than incremental edit | The structural change from 3-stage to 5-phase is pervasive — every section references stage numbers, envelope mechanics, and artifact names. Incremental editing would leave stale references. Traces to FR-028, FR-029. |
| Split `templates/spawn-prompts.md` into `builder-prompts.md` and `verifier-prompts.md` | Separate files match the separate concerns (FR-028). Builder prompts gain the "Do NOT infer response envelope shapes" instruction (FR-018) and explicitly exclude verification details (FR-022, NC-003). Verifier prompts reference the comparison report template and mandatory recipe update (FR-026). |
| Delete `templates/audit-artifacts.md` entirely (NC-008) | Custom artifact templates (parity table, architectural mapping, verification plan) are replaced by spec document format (spec.md, plan.md, tasks.md) per FR-012. The recipe gains guidance on producing spec documents instead. |
| Add HTTP client reference section template to `commands-reference.md` | FR-013 mandates this in plan.md output. Placing the template in commands-reference.md gives builders a reusable structure. Prevents response envelope hallucination (D-007, D-012). |
| Add lint compliance checklist to `conventions.md` | FR-002 requires Phase 0 to check compliance docs. The checklist in conventions.md is the concrete artifact that Phase 0 fills out. Traces to D-001, D-010. |
| Retain file ownership table and comparison report template | FR-030 and FR-031 explicitly require retention. The ownership table enables agent team parallelism in Phase 3; the comparison report gains mandatory smoke test sections. |
| Include "small provider shortcut" in SKILL.md Phase 1 | The spec's Risks section identifies wall-clock overhead for simple providers. Collapsing stages 1B–1D for providers with 3 or fewer subcommands mitigates this without violating progressive disclosure for complex providers. |
| Recipe restructured around 5 phases with cross-references to SKILL.md | The recipe remains the mechanical source of truth (spec In Scope) but its "Skill Structure" section and step numbering must align with the new phases. Recipe steps map to Phase 3 build tasks; verification steps map to Phase 4. |

## Compatibility

**Continues working unchanged:**
- The `gcx-provider-recipe.md` mechanical steps (types → client → adapter → resource_adapter → provider → commands) remain identical in content — only the framing changes from "Stage 2 steps" to "Phase 3 steps"
- The file ownership table structure (Recipe Phase | Files | Teammate) is preserved (FR-030)
- `GCX_AGENT_MODE=false make all` remains the build gate command
- The comparison report template retains its existing sections and gains new mandatory sections
- The Gotchas & Lessons Learned section in the recipe is preserved and continues to grow per FR-026

**Deprecated / Removed:**
- Sealed envelope concept (Build envelope, Verify envelope) — all references removed (NC-001)
- `templates/audit-artifacts.md` — deleted entirely (NC-008)
- `templates/spawn-prompts.md` — replaced by `templates/builder-prompts.md` and `templates/verifier-prompts.md`
- Custom artifact format (parity table, architectural mapping, verification plan as standalone documents) — replaced by spec format (NC-005)
- Red flag rules about "peeking across envelopes" — removed (FR-029)

**Newly available:**
- Phase 0 autonomous context gathering with compliance checklist
- Phase 1 progressive disclosure with four gated stages (1A–1D)
- Phase 2 spec document production (spec.md, plan.md, tasks.md) with HTTP client reference template
- Mandatory smoke tests for all four output formats per show/list command (Phase 4B)
- Mandatory adapter smoke tests via `resources schemas` and `resources get` (Phase 4C)
- Red flag for "Inferring response envelope shapes instead of copying from source"
- Optional integration with `/plan-spec` (Phase 2) and `/build-spec` (Phase 3) when available

---
type: feature-plan
title: "SLO and Synthetic Monitoring Agent Skills"
status: approved
spec: docs/specs/slo-skills/spec.md
created: 2026-03-09
---

# Architecture and Design Decisions

## Pipeline Architecture

```
                          ┌──────────────────────────────────────────────┐
                          │            .claude/skills/                   │
                          │                                              │
                          │  ┌─────────────┐  ┌──────────────────┐      │
                          │  │ slo-manage/  │  │ slo-check-status/│      │
                          │  │  SKILL.md    │  │  SKILL.md        │      │
                          │  │  refs/       │  └──────────────────┘      │
                          │  │   slo-       │                            │
                          │  │   templates  │  ┌──────────────────┐      │
                          │  └─────────────┘  │ slo-investigate/  │      │
                          │                   │  SKILL.md         │      │
                          │  ┌─────────────┐  │  refs/            │      │
                          │  │ slo-optimize/│  │   slo-promql-    │      │
                          │  │  SKILL.md    │  │   patterns       │      │
                          │  └─────────────┘  └──────────────────┘      │
                          │                                              │
                          │  ┌────────────────┐ ┌──────────────────────┐ │
                          │  │synth-check-    │ │synth-investigate-    │ │
                          │  │ status/        │ │ check/               │ │
                          │  │ SKILL.md       │ │ SKILL.md             │ │
                          │  └────────────────┘ │ refs/                │ │
                          │                     │  failure-modes       │ │
                          │  ┌────────────────┐ │  sm-promql-patterns  │ │
                          │  │synth-manage-   │ └──────────────────────┘ │
                          │  │ checks/        │                          │
                          │  │ SKILL.md       │                          │
                          │  │ refs/          │                          │
                          │  │  check-types   │                          │
                          │  └────────────────┘                          │
                          └──────────────┬───────────────────────────────┘
                                         │ Skills invoke
                                         ▼
              ┌──────────────────────────────────────────────────────────┐
              │                     gcx CLI                       │
              │                                                          │
              │  slo definitions    slo reports    synth checks          │
              │  ├── list           ├── status     ├── list              │
              │  ├── get            ├── timeline   ├── get               │
              │  ├── status         └── ...        ├── status            │
              │  ├── timeline                      ├── timeline          │
              │  ├── push           query          ├── push              │
              │  ├── pull           └── -e <expr>  ├── pull              │
              │  └── delete                        └── delete            │
              │                                                          │
              │  alert rules list   datasources list  synth probes list  │
              └──────────────────────────────────────────────────────────┘
                                         │
                                         ▼
              ┌──────────────────────────────────────────────────────────┐
              │              Grafana REST API / Prometheus               │
              └──────────────────────────────────────────────────────────┘
```

### Cross-Skill Routing Map

```
slo-manage ──→ (routes to) ── slo-check-status, slo-investigate
slo-check-status ──→ slo-investigate, slo-optimize
slo-investigate ──→ slo-manage, slo-optimize
slo-optimize ──→ slo-manage

synth-check-status ──→ synth-investigate-check, synth-manage-checks
synth-investigate-check ──→ synth-manage-checks
synth-manage-checks ──→ synth-check-status, synth-investigate-check
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| T1 (flag standardization) is a hard prerequisite for all skill tasks | Skills MUST use `--from`/`--to` and `--window` flags (FR-005, FR-080–087). Without the code change, skill examples would reference nonexistent flags. |
| Each skill is an independent task (T2–T8), parallelizable in Wave 2 | Skills have no inter-dependencies beyond the shared flag convention. Cross-skill routing references skill names as strings, not shared code. |
| Reference files are bundled with their parent skill task | Each reference file (slo-templates.md, slo-promql-patterns.md, failure-modes.md, sm-promql-patterns.md, check-types.md) is tightly coupled to one skill and small enough (<200 lines) to deliver together. |
| SLO definitions timeline adds `--from`/`--to` + `--window`; retains `--start`/`--end` as hidden deprecated aliases | FR-080, FR-082, FR-086. The current `timelineOpts` struct has `Start`/`End` string fields. We add `From`/`To`/`Window` fields, wire them as primary flags, and keep `--start`/`--end` as `MarkHidden` + `MarkDeprecated` aliases. Error message text references `--from`/`--to` instead of `--start`/`--end`. |
| SLO reports timeline mirrors the definitions timeline flag pattern | FR-081, FR-083. Reports timeline delegates time parsing to `definitions.ParseTimelineTime`. Same flag structure applies. |
| SM checks timeline adds `--from`/`--to` alongside existing `--window` | FR-084. Currently only has `--window`. Add `From`/`To` fields; when set, they take precedence. Mutual exclusivity validation per FR-085. |
| Mutual exclusivity error for `--window` vs `--from`/`--to` is a validation step in `RunE` | FR-085. Checked before time parsing. Returns a clear error message. |
| SLO timeline existing tests kept, new tests added in same file | FR-087. Table-driven tests for `--from`/`--to`, `--window`, mutual exclusivity, and deprecated `--start`/`--end` backward compatibility. |
| T9 (integration review) validates all skills meet shared structure ACs | Ensures FR-001–FR-007 and negative constraints are satisfied across all 7 skills. |

## Compatibility

**Unchanged behavior:**
- `gcx slo definitions timeline --start now-7d --end now` continues to work (deprecated aliases)
- `gcx slo reports timeline --start now-7d --end now` continues to work (deprecated aliases)
- `gcx synth checks timeline --window 6h` continues to work
- `gcx query --from / --to` unchanged (already uses `--from`/`--to`)
- All existing tests pass without modification

**New behavior:**
- `--from`/`--to` accepted on all timeline commands
- `--window` accepted on SLO timeline commands (new)
- `--start`/`--end` hidden from `--help` on SLO timeline commands
- Mutual exclusivity error when `--window` and `--from`/`--to` are both provided

**New artifacts:**
- 7 skill directories under `.claude/skills/` with SKILL.md files
- 5 reference files across 4 skill directories

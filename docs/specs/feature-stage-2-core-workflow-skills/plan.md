---
type: feature-plan
title: "Stage 2: Core Workflow Skills"
status: draft
spec: spec/feature-stage-2-core-workflow-skills/spec.md
created: 2026-03-07
---

# Architecture and Design Decisions

## Pipeline Architecture

All deliverables are markdown content files within the existing `claude-plugin/` directory. No Go code, no infrastructure changes. The plugin runtime discovers skills and agents via directory convention.

```
claude-plugin/
├── .claude-plugin/
│   └── plugin.json                         (UNCHANGED)
├── agents/
│   └── grafana-debugger.md                 (REPLACE stub with full 300+ line prompt)
└── skills/
    ├── setup-gcx/                   (UNCHANGED)
    ├── explore-datasources/                (UNCHANGED)
    ├── investigate-alert/                  (UNCHANGED — flag fix is out of scope)
    ├── debug-with-grafana/                 (NEW)
    │   ├── SKILL.md                        ← 7-step diagnostic workflow
    │   └── references/
    │       └── error-recovery.md           ← 5+ failure mode recovery patterns
    └── manage-dashboards/                  (NEW)
        ├── SKILL.md                        ← 5 dashboard lifecycle workflows
        └── references/
            └── resource-operations.md      ← Full command flag reference + selectors

Source files copied into plugin (originals unchanged):
  .claude/skills/gcx/references/query-patterns.md   → debug-with-grafana/references/query-patterns.md
  .claude/skills/gcx/references/resource-model.md   → manage-dashboards/references/resource-model.md
```

### Authoring Pipeline Per Artifact

```
CLI Flag Audit (T1)
       |
       v
+------+------+
|             |
v             v
T2: error-    T4: resource-
recovery.md   operations.md
|             |
v             v
T3: debug-    T5: manage-
with-grafana  dashboards
SKILL.md      SKILL.md
|             |
+------+------+
       |
       v
T6: grafana-debugger agent
       |
       v
T7: Plugin-wide validation
```

Each skill/agent is authored using the corresponding built-in plugin-dev skill, then reviewed with skill-creator:skill-reviewer immediately after writing. Final plugin-wide validation runs once all files exist.

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | CLI flag audit runs first as a dedicated task | NC-007 mandates consistent `--from`/`--to` vs `--start`/`--end` usage. Actual CLI uses `--from`/`--to` (verified in `cmd/gcx/query/command.go`). All subsequent content MUST use verified flags. The audit produces a reference table consumed by all authoring tasks. |
| D2 | Reference files (error-recovery.md, resource-operations.md) are written before their parent SKILL.md files | FR-007/FR-019 require SKILL.md to reference these files. Writing references first ensures links are valid and content is available for cross-referencing during skill authoring. |
| D3 | The two skill tracks (debug-with-grafana, manage-dashboards) run in parallel after T1 | The spec states the two skills are independent (Spec constraint #1). Parallel waves reduce wall-clock time. |
| D4 | grafana-debugger agent is written after debug-with-grafana skill | FR-031 requires the agent to delegate to `debug-with-grafana`. The skill must exist so the agent can reference its structure and step names accurately. Also addresses Spec constraint #2. |
| D5 | `resource-model.md` is copied into `claude-plugin/skills/manage-dashboards/references/resource-model.md` | The plugin must be self-contained. External relative paths like `../../../.claude/skills/gcx/references/resource-model.md` break when the plugin is installed outside the repo (e.g., `~/.claude/plugins/gcx/`). Content duplication is intentional and acceptable. T5 includes copying this file. |
| D6 | `query-patterns.md` is copied into `claude-plugin/skills/debug-with-grafana/references/query-patterns.md` | Same rationale as D5. The source file exists at `.claude/skills/gcx/references/query-patterns.md`. T3 includes copying this file. |
| D7 | All time-range flags use `--from`/`--to` (not `--start`/`--end`) | Verified against `cmd/gcx/query/command.go` lines 40-41. `investigate-alert` SKILL.md uses `--start`/`--end` incorrectly, but fixing that file is out of scope per spec. New files MUST use correct flags per NC-007. |
| D8 | Each skill invokes plugin-dev:skill-development during authoring and skill-creator:skill-reviewer after | FR-037 and FR-040 mandate this. The built-in skills encode frontmatter conventions, progressive disclosure patterns, and triggering effectiveness checks. |
| D9 | The agent invokes plugin-dev:agent-development during authoring | FR-038 mandates this. Ensures frontmatter schema, example triggers, color, and tools list follow conventions. |
| D10 | plugin-dev:plugin-validator runs once after all files are written | FR-039 mandates whole-plugin structural validation. Running it per-file would produce false negatives for cross-file references. |

## Compatibility

| Concern | Status |
|---------|--------|
| Existing skills (setup-gcx, explore-datasources, investigate-alert) | UNCHANGED. No modifications to any existing skill files. |
| plugin.json | UNCHANGED. Plugin runtime auto-discovers skills/agents by directory convention. |
| grafana-debugger agent (existing) | REPLACED. Current 183-line stub is overwritten with 300+ line full prompt. Agent name, file path, and frontmatter schema remain identical. |
| query-patterns.md and resource-model.md | COPIED into the plugin. Source files at `.claude/skills/gcx/references/` are not modified. Plugin copies are independent — future updates to source files require manual re-copy. |

---
type: feature-plan
title: "Stage 1: MVP plugin"
status: draft
spec: docs/specs/feature-v28/spec.md
created: 2026-03-06
---

# Architecture and Design Decisions

## Plugin Layout Architecture

All plugin content is nested inside a `claude-plugin/` subdirectory at the repository root. This isolates plugin files from the rest of the repo, groups them for coherent `git` change tracking, and enables loading with `claude --plugin-dir ./claude-plugin`.

```
gcx (repo root)
|
+-- .claude/                         <-- Existing: contributor-facing skills (unchanged)
|   +-- skills/
|       +-- gcx/              <-- Contains known bugs; NOT a source for plugin
|       +-- discover-datasources/    <-- Source for explore-datasources adaptation
|       +-- grafana-investigate-alert/ <-- Source for investigate-alert adaptation
|       +-- add-provider/            <-- Contributor-facing; excluded from plugin
|
+-- claude-plugin/                   <-- NEW: all plugin content in one directory
|   +-- .claude-plugin/
|   |   +-- plugin.json              <-- Plugin manifest
|   |
|   +-- agents/
|   |   +-- grafana-debugger.md      <-- Stub (full workflow in Stage 2)
|   |
|   +-- skills/
|       +-- setup-gcx/
|       |   +-- SKILL.md             <-- Written from scratch
|       |   +-- references/
|       |       +-- configuration.md <-- Written from scratch (source: agent-docs/config-system.md)
|       |
|       +-- explore-datasources/
|       |   +-- SKILL.md             <-- Adapted from .claude/skills/discover-datasources/
|       |   +-- references/
|       |       +-- discovery-patterns.md  <-- Copied + verified (bug-free)
|       |       +-- logql-syntax.md        <-- Copied + verified (bug-free)
|       |
|       +-- investigate-alert/
|           +-- SKILL.md             <-- Adapted from .claude/skills/grafana-investigate-alert/
|
+-- agent-docs/                      <-- Existing: authoritative source of truth
    +-- config-system.md             <-- Primary source for configuration.md rewrite
```

### Content Flow

```
                         AUTHORITATIVE SOURCES
                         =====================

  agent-docs/config-system.md --------> claude-plugin/skills/setup-gcx/SKILL.md
         |                                      |
         +------------------------------------> claude-plugin/skills/setup-gcx/references/configuration.md
                                                (written from scratch)

  .claude/skills/discover-datasources/
         |
         +-- SKILL.md ----(adapt)-----------> claude-plugin/skills/explore-datasources/SKILL.md
         |                                    (add cross-ref to setup-gcx,
         |                                     remove any graph pipe refs)
         |
         +-- references/discovery-patterns.md -(copy + fix)-> ...references/discovery-patterns.md
         |                                                     (fix Bug 2: graph pipe, Bug 3: jq path)
         |
         +-- references/logql-syntax.md -------(copy)-------> ...references/logql-syntax.md
                                                               (no bugs found; verbatim copy)

  .claude/skills/grafana-investigate-alert/
         |
         +-- SKILL.md ----(adapt)-----------> claude-plugin/skills/investigate-alert/SKILL.md
                                              (add cross-ref to setup-gcx,
                                               update frontmatter per plugin-dev:skill-development,
                                               preserve 4-step workflow; no Bug 1-4 patterns found)

  Research report Section 2 ----------> claude-plugin/agents/grafana-debugger.md
                                        (stub from research agent template)

  Research report Section 2 ----------> claude-plugin/.claude-plugin/plugin.json
                                        (manifest from research template)
```

### Bug Impact Map

Each bug has a specific blast radius that determines which files need attention:

```
Bug 1 (config paths):    .claude/skills/gcx/ (throughout)
                         .claude/skills/gcx/references/configuration.md (throughout)
                         --> Mitigation: rewrite from scratch, never copy from these files

Bug 2 (graph pipe):      .claude/skills/gcx/SKILL.md (lines 72, 117, 152)
                         .claude/skills/discover-datasources/references/discovery-patterns.md (lines 207-214)
                         --> Mitigation: fix in discovery-patterns.md before copying

Bug 3 (jq envelope):    .claude/skills/discover-datasources/references/discovery-patterns.md (lines 84, 87)
                         --> Mitigation: fix .datasources[] wrapper in discovery-patterns.md

Bug 4 (--all-versions): .claude/skills/gcx/references/selectors.md
                         --> Mitigation: not copying selectors.md; verify no leakage into other files
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Plugin nested in `claude-plugin/` subdirectory | All plugin files live under `claude-plugin/` at the repo root. `claude --plugin-dir ./claude-plugin` loads the plugin. This isolates plugin content from the rest of the repo, avoids `agents/` and `skills/` directories at the repo root, and enables coherent `git` tracking (all plugin changes appear under `claude-plugin/`). See reference: https://github.com/steveyegge/beads/tree/main/claude-plugin |
| Write configuration.md from config-system.md struct hierarchy | The existing `configuration.md` has Bug 1 embedded in every example (fictional `auth.type`/`auth.token`/`namespace` schema). The correct struct path is `contexts.<name>.grafana.{server,token,user,password,org-id,stack-id}`. Patching would risk missing instances; a clean rewrite from the authoritative source is safer. |
| Copy discovery-patterns.md with targeted fixes rather than rewrite | The file is 90% correct. Only the "Visualizing Query Results" section (Bug 2: `gcx graph` pipe) and "Saving Datasource UIDs" section (Bug 3: `.[]` vs `.datasources[]`) need fixes. A targeted edit preserves tested content. |
| Copy logql-syntax.md as-is | Verified: contains zero instances of Bug 1-4 patterns. No config paths, no graph pipe, no jq envelope paths, no --all-versions. Safe to copy verbatim. |
| SKILL.md `description` field drives auto-triggering | Claude Code matches user intent against skill descriptions. The descriptions must be keyword-rich but non-overlapping. `setup-gcx` owns "setup/config/auth/connection"; `explore-datasources` owns "datasource/metrics/labels/log streams/UIDs". |
| grafana-debugger as minimal stub, can reference investigate-alert | The agent references a diagnostic workflow that depends on `debug-with-grafana` skill (Stage 2). Shipping a full agent workflow without its supporting skill would produce hallucinated tool invocations. The stub establishes the agent's identity and approach without committing to specific skill references. However, the agent CAN reference the `investigate-alert` skill by name since it ships in the same plugin. |
| investigate-alert adaptation: clean source, minimal changes | The source skill at `.claude/skills/grafana-investigate-alert/SKILL.md` is ~135 lines, uses correct gcx commands, and contains zero Bug 1-4 patterns. Adaptation is limited to updating frontmatter per plugin-dev:skill-development conventions (name, description), adding a cross-reference to setup-gcx, and preserving the 4-step workflow verbatim. No reference files needed -- the skill is self-contained. |
| Omit `allowed-tools` from SKILL.md frontmatter | The existing discover-datasources skill uses `allowed-tools: gcx`, but gcx is invoked via the Bash tool, not as a named tool. This field's semantics for CLI-wrapping plugins need verification. Omitting avoids breakage; can be added post-verification. |
| Use individual plugin-dev skills as implementation guidance, NOT the /plugin-dev:create-plugin workflow | The `/plugin-dev:create-plugin` workflow is a generic 8-phase orchestrator for building plugins from scratch. Our plan IS the orchestrator -- with domain-specific content (gcx), bug-fix requirements, and adaptation-from-existing-content steps that the generic workflow cannot handle. Instead, we reference individual plugin-dev skills (plugin-structure, skill-development, agent-development) for their specific guidance at each task, and use plugin-dev agents (plugin-validator, skill-reviewer) as quality gates. |
| Follow plugin-dev:skill-development 6-step process for skill authoring | The 6-step process (understand, plan, create structure, edit with progressive disclosure, validate, iterate) provides a repeatable methodology. Key constraints from the skill: SKILL.md body target 1,500-2,000 words, third-person descriptions with specific trigger phrases, imperative/infinitive body text, and progressive disclosure (SKILL.md for workflow guidance, references/ for detailed data). |
| Agent description must include `<example>` blocks per plugin-dev:agent-development | The `description` field is the most critical field for agent triggering. It must include 2-4 `<example>` blocks showing specific user utterances that should invoke the agent. This replaces the plain-text description in the original plan. |

## Plugin-Dev Skills Usage Map

This maps which plugin-dev skill is consumed by which implementation task:

```
plugin-dev:plugin-structure -----> T1 (scaffold)
  Provides: manifest schema, auto-discovery rules, ${CLAUDE_PLUGIN_ROOT},
            kebab-case naming, directory conventions

plugin-dev:skill-development ----> T2 (setup-gcx), T4 (explore-datasources), T4b (investigate-alert)
  Provides: 6-step process, YAML frontmatter rules, description writing
            (third-person, trigger phrases), body writing (imperative form,
            1,500-2,000 words), progressive disclosure pattern, validation
            checklist (8 items)

plugin-dev:agent-development ----> T5 (grafana-debugger)
  Provides: frontmatter schema (name, description with <example> blocks,
            model, color, tools array), system prompt rules (second person,
            under 10K chars, responsibilities + process + output format)

plugin-dev:skill-reviewer -------> T6 (quality gates)
  Provides: 8-step review process covering structure, description evaluation,
            content quality, progressive disclosure, supporting files

plugin-dev:plugin-validator -----> T6 (quality gates)
  Provides: 10-step validation covering manifest, directory structure,
            agents, skills, file organization, security
```

## Compatibility

### What Continues Working
- `.claude/skills/` contributor-facing skills remain untouched. They continue functioning for contributors working on gcx itself.
- `agent-docs/` reference documentation is read-only; no modifications needed.
- All existing CLI behavior, tests, and builds are unaffected (this is a content-only change).

### What Is New
- `.claude-plugin/plugin.json` -- Plugin manifest enabling `claude --plugin-dir` loading.
- `skills/setup-gcx/` -- New skill teaching agents to configure gcx correctly.
- `skills/explore-datasources/` -- Adapted skill for datasource discovery (corrected version of the contributor-facing skill).
- `skills/investigate-alert/` -- Adapted skill for alert investigation (4-step workflow: verify context, get alert details, full investigation, surface resources).
- `agents/grafana-debugger.md` -- Stub agent for future diagnostic workflows (can reference investigate-alert).
- `skills/setup-gcx/references/configuration.md` -- Authoritative config reference for agents.

### What Is Explicitly Excluded
- No MCP server, hooks, or slash commands (by design; see spec negative constraints).
- No modifications to `.claude/skills/` (bug fixes go into plugin copies only).
- No changes to Go source code, Makefile, or CI/CD.
- No use of `/plugin-dev:create-plugin` as orchestrator (our plan is the orchestrator; see design decisions).

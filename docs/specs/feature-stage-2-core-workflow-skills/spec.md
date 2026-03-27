---
type: feature-spec
title: "Stage 2: Core Workflow Skills"
status: done
beads_id: gcx-experiments-cfn
created: 2026-03-07
---

# Stage 2: Core Workflow Skills

## Problem Statement

The gcx Claude plugin (Stage 1) ships three skills (`setup-gcx`,
`explore-datasources`, `investigate-alert`) and one stub agent
(`grafana-debugger`). Two high-value workflow categories remain uncovered:

1. **Diagnostic debugging** — users who report errors, latency spikes, or
   service degradation need a structured multi-step workflow that queries
   metrics, correlates logs, checks SLOs, and surfaces findings. The
   `grafana-debugger` agent exists as a ~180-line stub with no worked
   examples, no error-recovery guidance, and no reference to the
   `query-patterns.md` or `resource-model.md` docs that already exist.

2. **Dashboard lifecycle management** — users who need to pull, push, create,
   validate, or promote dashboards across environments have no skill guiding
   them through gcx's resource commands, topological sort behavior,
   manager metadata, or promotion patterns. The CLI supports all necessary
   operations (`get`, `push`, `pull`, `delete`, `edit`, `validate`, `serve`)
   but there is no skill that teaches agents to use them correctly.

Without these skills, agents either refuse to help ("I don't know how to do
that with gcx") or improvise incorrect commands (wrong flag order,
missing `-d` for datasource UID, pushing dashboards before folders).

**Affected users**: AI coding agents (Claude Code, Cursor, Copilot) using the
gcx plugin to assist developers and SREs with Grafana workflows.

## Scope

### In Scope

- **`debug-with-grafana` skill**: 7-step diagnostic workflow SKILL.md with
  references to `query-patterns.md` (copied into the plugin) and a new
  `error-recovery.md`, plus 3 worked example scenarios
- **`grafana-debugger` agent**: Full specialist system prompt replacing the
  current stub, with worked examples, error-recovery patterns, and delegation
  to the `debug-with-grafana` and `investigate-alert` skills
- **`manage-dashboards` skill**: Pull/push/create/validate/promote workflows
  SKILL.md with references to `resource-model.md` (copied into the plugin) and
  a new `resource-operations.md`
- **Plugin-local copies of shared reference files**: `query-patterns.md` and
  `resource-model.md` are copied from `.claude/skills/gcx/references/`
  into their respective skill `references/` directories so the plugin is
  self-contained when installed outside the repo
- **Auto-trigger descriptions**: Each skill and agent MUST have a `description`
  field in its YAML frontmatter that causes correct auto-trigger behavior
- **Example verification**: All CLI command examples MUST reflect actual
  gcx CLI syntax and flag names as documented in the codebase

### Out of Scope

- **New CLI commands or flags** — this spec produces skill/agent content files
  only; no Go code changes
- **SLO provider skills** — SLO-based debugging workflows will be added in a
  future stage when the SLO provider is more mature
- **Synthetic Monitoring skills** — covered by a separate spec
- **Plugin infrastructure changes** — `plugin.json`, directory layout, and
  plugin loading remain unchanged
- **Automated end-to-end testing harness** — examples are verified manually
  against CLI `--help` and existing reference docs, not via a test framework

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| debug-with-grafana uses 7 steps (not 5 like investigate-alert) | 7-step workflow: discover datasources, confirm data availability, query error rates, query latency, correlate logs, check related dashboards, summarize | Debugging requires broader signal coverage than alert investigation; latency and dashboard correlation are distinct diagnostic phases that warrant separate steps | Bead acceptance criteria |
| error-recovery.md is a new reference file under debug-with-grafana | New file `references/error-recovery.md` | Query failures, auth errors, empty results, and timeout handling need a dedicated reference rather than inline noise in SKILL.md | Bead acceptance criteria |
| resource-operations.md is a new reference file under manage-dashboards | New file `references/resource-operations.md` | Push/pull/validate/promote command patterns and flag combinations are too detailed for inline SKILL.md content | Bead acceptance criteria |
| Shared reference files are copied into the plugin, not referenced externally | Copy `query-patterns.md` to `claude-plugin/skills/debug-with-grafana/references/query-patterns.md` and `resource-model.md` to `claude-plugin/skills/manage-dashboards/references/resource-model.md` | The plugin must be self-contained. External relative paths (e.g. `../../../.claude/skills/...`) break when the plugin is installed outside the repo (e.g. `~/.claude/plugins/gcx/`). Content duplication is acceptable; broken references are not. | Architecture review |
| grafana-debugger agent delegates to skills rather than inlining all logic | Agent prompt references `debug-with-grafana` and `investigate-alert` skills by name | Keeps the agent prompt focused on diagnostic reasoning; skill files carry the step-by-step CLI commands | Plugin architecture pattern |
| All examples use `gcx query -d <uid> -e '<expr>'` syntax | Match actual CLI flag names: `-d` for datasource, `-e` for expression, `--from`/`--to` for time range | Verified against `cmd/gcx/query/command.go` lines 40–41 | Codebase context |
| Use built-in plugin-dev skills for creation and validation | Invoke `plugin-dev:skill-development` and `plugin-dev:agent-development` during authoring; run `plugin-dev:plugin-validator` and `skill-creator:skill-reviewer` for post-implementation validation | Built-in skills encode best practices for frontmatter, progressive disclosure, description triggering, and plugin structure that manual review would miss | Available Claude Code skills |

## Functional Requirements

### debug-with-grafana Skill

- **FR-001**: The skill MUST be located at `claude-plugin/skills/debug-with-grafana/SKILL.md` following the canonical skill directory structure.
- **FR-002**: The SKILL.md MUST contain YAML frontmatter with `name: debug-with-grafana` and a `description` field that specifies when the skill auto-triggers.
- **FR-003**: The SKILL.md MUST define a 7-step diagnostic workflow in this order: (1) discover datasources, (2) confirm data availability, (3) query error rates, (4) query latency, (5) correlate logs, (6) check related dashboards/resources, (7) summarize findings.
- **FR-004**: Each step MUST include at least one concrete `gcx` command example with correct flag syntax.
- **FR-005**: The skill MUST include 3 worked example scenarios: (a) HTTP 500 error spike, (b) latency degradation, (c) service down / no data.
- **FR-006**: Each example scenario MUST show the sequence of commands the agent would run and the expected shape of the output (field names, not fabricated data).
- **FR-007**: The skill MUST reference `references/error-recovery.md` for handling query failures, empty results, auth errors, and timeouts.
- **FR-008**: The skill MUST reference `references/query-patterns.md` — a copy of the file that MUST reside at `claude-plugin/skills/debug-with-grafana/references/query-patterns.md` (copied from `.claude/skills/gcx/references/query-patterns.md`). External paths outside `claude-plugin/` MUST NOT be used.
- **FR-009**: The skill MUST include a prerequisites section that directs to `setup-gcx` if gcx is not configured.

### error-recovery.md Reference

- **FR-010**: The file MUST be located at `claude-plugin/skills/debug-with-grafana/references/error-recovery.md`.
- **FR-011**: The file MUST document recovery patterns for at least these failure modes: (a) authentication/authorization errors (401/403), (b) datasource not found, (c) query returns empty result set, (d) query timeout or server error (5xx), (e) malformed PromQL/LogQL syntax errors.
- **FR-012**: Each failure mode MUST include: the error message pattern to match, the likely cause, and the corrective action (specific gcx commands).

### manage-dashboards Skill

- **FR-013**: The skill MUST be located at `claude-plugin/skills/manage-dashboards/SKILL.md` following the canonical skill directory structure.
- **FR-014**: The SKILL.md MUST contain YAML frontmatter with `name: manage-dashboards` and a `description` field that specifies when the skill auto-triggers.
- **FR-015**: The SKILL.md MUST document these workflows: (a) pull dashboards from Grafana to local files, (b) push dashboards from local files to Grafana, (c) create a new dashboard from scratch, (d) validate dashboard files, (e) promote dashboards across environments (dev to staging to prod).
- **FR-016**: The push workflow MUST document the topological sort requirement: folders MUST be pushed before dashboards, and gcx handles this automatically when both are included in the same push.
- **FR-017**: The push workflow MUST document manager metadata behavior: `grafana.app/managed-by: gcx` annotation is set automatically, and resources managed by other tools are protected by default (require `--include-managed` to override).
- **FR-018**: The promote workflow MUST document the multi-context pattern: pull from source context, push to target context using `--context` flag or `use-context` switch.
- **FR-019**: The skill MUST reference `references/resource-operations.md` for detailed command flag reference.
- **FR-020**: The skill MUST reference `references/resource-model.md` — a copy of the file that MUST reside at `claude-plugin/skills/manage-dashboards/references/resource-model.md` (copied from `.claude/skills/gcx/references/resource-model.md`). External paths outside `claude-plugin/` MUST NOT be used.
- **FR-021**: The skill MUST include a prerequisites section that directs to `setup-gcx` if gcx is not configured.

### resource-operations.md Reference

- **FR-022**: The file MUST be located at `claude-plugin/skills/manage-dashboards/references/resource-operations.md`.
- **FR-023**: The file MUST document the full flag set and usage patterns for each resource command: `get`, `push`, `pull`, `delete`, `edit`, `validate`, `serve`.
- **FR-024**: The file MUST document selector syntax: kind selectors (`dashboards`), UID selectors (`dashboards/my-uid`), glob patterns (`dashboards/my-*`), and multi-selector (`dashboards folders`).
- **FR-025**: The file MUST document the `serve` command workflow: live dev server with watch, hot reload, and browser preview.

### grafana-debugger Agent

- **FR-026**: The agent file MUST remain at `claude-plugin/agents/grafana-debugger.md`.
- **FR-027**: The agent MUST contain YAML frontmatter with `name: grafana-debugger`, a `description` field with at least 4 `<example>` trigger phrases, `color`, and `tools` list.
- **FR-028**: The agent system prompt MUST be a full specialist prompt (minimum 300 lines) replacing the current ~180-line stub.
- **FR-029**: The agent MUST define a diagnostic methodology section that explains how to approach different categories of issues (error spikes, latency, resource exhaustion, service down).
- **FR-030**: The agent MUST include at least 2 worked examples showing complete diagnostic flows with commands and reasoning.
- **FR-031**: The agent MUST explicitly delegate to `debug-with-grafana` for step-by-step diagnostic workflows and to `investigate-alert` for alert-specific investigations.
- **FR-032**: The agent MUST include an error-recovery section that references `error-recovery.md` patterns for handling CLI failures during diagnosis.
- **FR-033**: The agent MUST include output formatting rules: always use `-o json` for data retrieval, use `-o graph` for user-facing visualizations, always use datasource UIDs not display names.

### Auto-Trigger

- **FR-034**: The `debug-with-grafana` skill description MUST trigger for user requests about debugging application issues, diagnosing errors, investigating latency, or troubleshooting services using Grafana/Prometheus/Loki data.
- **FR-035**: The `manage-dashboards` skill description MUST trigger for user requests about pulling, pushing, creating, editing, validating, promoting, or managing Grafana dashboards and folders.
- **FR-036**: The `grafana-debugger` agent description MUST trigger for user requests that describe specific symptoms (500 errors, latency spikes, service down) and want diagnostic investigation using Grafana observability data.

### Implementation & Validation Tooling

- **FR-037**: Implementation of each skill MUST invoke the `plugin-dev:skill-development` skill to follow best practices for skill structure, progressive disclosure, and frontmatter conventions.
- **FR-038**: Implementation of the `grafana-debugger` agent MUST invoke the `plugin-dev:agent-development` skill to follow best practices for agent system prompts, triggering conditions, and tool declarations.
- **FR-039**: After all files are written, `plugin-dev:plugin-validator` MUST be run to validate the overall plugin structure (plugin.json, file paths, frontmatter schema).
- **FR-040**: After each skill is written, `skill-creator:skill-reviewer` MUST be run to validate skill quality (description triggering effectiveness, progressive disclosure, content depth).

## Acceptance Criteria

### debug-with-grafana Skill

- **AC-001**: GIVEN `claude-plugin/skills/debug-with-grafana/SKILL.md` exists
  WHEN the file is parsed
  THEN the YAML frontmatter contains `name: debug-with-grafana` and a non-empty `description` field.

- **AC-002**: GIVEN the SKILL.md content
  WHEN the workflow steps are counted
  THEN exactly 7 numbered steps are present in the specified order: discover datasources, confirm data availability, query error rates, query latency, correlate logs, check related dashboards/resources, summarize findings.

- **AC-003**: GIVEN each workflow step in SKILL.md
  WHEN the step content is inspected
  THEN at least one `gcx` command is present with correct flag syntax matching the CLI's actual flags (`-d`, `-e`, `--from`, `--to`, `--step`, `-o`).

- **AC-004**: GIVEN the SKILL.md content
  WHEN example scenarios are counted
  THEN exactly 3 scenarios are present: HTTP 500 error spike, latency degradation, and service down / no data.

- **AC-005**: GIVEN each example scenario
  WHEN the scenario content is inspected
  THEN it contains a sequence of gcx commands and a description of expected output shape.

- **AC-006**: GIVEN the SKILL.md content
  WHEN references are inspected
  THEN it contains a reference link to `references/error-recovery.md` and a reference to `query-patterns.md`.

### error-recovery.md

- **AC-007**: GIVEN `claude-plugin/skills/debug-with-grafana/references/error-recovery.md` exists
  WHEN the file is parsed
  THEN it documents recovery patterns for at least 5 failure modes: 401/403 auth errors, datasource not found, empty result set, query timeout/5xx, and malformed query syntax.

- **AC-008**: GIVEN each failure mode in error-recovery.md
  WHEN the entry is inspected
  THEN it contains: an error message pattern, a likely cause description, and a corrective action with specific gcx commands.

### manage-dashboards Skill

- **AC-009**: GIVEN `claude-plugin/skills/manage-dashboards/SKILL.md` exists
  WHEN the file is parsed
  THEN the YAML frontmatter contains `name: manage-dashboards` and a non-empty `description` field.

- **AC-010**: GIVEN the SKILL.md content
  WHEN workflows are enumerated
  THEN all 5 workflows are present: pull, push, create, validate, promote.

- **AC-011**: GIVEN the push workflow section
  WHEN the content is inspected
  THEN it documents that gcx automatically sorts folders before dashboards during push and that manager metadata (`grafana.app/managed-by: gcx`) is set automatically.

- **AC-012**: GIVEN the promote workflow section
  WHEN the content is inspected
  THEN it documents the multi-context pattern using `--context` flag or `use-context` to pull from source and push to target.

- **AC-013**: GIVEN the SKILL.md content
  WHEN references are inspected
  THEN it contains reference links to `references/resource-operations.md` and to `resource-model.md`.

### resource-operations.md

- **AC-014**: GIVEN `claude-plugin/skills/manage-dashboards/references/resource-operations.md` exists
  WHEN the file is parsed
  THEN it documents usage patterns for all 7 resource commands: `get`, `push`, `pull`, `delete`, `edit`, `validate`, `serve`.

- **AC-015**: GIVEN the resource-operations.md content
  WHEN the selector syntax section is inspected
  THEN it documents kind selectors, UID selectors, glob patterns, and multi-selector usage with examples.

### grafana-debugger Agent

- **AC-016**: GIVEN `claude-plugin/agents/grafana-debugger.md` exists
  WHEN the file is parsed
  THEN the YAML frontmatter contains `name: grafana-debugger`, a `description` with at least 4 `<example>` trigger phrases, a `color` field, and a `tools` list.

- **AC-017**: GIVEN the agent system prompt
  WHEN the line count is measured
  THEN the prompt is at least 300 lines (excluding frontmatter).

- **AC-018**: GIVEN the agent system prompt
  WHEN the content is inspected
  THEN it contains a diagnostic methodology section covering at least: error spikes, latency degradation, resource exhaustion, and service-down scenarios.

- **AC-019**: GIVEN the agent system prompt
  WHEN worked examples are counted
  THEN at least 2 complete diagnostic flow examples are present with commands and reasoning.

- **AC-020**: GIVEN the agent system prompt
  WHEN delegation references are inspected
  THEN it explicitly names `debug-with-grafana` skill and `investigate-alert` skill as delegation targets.

- **AC-021**: GIVEN the agent system prompt
  WHEN output rules are inspected
  THEN it mandates `-o json` for data retrieval, `-o graph` for user-facing visualizations, and datasource UIDs (not display names) for all queries.

### Auto-Trigger

- **AC-022**: GIVEN a user message "My API is returning 500 errors, help me debug using Grafana"
  WHEN auto-trigger matching is evaluated
  THEN both the `grafana-debugger` agent and the `debug-with-grafana` skill are candidate matches based on their description fields.

- **AC-023**: GIVEN a user message "Pull all dashboards from production and push them to staging"
  WHEN auto-trigger matching is evaluated
  THEN the `manage-dashboards` skill is a candidate match based on its description field.

### Cross-Cutting

- **AC-024**: GIVEN all gcx command examples across all new files
  WHEN each command is checked against CLI `--help` output
  THEN every flag name, subcommand path, and argument position matches the actual CLI interface.

- **AC-025**: GIVEN all new files
  WHEN the file paths are checked
  THEN all files reside under `claude-plugin/` following the canonical skill (`skills/<name>/SKILL.md`, `skills/<name>/references/*.md`) and agent (`agents/<name>.md`) directory structure.

### Validation via Built-in Skills

- **AC-026**: GIVEN implementation is complete
  WHEN `plugin-dev:plugin-validator` is run against the `claude-plugin/` directory
  THEN the validator reports no structural errors (valid plugin.json, all referenced files exist, frontmatter schema valid).

- **AC-027**: GIVEN each new skill (`debug-with-grafana`, `manage-dashboards`)
  WHEN `skill-creator:skill-reviewer` is run against the skill
  THEN the reviewer reports no critical issues and the skill description scores adequately for triggering effectiveness.

- **AC-028**: GIVEN the `grafana-debugger` agent
  WHEN the agent frontmatter is validated against `plugin-dev:agent-development` conventions
  THEN the agent has valid `name`, `description` with examples, `color`, and `tools` fields matching the documented schema.

## Negative Constraints

- **NC-001**: Skills and agent files MUST NOT contain fabricated CLI output with specific metric values or timestamps. Examples MUST show output field structure (e.g., `{status, data.result[].metric, data.result[].value}`) but MUST NOT invent concrete numbers that could mislead agents into expecting specific values.
- **NC-002**: No file in this spec MUST modify any Go source code, Makefile, or build artifact. All deliverables are markdown files under `claude-plugin/`.
- **NC-003**: The `manage-dashboards` skill MUST NOT recommend pushing dashboards without folders when both are present in the local directory. The topological sort requirement MUST always be documented.
- **NC-004**: The `grafana-debugger` agent MUST NOT inline the full step-by-step diagnostic workflow. It MUST delegate to the `debug-with-grafana` skill for procedural steps.
- **NC-005**: No skill or agent MUST reference datasources by display name in command examples. All query examples MUST use `-d <uid>` placeholder or a resolved UID variable.
- **NC-006**: The `error-recovery.md` MUST NOT advise users to modify gcx source code or bypass authentication. Recovery actions MUST be limited to CLI commands and configuration changes.
- **NC-007**: Skills MUST NOT use `--start` / `--end` and `--from` / `--to` interchangeably.
- **NC-008**: No skill or agent file MUST reference files outside the `claude-plugin/` directory. All reference file paths in SKILL.md and agent prompts MUST be relative paths that resolve within `claude-plugin/`. The plugin MUST be self-contained so it works when installed at any location (e.g., `~/.claude/plugins/gcx/`). All time range flags MUST use `--from` / `--to` as verified against the actual CLI source (`cmd/gcx/query/command.go`).

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| CLI flag names change between gcx versions | Examples become incorrect, agents produce failing commands | Pin examples to verified flag names from current codebase; document flag source in a comment at top of each reference file |
| Auto-trigger descriptions overlap between `debug-with-grafana` skill and `grafana-debugger` agent | Both activate simultaneously causing confusion | Agent description emphasizes symptom-based triggers (specific errors, latency numbers); skill description emphasizes workflow-based triggers (step-by-step debugging). Claude plugin runtime handles multi-match gracefully. |
| 300-line agent prompt exceeds context budget for some LLM consumers | Agent prompt is truncated or ignored by smaller-context models | Keep prompt dense and structured; use headings for skip-scanning; place most critical rules in first 100 lines |
| Existing `query-patterns.md` uses `--from`/`--to` while `investigate-alert` uses `--start`/`--end` | Inconsistent flag names across references confuse agents | Audit and reconcile all time-range flag references during implementation; NC-007 enforces consistency |
| `resource-model.md` is in `.claude/skills/gcx/references/` not in `claude-plugin/` | Cross-directory reference may not resolve in plugin context | Use relative path or copy/adapt content into `claude-plugin/skills/manage-dashboards/references/resource-model.md` |

## Open Questions

- **[RESOLVED]** Should `error-recovery.md` live under `debug-with-grafana` or be shared across skills?
  Decision: Under `debug-with-grafana/references/` since it is specific to diagnostic query failures. Other skills can reference it by relative path if needed.

- **[RESOLVED]** Should the grafana-debugger agent be longer than 300 lines?
  Decision: Minimum 300 lines to ensure sufficient depth. No maximum enforced, but density is preferred over padding.

- **[RESOLVED]** The existing `query-patterns.md` uses `--from`/`--to` flags while `investigate-alert` SKILL.md uses `--start`/`--end`. Which is the correct flag set for `gcx query`?
  Decision: Verified against `cmd/gcx/query/command.go` — the correct flags are `--from`/`--to`. The `investigate-alert` SKILL.md uses incorrect flag names but fixing it is out of scope for this spec. All new files MUST use `--from`/`--to`.

- **[DEFERRED]** Should there be a shared `common-references/` directory under `claude-plugin/` for files referenced by multiple skills? Deferred to Stage 3 packaging review.

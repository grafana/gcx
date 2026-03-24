---
type: feature-tasks
title: "Stage 2: Core Workflow Skills"
status: draft
spec: spec/feature-stage-2-core-workflow-skills/spec.md
plan: spec/feature-stage-2-core-workflow-skills/plan.md
created: 2026-03-07
---

# Implementation Tasks

## Dependency Graph

```
T1 (CLI flag audit) ──────────┬──────────────────┐
                               │                  │
                               v                  v
                    T2 (error-recovery.md)   T4 (resource-operations.md)
                               │                  │
                               v                  v
                    T3 (debug-with-grafana   T5 (manage-dashboards
                        SKILL.md)                SKILL.md)
                               │                  │
                               └────────┬─────────┘
                                        v
                               T6 (grafana-debugger agent)
                                        │
                                        v
                               T7 (plugin-wide validation)
```

## Wave 1: CLI Flag Audit

### T1: Audit CLI Flags and Produce Reference Table

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: chore

Audit the actual grafanactl CLI to produce a verified flag reference table for all commands used in the new skills: `query`, `get`, `push`, `pull`, `delete`, `edit`, `validate`, `serve`, `datasources list`, `datasources prometheus *`, `datasources loki *`, `resources list`, `config view`, `config set`, `config use-context`, `config current-context`. For each command, record the exact flag names, short aliases, and default values from the Go source in `cmd/grafanactl/`. The output is a markdown table stored at `spec/feature-stage-2-core-workflow-skills/cli-flag-audit.md` and referenced by all subsequent authoring tasks.

Key verification: confirm `grafanactl query` uses `--from`/`--to` (not `--start`/`--end`). Confirm resource commands use `--dry-run`, `--include-managed`, `--omit-manager-fields`, `--force`, `--path`, `--max-concurrent`. Confirm `serve` uses `--address`, `--port`, `--no-watch`, `--script`, `--script-format`.

**Deliverables:**
- `spec/feature-stage-2-core-workflow-skills/cli-flag-audit.md`

**Acceptance criteria:**
- GIVEN the audit file exists WHEN each flag name is compared against the corresponding Go source in `cmd/grafanactl/` THEN every flag name, short alias, and default value matches the source code exactly (traces to AC-024)
- GIVEN the audit documents `grafanactl query` flags WHEN the time-range flags are inspected THEN they list `--from` and `--to` (not `--start`/`--end`) (traces to NC-007)

---

## Wave 2: Reference Files (parallel)

### T2: Write error-recovery.md

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create `claude-plugin/skills/debug-with-grafana/references/error-recovery.md` documenting recovery patterns for CLI failures encountered during diagnostic workflows. The file MUST cover at least 5 failure modes: (a) 401/403 authentication/authorization errors, (b) datasource not found, (c) query returns empty result set, (d) query timeout or server error (5xx), (e) malformed PromQL/LogQL syntax errors. Each failure mode MUST include the error message pattern to match, the likely cause, and the corrective action using specific grafanactl commands.

Use flag names from the T1 audit. Recovery actions MUST be limited to CLI commands and configuration changes (NC-006). MUST NOT advise modifying grafanactl source code.

**Deliverables:**
- `claude-plugin/skills/debug-with-grafana/references/error-recovery.md`

**Acceptance criteria:**
- GIVEN the file exists WHEN failure modes are counted THEN at least 5 are present covering 401/403, datasource not found, empty results, timeout/5xx, and malformed query syntax (traces to AC-007, FR-011)
- GIVEN each failure mode entry WHEN inspected THEN it contains an error message pattern, a likely cause description, and a corrective action with specific grafanactl commands (traces to AC-008, FR-012)
- GIVEN the corrective actions WHEN inspected THEN none advise modifying source code or bypassing authentication (traces to NC-006)
- GIVEN all grafanactl commands in the file WHEN checked against the T1 audit THEN every flag name matches the verified CLI flags (traces to AC-024)

---

### T4: Write resource-operations.md

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create `claude-plugin/skills/manage-dashboards/references/resource-operations.md` documenting the full flag set and usage patterns for each resource command: `get`, `push`, `pull`, `delete`, `edit`, `validate`, `serve`. Include selector syntax documentation covering kind selectors (`dashboards`), UID selectors (`dashboards/my-uid`), glob patterns (`dashboards/my-*`), and multi-selector (`dashboards folders`). Document the `serve` command workflow including live dev server with watch, hot reload, and browser preview.

Use flag names and defaults from the T1 audit exclusively.

**Deliverables:**
- `claude-plugin/skills/manage-dashboards/references/resource-operations.md`

**Acceptance criteria:**
- GIVEN the file exists WHEN resource commands are enumerated THEN all 7 commands are documented: get, push, pull, delete, edit, validate, serve (traces to AC-014, FR-023)
- GIVEN the selector syntax section WHEN inspected THEN it documents kind selectors, UID selectors, glob patterns, and multi-selector usage with examples (traces to AC-015, FR-024)
- GIVEN the serve command section WHEN inspected THEN it documents the live dev server workflow with watch, hot reload, and browser preview (traces to FR-025)
- GIVEN all flags in the file WHEN checked against the T1 audit THEN every flag name and default matches the verified CLI flags (traces to AC-024)

---

## Wave 3: Skill SKILL.md Files (parallel)

### T3: Write debug-with-grafana SKILL.md

**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T1, T2
**Type**: task

Create `claude-plugin/skills/debug-with-grafana/SKILL.md` with YAML frontmatter (`name: debug-with-grafana`, `description` field for auto-triggering on debugging/diagnosing/troubleshooting requests), a prerequisites section pointing to `setup-grafanactl`, and the 7-step diagnostic workflow: (1) discover datasources, (2) confirm data availability, (3) query error rates, (4) query latency, (5) correlate logs, (6) check related dashboards/resources, (7) summarize findings.

Include 3 worked example scenarios: HTTP 500 error spike, latency degradation, and service down / no data. Each scenario MUST show the sequence of grafanactl commands and the expected output shape (field structure, not fabricated values per NC-001).

Reference `references/error-recovery.md` (written in T2) and `references/query-patterns.md`. **Copy** `.claude/skills/grafanactl/references/query-patterns.md` to `claude-plugin/skills/debug-with-grafana/references/query-patterns.md` — do NOT use an external path (NC-008). Use `--from`/`--to` for all time-range flags per NC-007. Use `-d <uid>` for all datasource references per NC-005.

Invoke `plugin-dev:skill-development` during authoring. Run `skill-creator:skill-reviewer` after the file is written.

**Deliverables:**
- `claude-plugin/skills/debug-with-grafana/SKILL.md`
- `claude-plugin/skills/debug-with-grafana/references/query-patterns.md` (copy from `.claude/skills/grafanactl/references/query-patterns.md`)

**Acceptance criteria:**
- GIVEN the file exists WHEN YAML frontmatter is parsed THEN it contains `name: debug-with-grafana` and a non-empty `description` field (traces to AC-001, FR-002)
- GIVEN the SKILL.md content WHEN workflow steps are counted THEN exactly 7 numbered steps are present in the specified order (traces to AC-002, FR-003)
- GIVEN each workflow step WHEN inspected THEN at least one grafanactl command is present with correct flag syntax (`-d`, `-e`, `--from`, `--to`, `--step`, `-o`) (traces to AC-003, FR-004)
- GIVEN the SKILL.md content WHEN example scenarios are counted THEN exactly 3 scenarios are present: HTTP 500 error spike, latency degradation, service down / no data (traces to AC-004, FR-005)
- GIVEN each example scenario WHEN inspected THEN it contains a sequence of grafanactl commands and a description of expected output shape (traces to AC-005, FR-006)
- GIVEN the SKILL.md content WHEN references are inspected THEN it contains a reference link to `references/error-recovery.md` and a reference to `query-patterns.md` (traces to AC-006, FR-007, FR-008)
- GIVEN the SKILL.md content WHEN the prerequisites section is inspected THEN it directs to `setup-grafanactl` (traces to FR-009)
- GIVEN a user message about debugging application issues WHEN auto-trigger matching is evaluated THEN the description field is a candidate match (traces to AC-022, FR-034)
- GIVEN `skill-creator:skill-reviewer` is run WHEN it evaluates the skill THEN no critical issues are reported (traces to AC-027, FR-040)
- GIVEN all command examples WHEN inspected THEN no fabricated metric values or timestamps appear (traces to NC-001)
- GIVEN all datasource references in commands WHEN inspected THEN they use `-d <uid>` placeholder (traces to NC-005)
- GIVEN the skill's reference files WHEN file paths are checked THEN `references/query-patterns.md` exists inside `claude-plugin/skills/debug-with-grafana/references/` (no external paths) (traces to NC-008, FR-008)

---

### T5: Write manage-dashboards SKILL.md

**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T1, T4
**Type**: task

Create `claude-plugin/skills/manage-dashboards/SKILL.md` with YAML frontmatter (`name: manage-dashboards`, `description` field for auto-triggering on pull/push/create/validate/promote dashboard requests), a prerequisites section pointing to `setup-grafanactl`, and 5 documented workflows: (a) pull dashboards from Grafana to local files, (b) push dashboards from local files to Grafana, (c) create a new dashboard from scratch, (d) validate dashboard files, (e) promote dashboards across environments.

The push workflow MUST document topological sort (folders before dashboards) and manager metadata (`grafana.app/managed-by: grafanactl` annotation, `--include-managed` override). The promote workflow MUST document the multi-context pattern using `--context` flag or `use-context`.

Reference `references/resource-operations.md` (written in T4) and `references/resource-model.md`. **Copy** `.claude/skills/grafanactl/references/resource-model.md` to `claude-plugin/skills/manage-dashboards/references/resource-model.md` — do NOT use an external path (NC-008).

Invoke `plugin-dev:skill-development` during authoring. Run `skill-creator:skill-reviewer` after the file is written.

**Deliverables:**
- `claude-plugin/skills/manage-dashboards/SKILL.md`
- `claude-plugin/skills/manage-dashboards/references/resource-model.md` (copy from `.claude/skills/grafanactl/references/resource-model.md`)

**Acceptance criteria:**
- GIVEN the file exists WHEN YAML frontmatter is parsed THEN it contains `name: manage-dashboards` and a non-empty `description` field (traces to AC-009, FR-014)
- GIVEN the SKILL.md content WHEN workflows are enumerated THEN all 5 workflows are present: pull, push, create, validate, promote (traces to AC-010, FR-015)
- GIVEN the push workflow section WHEN inspected THEN it documents topological sort (folders before dashboards) and manager metadata (`grafana.app/managed-by: grafanactl`) (traces to AC-011, FR-016, FR-017, NC-003)
- GIVEN the promote workflow section WHEN inspected THEN it documents multi-context pattern using `--context` flag or `use-context` (traces to AC-012, FR-018)
- GIVEN the SKILL.md content WHEN references are inspected THEN it contains reference links to `references/resource-operations.md` and to `resource-model.md` (traces to AC-013, FR-019, FR-020)
- GIVEN the SKILL.md content WHEN the prerequisites section is inspected THEN it directs to `setup-grafanactl` (traces to FR-021)
- GIVEN a user message about pulling/pushing/promoting dashboards WHEN auto-trigger matching is evaluated THEN the description field is a candidate match (traces to AC-023, FR-035)
- GIVEN `skill-creator:skill-reviewer` is run WHEN it evaluates the skill THEN no critical issues are reported (traces to AC-027, FR-040)
- GIVEN the skill's reference files WHEN file paths are checked THEN `references/resource-model.md` exists inside `claude-plugin/skills/manage-dashboards/references/` (no external paths) (traces to NC-008, FR-020)

---

## Wave 4: Grafana Debugger Agent

### T6: Rewrite grafana-debugger Agent

**Priority**: P1
**Effort**: Large
**Depends on**: T3
**Type**: task

Replace the current ~183-line stub at `claude-plugin/agents/grafana-debugger.md` with a full specialist system prompt of at least 300 lines (excluding frontmatter). The YAML frontmatter MUST retain `name: grafana-debugger` and include a `description` field with at least 4 `<example>` trigger phrases targeting symptom-based requests (500 errors, latency spikes, service down, elevated error rates), `color`, and `tools` list (Bash, Read, Grep).

The system prompt MUST include:
- A diagnostic methodology section covering error spikes, latency degradation, resource exhaustion, and service-down scenarios (FR-029).
- At least 2 complete worked examples showing diagnostic flows with commands and reasoning (FR-030).
- Explicit delegation to `debug-with-grafana` skill for step-by-step workflows and to `investigate-alert` skill for alert-specific investigations (FR-031). The agent MUST NOT inline the full diagnostic workflow (NC-004).
- An error-recovery section referencing `error-recovery.md` (FR-032).
- Output formatting rules: `-o json` for data retrieval, `-o graph` for visualization, datasource UIDs only (FR-033, NC-005).

Use `--from`/`--to` for all time-range flags per NC-007. Invoke `plugin-dev:agent-development` during authoring.

**Deliverables:**
- `claude-plugin/agents/grafana-debugger.md` (overwrite)

**Acceptance criteria:**
- GIVEN the file exists WHEN YAML frontmatter is parsed THEN it contains `name: grafana-debugger`, a `description` with at least 4 `<example>` trigger phrases, a `color` field, and a `tools` list (traces to AC-016, FR-027)
- GIVEN the agent system prompt WHEN line count is measured (excluding frontmatter) THEN the prompt is at least 300 lines (traces to AC-017, FR-028)
- GIVEN the agent system prompt WHEN inspected THEN it contains a diagnostic methodology section covering error spikes, latency degradation, resource exhaustion, and service-down scenarios (traces to AC-018, FR-029)
- GIVEN the agent system prompt WHEN worked examples are counted THEN at least 2 complete diagnostic flow examples are present with commands and reasoning (traces to AC-019, FR-030)
- GIVEN the agent system prompt WHEN delegation references are inspected THEN it explicitly names `debug-with-grafana` and `investigate-alert` as delegation targets (traces to AC-020, FR-031)
- GIVEN the agent system prompt WHEN output rules are inspected THEN it mandates `-o json` for data retrieval, `-o graph` for visualization, and datasource UIDs (not display names) (traces to AC-021, FR-033)
- GIVEN the agent system prompt WHEN inspected for inlined workflows THEN the full 7-step diagnostic procedure is NOT inlined; the agent delegates to the skill (traces to NC-004)
- GIVEN a user message describing 500 errors or latency spikes WHEN auto-trigger matching is evaluated THEN the agent description is a candidate match (traces to AC-022, FR-036)
- GIVEN the agent frontmatter WHEN validated against plugin-dev:agent-development conventions THEN name, description, color, and tools fields match the documented schema (traces to AC-028, FR-038)

---

## Wave 5: Plugin-Wide Validation

### T7: Run Plugin Validator and Final Checks

**Priority**: P1
**Effort**: Small
**Depends on**: T3, T5, T6
**Type**: chore

Run `plugin-dev:plugin-validator` against the entire `claude-plugin/` directory to validate structural integrity: plugin.json validity, all referenced file paths exist, frontmatter schema validity for all skills and agents. Verify that all file paths conform to the canonical directory structure (`skills/<name>/SKILL.md`, `skills/<name>/references/*.md`, `agents/<name>.md`).

Perform a final cross-file consistency check: all grafanactl command examples across T2, T3, T4, T5, and T6 use flag names from the T1 audit. All datasource references use `-d <uid>` placeholder. No fabricated output values exist.

**Deliverables:**
- Validation report (pass/fail) — no new files; existing files are corrected if validator finds issues

**Acceptance criteria:**
- GIVEN `plugin-dev:plugin-validator` is run against `claude-plugin/` WHEN validation completes THEN no structural errors are reported (valid plugin.json, all referenced files exist, frontmatter schema valid) (traces to AC-026, FR-039)
- GIVEN all new files WHEN file paths are checked THEN all reside under `claude-plugin/` following canonical skill and agent directory structure (traces to AC-025)
- GIVEN all grafanactl command examples across all new files WHEN each command is checked against the T1 audit THEN every flag name, subcommand path, and argument position matches the actual CLI interface (traces to AC-024)

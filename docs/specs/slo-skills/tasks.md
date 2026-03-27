---
type: feature-tasks
title: "SLO and Synthetic Monitoring Agent Skills"
status: approved
spec: docs/specs/slo-skills/spec.md
plan: docs/specs/slo-skills/plan.md
created: 2026-03-09
---

# Implementation Tasks

## Dependency Graph

```
T1 (flag standardization)
├──→ T2 (slo-manage)
├──→ T3 (slo-check-status)
├──→ T4 (slo-investigate)
├──→ T5 (slo-optimize)
├──→ T6 (synth-check-status)
├──→ T7 (synth-investigate-check)
├──→ T8 (synth-manage-checks)
└──→ T9 (integration review) ← depends on T2–T8
```

## Wave 1: Flag Standardization

### T1: Standardize timeline command time range flags to --from/--to/--window

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Add `--from` and `--to` as the primary time range flags on all SLO and SM timeline commands, add `--window` convenience flag to SLO timeline commands, and retain `--start`/`--end` as hidden deprecated aliases on SLO timeline commands. Add mutual exclusivity validation between `--window` and `--from`/`--to`. Update error messages to reference the new flag names. Add tests for all new flags and backward compatibility.

**Deliverables:**
- `internal/providers/slo/definitions/timeline.go` (add `--from`, `--to`, `--window` flags; deprecate `--start`/`--end`)
- `internal/providers/slo/definitions/timeline_test.go` (new test cases for `--from`/`--to`, `--window`, mutual exclusivity, backward compat)
- `internal/providers/slo/reports/timeline.go` (add `--from`, `--to`, `--window` flags; deprecate `--start`/`--end`)
- `internal/providers/slo/reports/timeline_test.go` (new test cases mirroring definitions)
- `internal/providers/synth/checks/status.go` (add `--from`, `--to` flags to timeline command; mutual exclusivity with `--window`)
- `internal/providers/synth/checks/status_test.go` (new test cases for `--from`/`--to` and mutual exclusivity)

**Acceptance criteria:**
- GIVEN the SLO definitions timeline command
  WHEN `--from now-7d --to now` flags are passed
  THEN it MUST produce identical output to the current `--start now-7d --end now` behavior

- GIVEN the SLO definitions timeline command
  WHEN `--window 7d` flag is passed
  THEN it MUST set from=now-7d and to=now internally, producing the same output as `--from now-7d --to now`

- GIVEN the SLO reports timeline command
  WHEN `--from now-7d --to now` flags are passed
  THEN it MUST produce identical output to the current `--start now-7d --end now` behavior

- GIVEN the SM checks timeline command
  WHEN `--from now-1h --to now` flags are passed
  THEN it MUST produce the same results as the current `--window 1h` behavior

- GIVEN any timeline command
  WHEN both `--window` and `--from`/`--to` are provided simultaneously
  THEN the command MUST return an error indicating the flags are mutually exclusive

- GIVEN the SLO definitions timeline command help output
  WHEN `--help` is run
  THEN `--from`, `--to`, and `--window` MUST appear in the flag list, and `--start`/`--end` MUST be hidden

- GIVEN the deprecated `--start`/`--end` flags on SLO timeline commands
  WHEN `--start now-7d --end now` is used
  THEN the command MUST still function correctly (backward compatibility)

- GIVEN existing tests for SLO timeline commands
  WHEN the test suite is run after the code change
  THEN all existing tests MUST pass, and new tests for `--from`/`--to` and `--window` MUST be added

---

## Wave 2: Skill Files (parallel)

### T2: slo-manage skill and slo-templates reference

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the slo-manage skill file with 4 workflows (create, update, GitOps sync, delete), query type decision table, dry-run-before-push convention, push semantics documentation, and configuration guidance. Create the references/slo-templates.md file with YAML templates for freeform, ratio, and threshold query types. Include YAML frontmatter, Core Principles, negative routing to slo-check-status and slo-investigate, error handling section, and output format section.

**Deliverables:**
- `.claude/skills/slo-manage/SKILL.md`
- `.claude/skills/slo-manage/references/slo-templates.md`

**Acceptance criteria:**
- GIVEN a user asking "create me an SLO for my API"
  WHEN the slo-manage skill is triggered
  THEN the agent MUST determine query type via the decision table, build a YAML definition from the template, resolve the destination datasource UID, dry-run push, and then push

- GIVEN a user asking to update an existing SLO's objective
  WHEN the slo-manage skill is triggered
  THEN the agent MUST pull the current definition with `gcx slo definitions get <UUID>`, modify the requested fields, dry-run push, and then push

- GIVEN a user asking "pull my SLOs"
  WHEN the slo-manage skill is triggered
  THEN the agent MUST run `gcx slo definitions pull -d <dir>` and report the output directory

- GIVEN a user asking to delete an SLO
  WHEN the slo-manage skill is triggered
  THEN the agent MUST confirm the SLO identity and run `gcx slo definitions delete <UUID> -f`

- GIVEN an SLO creation workflow
  WHEN the YAML file is being built
  THEN it MUST use `apiVersion: slo.ext.grafana.app/v1alpha1` and `kind: SLO` with the correct spec structure

- GIVEN the slo-manage skill
  WHEN a push operation is initiated
  THEN the skill MUST always run `--dry-run` first, then actual push only after dry-run succeeds

- GIVEN the references/slo-templates.md file
  WHEN inspected
  THEN it MUST contain YAML templates for freeform, ratio, and threshold query types with inline field documentation

- GIVEN the slo-manage skill
  WHEN inspected for allowed-tools
  THEN it MUST list `[gcx, Bash, Read, Write, Edit]`

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

- GIVEN the references/slo-templates.md file
  WHEN line count is measured
  THEN it MUST be under 200 lines

---

### T3: slo-check-status skill

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Create the slo-check-status skill with list, status, status-wide, conditional timeline, reports status, and routing guidance. Include YAML frontmatter, Core Principles, negative routing to slo-investigate and slo-optimize, error handling, and output format sections. Uses `--from`/`--to` for timeline commands.

**Deliverables:**
- `.claude/skills/slo-check-status/SKILL.md`

**Acceptance criteria:**
- GIVEN a user asking "how are my SLOs doing"
  WHEN the slo-check-status skill is triggered
  THEN the agent MUST run `gcx slo definitions status` and present a table of SLO health

- GIVEN a user asking about a specific SLO's status
  WHEN the slo-check-status skill is triggered
  THEN the agent MUST run `gcx slo definitions status <UUID> -o wide` to show SLI, budget, burn rate, SLI_1H, and SLI_1D

- GIVEN any SLO with status BREACHING in the status output
  WHEN the status step completes
  THEN the skill MUST show the SLO timeline and suggest using slo-investigate for deeper analysis

- GIVEN a user asking about SLO trends
  WHEN the slo-check-status skill is triggered
  THEN the agent MUST run `gcx slo definitions timeline [UUID]` with graph output

- GIVEN an SLO with status NODATA
  WHEN the status is displayed
  THEN the agent MUST note that recording rule metrics are unavailable and suggest checking the destination datasource configuration

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

---

### T4: slo-investigate skill and slo-promql-patterns reference

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the slo-investigate skill with the decision-branching investigation workflow: definition retrieval, status with `-o wide`, timeline, PromQL dimensional breakdown (ratio + freeform query handling), alert rule search, runbook/dashboard extraction. Implement early exit for OK and NODATA states. Create references/slo-promql-patterns.md with PromQL patterns for SLO metrics. Include structured output format template.

**Deliverables:**
- `.claude/skills/slo-investigate/SKILL.md`
- `.claude/skills/slo-investigate/references/slo-promql-patterns.md`

**Acceptance criteria:**
- GIVEN a user saying "my SLO is breaching, investigate"
  WHEN the slo-investigate skill is triggered
  THEN the agent MUST execute the full investigation workflow: definition retrieval, status with `-o wide`, timeline rendering, PromQL dimensional breakdown, alert rule search, and runbook/dashboard extraction

- GIVEN an SLO with status OK
  WHEN the slo-investigate skill is triggered
  THEN the agent MUST report current health metrics (SLI, budget, burn rate) and stop (early exit)

- GIVEN an SLO with a ratio query and status BREACHING
  WHEN the investigation reaches dimensional breakdown
  THEN the agent MUST extract the success/total metric selectors and groupByLabels from the definition and run `gcx query` with those selectors grouped by the relevant dimensions

- GIVEN an SLO with a freeform query and status BREACHING
  WHEN the investigation reaches dimensional breakdown
  THEN the agent MUST use the raw PromQL expression and add `by (<label>)` grouping for dimensional analysis

- GIVEN an SLO with annotations containing a GitHub runbook URL
  WHEN the investigation reaches runbook extraction
  THEN the agent MUST fetch runbook content using `gh api` if `gh` is available

- GIVEN an SLO with status NODATA
  WHEN the investigation is triggered
  THEN the agent MUST report that metrics are unavailable and suggest checking the destination datasource configuration and recording rule health

- GIVEN the slo-investigate skill output
  WHEN the investigation is complete
  THEN the output MUST follow the structured template: SLO name, target/window, SLI, budget, burn rate, dimensions, timeline, alerts, runbook, and next actions

- GIVEN the references/slo-promql-patterns.md file
  WHEN inspected
  THEN it MUST contain PromQL patterns for grafana_slo_sli_window, grafana_slo_sli_1h, grafana_slo_sli_1d, grafana_slo_success_rate_5m, grafana_slo_total_rate_5m, grafana_slo_objective, and the burn rate computation formula

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

- GIVEN the references/slo-promql-patterns.md file
  WHEN line count is measured
  THEN it MUST be under 200 lines

---

### T5: slo-optimize skill

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the slo-optimize skill with timeline trend analysis (sustained decline, periodic dips, sudden drops, budget exhaustion rate), advisory recommendation generation (objective tuning, groupByLabels additions, alerting sensitivity, window adjustments), and routing to slo-manage for execution. The skill MUST NOT modify SLO definitions directly.

**Deliverables:**
- `.claude/skills/slo-optimize/SKILL.md`

**Acceptance criteria:**
- GIVEN a user asking "check my SLO's performance and suggest improvements"
  WHEN the slo-optimize skill is triggered
  THEN the agent MUST retrieve the SLO definition, fetch 28-day timeline data, compute trend statistics, and present advisory recommendations

- GIVEN an SLO with a 28-day timeline showing sustained SLI decline over 7+ days
  WHEN the trend analysis step completes
  THEN the agent MUST classify this as "sustained decline" and recommend investigating underlying service degradation

- GIVEN an SLO where the average SLI over the analysis window is more than 0.5 percentage points below the objective
  WHEN the recommendation step runs
  THEN the agent MUST suggest adjusting the objective value to a realistic target based on observed performance

- GIVEN an SLO where the average SLI over the analysis window is more than 1 percentage point above the objective
  WHEN the recommendation step runs
  THEN the agent MUST suggest tightening the objective to better reflect achievable performance

- GIVEN an SLO with no groupByLabels configured and a ratio query type
  WHEN the recommendation step runs
  THEN the agent MUST suggest adding groupByLabels for dimensional visibility

- GIVEN an SLO with no alerting configuration
  WHEN the recommendation step runs
  THEN the agent MUST recommend configuring fastBurn and slowBurn alerts

- GIVEN an SLO with alerting configured and burn rate consistently above 2x for the past 7 days
  WHEN the recommendation step runs
  THEN the agent MUST suggest reviewing alerting sensitivity or investigating persistent error sources

- GIVEN the slo-optimize skill producing a recommendation the user wants to apply
  WHEN the user approves the change
  THEN the agent MUST route to the slo-manage skill for execution

- GIVEN the slo-optimize skill output
  WHEN analysis is complete
  THEN the output MUST include: SLO name, objective/window, analysis period, current SLI statistics (mean, min, max), budget consumption rate, trend classification, and numbered advisory recommendations with supporting data

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

---

### T6: synth-check-status skill

**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task

Create the synth-check-status skill with list, status, conditional timeline, status interpretation (OK/FAILING/NODATA), timeline pattern interpretation, and routing to synth-investigate-check. Uses `--window` or `--from`/`--to` for timeline commands.

**Deliverables:**
- `.claude/skills/synth-check-status/SKILL.md`

**Acceptance criteria:**
- GIVEN a user asking "are my checks healthy"
  WHEN the synth-check-status skill is triggered
  THEN the agent MUST run `gcx synth checks list` followed by `gcx synth checks status`

- GIVEN a check with status FAILING
  WHEN the status step completes
  THEN the skill MUST suggest viewing the timeline and mention the synth-investigate-check skill for deeper investigation

- GIVEN a user asking about trends for a specific check
  WHEN the synth-check-status skill is triggered
  THEN the agent MUST run `gcx synth checks timeline <ID>` with graph output

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

---

### T7: synth-investigate-check skill with failure-modes and sm-promql-patterns references

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the synth-investigate-check skill with the decision-branching investigation workflow: status check, configuration retrieval, timeline triage, failure scope classification, per-probe breakdown with geographic mapping, conditional deeper PromQL metrics, diagnosis and next actions. Implement early exit for OK and NODATA. Create references/failure-modes.md with 8 failure modes and references/sm-promql-patterns.md with PromQL patterns for SM metrics. Include structured output format template.

**Deliverables:**
- `.claude/skills/synth-investigate-check/SKILL.md`
- `.claude/skills/synth-investigate-check/references/failure-modes.md`
- `.claude/skills/synth-investigate-check/references/sm-promql-patterns.md`

**Acceptance criteria:**
- GIVEN a user asking "why is check X failing"
  WHEN the synth-investigate-check skill is triggered
  THEN the agent MUST execute the full investigation workflow: status check, configuration retrieval, timeline triage, probe breakdown, optional deeper metrics, and diagnosis with next actions

- GIVEN a check with status OK
  WHEN the investigation skill is triggered
  THEN the agent MUST report that the check is healthy with its success rate and stop (early exit)

- GIVEN a check with status NODATA
  WHEN the investigation skill is triggered
  THEN the agent MUST check if the check is enabled and verify datasource configuration before concluding

- GIVEN timeline data showing all probes at 0
  WHEN the triage step analyzes the data
  THEN the agent MUST classify this as a "target down" failure pattern

- GIVEN timeline data showing a subset of probes failing
  WHEN the triage step analyzes the data
  THEN the agent MUST classify this as a "regional/network" failure pattern and map probe names to regions using `gcx synth probes list`

- GIVEN the references/failure-modes.md file
  WHEN inspected
  THEN it MUST contain exactly 8 failure modes (target down, regional/CDN, SSL/TLS, DNS resolution, timeout, content/assertion, private probe infra, rate limiting) with Signals, Likely Cause, and Next Action columns

- GIVEN the references/sm-promql-patterns.md file
  WHEN inspected
  THEN it MUST contain PromQL patterns for probe_success rate, HTTP phase latency (probe_http_duration_seconds), SSL cert expiry (probe_ssl_earliest_cert_expiry), and per-probe error rates

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

- GIVEN the references/failure-modes.md file
  WHEN line count is measured
  THEN it MUST be under 200 lines

- GIVEN the references/sm-promql-patterns.md file
  WHEN line count is measured
  THEN it MUST be under 200 lines

---

### T8: synth-manage-checks skill and check-types reference

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Create the synth-manage-checks skill with 4 workflows (create, update, GitOps sync, delete), check type decision table, probe selection guidance, dry-run-before-push convention, push semantics (numeric vs non-numeric metadata.name), and configuration guidance. Create references/check-types.md with decision tree for check type selection and YAML examples for HTTP, Ping, DNS, TCP, and Traceroute.

**Deliverables:**
- `.claude/skills/synth-manage-checks/SKILL.md`
- `.claude/skills/synth-manage-checks/references/check-types.md`

**Acceptance criteria:**
- GIVEN a user asking "create a check for my API"
  WHEN the synth-manage-checks skill is triggered
  THEN the agent MUST determine check type, list probes, build YAML from the template, dry-run push, and then push

- GIVEN a user asking "pull my SM checks"
  WHEN the synth-manage-checks skill is triggered
  THEN the agent MUST run `gcx synth checks pull -d <dir>`

- GIVEN a check creation workflow
  WHEN the YAML file is being built
  THEN it MUST use `apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1` and `kind: Check` with the correct spec structure

- GIVEN the synth-manage-checks skill
  WHEN a push operation is initiated
  THEN the skill MUST always run `--dry-run` first, then actual push only after dry-run succeeds

- GIVEN the references/check-types.md file
  WHEN inspected
  THEN it MUST contain a decision tree for check type selection and YAML examples for at least HTTP, Ping, DNS, TCP, and Traceroute check types

- GIVEN the synth-manage-checks skill
  WHEN inspected for allowed-tools
  THEN it MUST list `[gcx, Bash, Read, Write, Edit]`

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

- GIVEN the references/check-types.md file
  WHEN line count is measured
  THEN it MUST be under 200 lines

---

## Wave 3: Integration Review

### T9: Cross-skill integration validation

**Priority**: P2
**Effort**: Small
**Depends on**: T2, T3, T4, T5, T6, T7, T8
**Type**: chore

Validate all 7 skills against the shared structure acceptance criteria from the spec. Verify YAML frontmatter fields, negative routing clauses, Core Principles sections, error handling sections, output format sections, line count limits, `--from`/`--to` flag usage (no `--start`/`--end` references), and datasource UID auto-resolution patterns. Fix any deviations.

**Deliverables:**
- All 7 SKILL.md files validated and corrected if needed
- All 5 reference files validated for line count and content completeness

**Acceptance criteria:**
- GIVEN a completed skill file
  WHEN inspected
  THEN it MUST have YAML frontmatter with `name`, `description`, and `allowed-tools` fields

- GIVEN a skill's description field
  WHEN parsed
  THEN it MUST contain at least one negative routing clause referencing a sibling skill by name

- GIVEN any completed skill file
  WHEN line count is measured
  THEN it MUST be under 400 lines

- GIVEN any completed skill file
  WHEN inspected
  THEN it MUST contain a "Core Principles" section, an error handling section, and an output format section

- GIVEN any skill that references a time-range command
  WHEN the command is written
  THEN it MUST use `--from`/`--to` or `--window` flags (not `--start`/`--end`)

- GIVEN any reference file
  WHEN line count is measured
  THEN it MUST be under 200 lines

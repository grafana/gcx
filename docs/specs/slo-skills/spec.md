---
type: feature-spec
title: "SLO and Synthetic Monitoring Agent Skills"
status: done
research: docs/research/2026-03-07-grafanactl-synth-skills-for-agents.md
created: 2026-03-09
---

# SLO and Synthetic Monitoring Agent Skills

## Problem Statement

Operators using grafanactl through Claude Code agents lack guided workflows for two key observability domains: SLO monitoring and Synthetic Monitoring. Without skills, agents must discover commands ad-hoc, miss diagnostic steps, produce inconsistent output, and fail to route between related workflows (status check vs. investigation vs. management vs. optimization).

The SLO provider (`grafanactl slo`) has `definitions` and `reports` subcommands with `status`, `timeline`, `list`, `get`, `push`, `pull`, and `delete` operations. The Synthetic Monitoring provider (`grafanactl synth`) has `checks` and `probes` subcommands with equivalent operations. Both providers support Prometheus metric queries via `grafanactl query`. No agent skills exist for either domain today.

The current workaround is for agents to read CLAUDE.md, guess which commands to run, and produce ad-hoc analysis without structured failure mode classification or cross-skill routing. For SLO management, users must manually construct YAML definitions from scratch with no guided workflow.

## Scope

### In Scope

- **slo-manage**: Skill for creating, updating, pulling, pushing, and deleting SLO definitions (query type selection, YAML template generation, dry-run validation, push)
- **slo-check-status**: Skill for SLO health overview (list all SLOs, status with SLI/budget/burn rate, optional timeline)
- **slo-investigate**: Skill for deep-dive investigation of breaching/alerting SLOs (definition retrieval, raw metric queries, dimensional breakdown, alert rule correlation, runbook fetching)
- **slo-optimize**: Skill for SLO timeline performance analysis and improvement recommendations (trend analysis, degradation pattern detection, objective tuning advisory, alerting sensitivity review)
- **synth-check-status**: Skill for viewing Synthetic Monitoring check health (list, status, timeline)
- **synth-investigate-check**: Skill for diagnosing SM check failures (triage, per-probe breakdown, failure mode classification, PromQL deep-dive)
- **synth-manage-checks**: Skill for creating, updating, pulling, pushing, and deleting SM checks (YAML templates, probe selection, GitOps workflows)
- SKILL.md files with YAML frontmatter, Core Principles, workflow steps, output format templates, and error handling sections
- Reference files for slo-manage (slo-templates.md), slo-investigate (slo-promql-patterns.md), synth-investigate-check (failure-modes.md, sm-promql-patterns.md), and synth-manage-checks (check-types.md)
- Cross-skill negative routing in skill descriptions
- Shared conventions: agent mode awareness, datasource UID resolution, `-o json` for agent processing vs. default table/graph for user display
- **Standardize time range flags on `--from`/`--to`**: Small code change to add `--from`/`--to` to SLO timeline (currently `--start`/`--end`) and SM timeline. All timeline commands also support `--window` as a convenience shorthand (`--window 1h` = `--from now-1h --to now`). SLO timeline gains `--window` (new), SM timeline keeps `--window` (existing). Old `--start`/`--end` flags remain as deprecated aliases.

### Out of Scope

- **New grafanactl CLI commands beyond flag standardization**: This spec covers agent skills (SKILL.md files) and the flag standardization code change, not other Go code changes. The investigation log identifies gaps (e.g., `slo status <uid>` shortcut, SLO-to-alert-rule mapping) but implementing those commands is separate work.
- **SLO Reports management skill**: Reports (`grafanactl slo reports`) are a secondary resource type. The slo-check-status skill covers report status as a supporting step, but a dedicated reports management skill is deferred.
- **Probe management skill**: `grafanactl synth probes list` is a supporting command embedded within other skills, not a standalone workflow.
- **Multi-HTTP, Browser, and Scripted check type YAML examples**: The check-types.md reference file covers HTTP, Ping, DNS, TCP, and Traceroute. Complex check types (MultiHTTP, Browser, Scripted) have undocumented `settings` map structures and are deferred.
- **CLAUDE.md modifications**: The existing CLAUDE.md already documents provider architecture. Skills MUST NOT duplicate that content.
- **Automated SLO objective calculation**: The slo-optimize skill provides advisory recommendations, not automated changes. It MUST NOT modify SLO definitions without explicit user approval.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Number of SLO skills | 4 (manage, check-status, investigate, optimize) | One skill per distinct user intent. Different triggers, workflow shapes, and output modes. Matches the 4 SLO use-cases identified by the user. | User feedback |
| Number of SM skills | 3 (status, investigate, manage) | One skill per distinct user intent. Different triggers, workflow shapes (linear vs. decision-branching), and output modes. | Research report Section 2 |
| Skill naming convention | `slo-manage`, `slo-check-status`, `slo-investigate`, `slo-optimize`, `synth-check-status`, `synth-investigate-check`, `synth-manage-checks` | `slo-` and `synth-` prefixes group skills by domain. Verb-noun pattern follows existing `grafana-investigate-alert`. | Existing skill naming in `.claude/skills/` |
| Allowed tools for slo-manage | `[grafanactl, Bash, Read, Write, Edit]` | This skill creates and modifies YAML SLO definition files on disk, requiring file tools. | Analogous to synth-manage-checks |
| Allowed tools for slo-check-status | `[grafanactl, Bash]` | Read-only status queries require no file manipulation. | Existing skill patterns |
| Allowed tools for slo-investigate | `[grafanactl, Bash]` | Investigation is read-only: queries, status checks, alert rule inspection. | Investigation log |
| Allowed tools for slo-optimize | `[grafanactl, Bash]` | Optimization analysis is read-only. If the user approves changes, the agent routes to slo-manage. | User feedback |
| Allowed tools for synth-manage-checks | `[grafanactl, Bash, Read, Write, Edit]` | This skill creates and modifies YAML check definition files on disk, requiring file tools. | Research report Section 5 |
| Output format convention | Default (table/graph) for user display, `-o json` for agent processing | Follows established pattern from `grafana-investigate-alert` skill. Agent mode auto-selects JSON but explicit `-o` flags override. | Research report Section 6 |
| slo-investigate workflow source | Investigation log workflow (definition -> raw metrics -> dimension breakdown -> alert rules -> runbook) | Real-world validated workflow, proven with grafanactl commands. | Investigation log |
| Query time flags | Standardize on `--from`/`--to` + `--window` convenience shorthand | All timeline commands support `--from`/`--to` (explicit range) and `--window` (convenience: `--window 1h` = `--from now-1h --to now`). SLO timeline gains both (replacing `--start`/`--end`). SM timeline already has `--window`, gains `--from`/`--to`. `grafanactl query` already has `--from`/`--to`. | User feedback + codebase analysis |
| SLO push semantics | UUID present = update (if exists on server) or create (if not); UUID absent = always create | Matches the `upsertSLO` logic in `commands.go`. Different from SM checks which use numeric vs. non-numeric metadata.name. | Codebase analysis (`commands.go`) |
| slo-optimize scope | Advisory only — recommend changes, never auto-apply | Optimization suggestions (objective tuning, label additions, alerting sensitivity) require human judgment. Agent routes to slo-manage for execution. | User feedback |
| Failure mode reference file | 8 documented failure modes in references/failure-modes.md | Covers target down, regional/CDN, SSL/TLS, DNS, timeout, content/assertion, private probe infra, rate limiting. | Research report Section 4 |

## Functional Requirements

### Shared Requirements

**FR-001**: Each skill MUST have a SKILL.md file with YAML frontmatter containing `name`, `description`, and `allowed-tools` fields.

**FR-002**: Each skill description MUST include negative routing text directing to sibling skills for adjacent use cases (e.g., "For creating or modifying SLOs, use slo-manage instead", "For investigating a breaching SLO, use slo-investigate instead").

**FR-003**: Each skill MUST include a "Core Principles" section containing: (1) use grafanactl commands, do not call APIs directly; (2) trust the user's expertise; (3) use `-o json` for agent processing, default format for user display; (4) show graphs for time-series data.

**FR-004**: Each skill MUST include an "Error Handling" section with common errors collected at the end of the skill file, not interleaved in workflow steps.

**FR-005**: All skills MUST use `--from`/`--to` as the primary time range flags across all commands (`grafanactl query`, `grafanactl slo definitions timeline`, `grafanactl slo reports timeline`, `grafanactl synth checks timeline`). Skills MAY use `--window` as a convenience shorthand where appropriate (`--window 1h` is equivalent to `--from now-1h --to now`).

**FR-006**: Skills MUST NOT ask the user for the Prometheus datasource UID unless auto-discovery fails. The resolution order is: flag -> config -> auto-discovery via `grafanactl datasources list --type prometheus`.

**FR-007**: Skills MUST NOT explain what grafanactl, SLOs, or Synthetic Monitoring are. The user is assumed to be an experienced operator.

### Flag Standardization (Code Change)

**FR-080**: The SLO definitions timeline command (`internal/providers/slo/definitions/timeline.go`) MUST add `--from` and `--to` flags as the primary time range flags, with `--start` and `--end` retained as deprecated aliases.

**FR-081**: The SLO reports timeline command (`internal/providers/slo/reports/timeline.go`) MUST add `--from` and `--to` flags as the primary time range flags, with `--start` and `--end` retained as deprecated aliases.

**FR-082**: The SLO definitions timeline command MUST add a `--window` convenience flag that sets `--from` to `now-<duration>` and `--to` to `now`. When `--window` is provided, `--from` and `--to` MUST NOT also be required.

**FR-083**: The SLO reports timeline command MUST add a `--window` convenience flag with the same semantics as FR-082.

**FR-084**: The SM checks timeline command (`internal/providers/synth/checks/timeline.go` or equivalent) MUST add `--from` and `--to` flags alongside the existing `--window` flag. When `--from`/`--to` are provided, they MUST take precedence over `--window`.

**FR-085**: When both `--window` and `--from`/`--to` are provided to any timeline command, the command MUST return an error indicating the flags are mutually exclusive.

**FR-086**: The deprecated `--start`/`--end` flags on SLO timeline commands MUST be hidden from help output but continue to function. They MUST be marked with a deprecation notice in the flag description (e.g., `"Deprecated: use --from instead"`).

**FR-087**: Existing tests for SLO timeline commands using `--start`/`--end` MUST continue to pass. New tests MUST be added for `--from`/`--to` and `--window` flags.

### slo-manage

**FR-010**: The slo-manage skill MUST support 4 workflows: create new SLO definition, update existing SLO definition, GitOps sync (pull/push), and delete SLO definitions.

**FR-011**: For creating SLOs, the skill MUST include a decision table for selecting query type based on the user's description: "percentage of successful requests" -> ratio, "raw PromQL expression" -> freeform, "metric above/below threshold" -> threshold.

**FR-012**: The skill MUST provide YAML templates for each query type (freeform, ratio, threshold) using `apiVersion: slo.ext.grafana.app/v1alpha1` and `kind: SLO` with the correct `spec` structure.

**FR-013**: The skill MUST include a `references/slo-templates.md` file containing per-query-type YAML templates with inline comments explaining each field, including: name, description, query (type-specific), objectives (value as 0-1, window), labels, alerting (fastBurn/slowBurn with annotations), destinationDatasource, and folder.

**FR-014**: The skill MUST use `grafanactl slo definitions push <file> --dry-run` before `grafanactl slo definitions push <file>` for all create and update operations.

**FR-015**: The skill MUST document SLO push semantics: UUID present means upsert (update if exists on server, create if not); UUID absent means always create. After creation, the server assigns a UUID.

**FR-016**: The skill MUST use `grafanactl slo definitions pull -d <dir>` for pulling (writes to `<dir>/SLO/<uuid>.yaml`) and `grafanactl slo definitions delete UUID... [-f]` for deletion.

**FR-017**: The skill MUST include configuration guidance: objective value ranges (0.9 to 0.9999 typical), window options (7d, 14d, 28d, 30d), alerting best practices (fastBurn for pages, slowBurn for tickets), and label conventions.

**FR-018**: The skill MUST resolve the destination datasource UID by running `grafanactl datasources list --type prometheus` if the user does not specify one.

### slo-check-status

**FR-020**: The slo-check-status skill MUST use `grafanactl slo definitions list` to show all SLOs with UUID, name, target, window, and status.

**FR-021**: The skill MUST use `grafanactl slo definitions status` (no UUID) to show SLI, error budget, and health status for all SLOs in a table.

**FR-022**: The skill MUST use `grafanactl slo definitions status <UUID> -o wide` when the user asks about a specific SLO, to include BURN_RATE, SLI_1H, and SLI_1D columns.

**FR-023**: The skill MUST conditionally show timeline when the user asks about trends or when any SLO shows BREACHING status, using `grafanactl slo definitions timeline [UUID] --from <start> --to <end>`.

**FR-024**: The skill MUST interpret status values: OK (SLI >= objective), BREACHING (SLI < objective), NODATA (no Prometheus metrics from recording rules), and lifecycle states (Creating, Updating, Deleting, Error).

**FR-025**: The skill MUST support SLO reports status via `grafanactl slo reports status [UUID]` when the user asks about SLO reports or combined SLO health.

**FR-026**: The skill MUST route to slo-investigate when a specific SLO is BREACHING and the user wants to know why, and route to slo-optimize when the user asks about improvement suggestions.

### slo-investigate

**FR-030**: The slo-investigate skill MUST follow a decision-branching workflow: (1) retrieve SLO definition, (2) check status with `-o wide`, (3) render timeline, (4) extract PromQL queries from definition, (5) run dimensional breakdown queries, (6) search for related alert rules, (7) extract runbook/dashboard URLs from annotations.

**FR-031**: The skill MUST implement early exit: if SLO status is OK, report health metrics and stop. If NODATA, branch to NODATA diagnosis (check destination datasource configuration and recording rule health).

**FR-032**: The skill MUST use `grafanactl slo definitions get <UUID> -o json` to retrieve the full SLO definition including queries, objectives, alerting config, and annotations.

**FR-033**: When investigating a breaching SLO with a ratio query, the skill MUST extract the success/total metric selectors and groupByLabels from the SLO definition and run `grafanactl query -d <datasource> -e '<query>' --from now-1h --to now --step 1m` to identify the error dimension (e.g., by cluster, by status code, by endpoint).

**FR-034**: When investigating a breaching SLO with a freeform query, the skill MUST use the raw PromQL expression from the definition and add dimensional breakdown using `by (<label>)` clauses.

**FR-035**: The skill MUST search for SLO-generated alert rules using `grafanactl alert rules list -o json` filtered by the SLO name pattern (e.g., `jq 'select(.name | test("<slo-name>"; "i"))'`).

**FR-036**: The skill MUST extract runbook and dashboard URLs from SLO annotations and alerting annotations. When a GitHub URL is found and `gh` is available, the skill MUST fetch runbook content using `gh api`.

**FR-037**: The skill MUST classify SLO status into: OK, BREACHING, NODATA, or lifecycle states (Creating, Updating, Deleting, Error).

**FR-038**: The skill MUST include a `references/slo-promql-patterns.md` file containing PromQL patterns for: SLI window metric (`grafana_slo_sli_window`), 1h/1d SLI snapshots (`grafana_slo_sli_1h`, `grafana_slo_sli_1d`), success/total rate metrics (`grafana_slo_success_rate_5m`, `grafana_slo_total_rate_5m`), objective metric (`grafana_slo_objective`), and burn rate computation formula.

**FR-039**: The skill MUST provide a structured output format template for investigation results containing: SLO name, target/window, current SLI, error budget remaining, burn rate, affected dimensions, timeline graph, related alert rule states, runbook link, and recommended next actions.

### slo-optimize

**FR-040**: The slo-optimize skill MUST retrieve the SLO definition and timeline data over a configurable window (default: 28 days, matching common SLO windows).

**FR-041**: The skill MUST use `grafanactl slo definitions timeline <UUID> --from now-28d --to now` to fetch historical SLI trends.

**FR-042**: The skill MUST use `grafanactl slo definitions status <UUID> -o wide` to get current SLI, budget, burn rate, and 1h/1d SLI snapshots.

**FR-043**: The skill MUST analyze timeline data to detect degradation patterns: (a) sustained decline (SLI trending downward over 7+ days), (b) periodic dips (recurring pattern at specific times), (c) sudden drops (step-change in SLI), (d) budget exhaustion rate (projecting when budget will reach 0).

**FR-044**: The skill MUST provide advisory recommendations based on analysis, including: (a) objective tuning (e.g., "current SLI averages 99.2% but objective is 99.9% — consider adjusting to 99.5%"), (b) adding groupByLabels for dimensional visibility, (c) alerting sensitivity adjustments (e.g., "burn rate consistently above 2x — consider adding slowBurn alerts if missing"), (d) window adjustments (e.g., "7d window causes frequent breaches from weekend traffic — consider 28d").

**FR-045**: The skill MUST present recommendations as advisory text with supporting data (current values, projected values, historical comparisons). The skill MUST NOT modify SLO definitions directly.

**FR-046**: The skill MUST route to slo-manage if the user approves a recommendation and wants to apply changes.

**FR-047**: The skill MUST query the destination datasource for the raw SLI metrics (`grafana_slo_sli_window`, `grafana_slo_success_rate_5m`, `grafana_slo_total_rate_5m`) to compute trend statistics when timeline data alone is insufficient.

**FR-048**: The skill MUST check whether alerting is configured on the SLO and, if so, evaluate whether the current burn rate patterns suggest the alerting thresholds are appropriately calibrated.

### synth-check-status

**FR-050**: The synth-check-status skill MUST use `grafanactl synth checks list` to show all checks with ID, JOB, TARGET, and TYPE.

**FR-051**: The skill MUST use `grafanactl synth checks status [ID]` to show SUCCESS%, STATUS (OK/FAILING/NODATA), and PROBES_UP for all or a specific check.

**FR-052**: The skill MUST conditionally show timeline when the user asks about trends or when status shows FAILING, using `grafanactl synth checks timeline <ID> --window <window>`.

**FR-053**: The skill MUST interpret status values: OK (success >= 50%) means healthy; FAILING (success < 50%) means more than half of probes failing and suggests investigation; NODATA means no Prometheus data.

**FR-054**: The skill MUST interpret timeline patterns: flat line at 1.0 means healthy; drops to 0.0 means probe-level failures; intermittent spikes to 0 means flapping.

### synth-investigate-check

**FR-060**: The synth-investigate-check skill MUST follow a decision-branching workflow pattern: (1) identify failing check, (2) get check configuration, (3) triage via timeline, (4) conditional per-probe breakdown, (5) conditional deeper metrics, (6) diagnosis and next actions.

**FR-061**: The skill MUST implement early exit: if check status is OK, report health and stop. If NODATA, branch to NODATA diagnosis (check if enabled, check datasource config).

**FR-062**: The skill MUST analyze timeline data to classify failure scope: all probes failing (target/service issue), subset of probes failing (regional/network issue), intermittent failures (flapping/timeout), recent onset (change-related).

**FR-063**: The skill MUST use `grafanactl synth probes list` to map probe names to regions for geographic failure analysis.

**FR-064**: The skill MUST include a `references/failure-modes.md` file documenting 8 failure modes with columns: Failure Mode, Signals, Likely Cause, Next Action. The 8 modes are: target down, regional/CDN, SSL/TLS, DNS resolution, timeout, content/assertion, private probe infra, rate limiting.

**FR-065**: The skill MUST include a `references/sm-promql-patterns.md` file containing PromQL queries for: success rate over time (`probe_success`), HTTP phase latency breakdown (`probe_http_duration_seconds` by phase), SSL certificate expiry (`probe_ssl_earliest_cert_expiry`), error rates by probe.

**FR-066**: The skill MUST provide a structured output format template containing: check name/target, type, status, success rate, window analyzed, timeline graph, failure pattern classification, affected probes (count and list), onset time/duration, diagnosis text, and numbered next actions.

**FR-067**: When a datasource is available for deeper queries, the skill MUST use `grafanactl query -d <datasource-uid> -e '<promql>' --from <start> --to <end> --step <step>` with patterns from the sm-promql-patterns.md reference.

### synth-manage-checks

**FR-070**: The synth-manage-checks skill MUST support 4 workflows: create new check, update existing checks, GitOps sync (pull/push), and delete checks.

**FR-071**: For creating checks, the skill MUST include a decision table for selecting check type based on target type and test layer: URL -> HTTP, hostname/IP -> Ping, domain -> DNS, host:port -> TCP, URL with routing -> Traceroute.

**FR-072**: The skill MUST use `grafanactl synth probes list` to show available probes and recommend selecting a minimum of 3 geographically distributed probes.

**FR-073**: The skill MUST provide a YAML template for check creation using the Kubernetes-compatible resource model with `apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1`, `kind: Check`, and the correct `spec` structure (job, target, frequency, timeout, enabled, labels, settings, probes, basicMetricsOnly, alertSensitivity).

**FR-074**: The skill MUST use `grafanactl synth checks push <file> --dry-run` before `grafanactl synth checks push <file>` for all create and update operations.

**FR-075**: The skill MUST document that push with numeric `metadata.name` means update, non-numeric means create. After creation, the tool updates the local file with the server-assigned ID.

**FR-076**: The skill MUST use `grafanactl synth checks pull -d <dir>` for pulling and `grafanactl synth checks delete <ID...>` for deletion (`-f` to skip confirmation).

**FR-077**: The skill MUST include configuration guidance: frequency ranges (critical: 10-60s, standard: 1-5min), timeout (must be < frequency), alertSensitivity levels ("high" = >5%, "medium" = >10%, "low" = >25%), basicMetricsOnly tradeoff.

**FR-078**: The skill MUST include a `references/check-types.md` file containing a decision tree for check type selection and per-type YAML examples for at least HTTP, Ping, DNS, TCP, and Traceroute check types.

## Acceptance Criteria

### Shared Structure

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

- GIVEN any skill that references a time-range command (`grafanactl query`, `grafanactl slo definitions timeline`, `grafanactl slo reports timeline`, `grafanactl synth checks timeline`)
  WHEN the command is written
  THEN it MUST use `--from`/`--to` or `--window` flags (not `--start`/`--end`)

- GIVEN the grafanactl codebase after the prerequisite code change
  WHEN `grafanactl slo definitions timeline --from now-7d --to now` is run
  THEN it MUST work identically to the current `--start now-7d --end now` behavior

- GIVEN the grafanactl codebase after the prerequisite code change
  WHEN `grafanactl slo definitions timeline --window 7d` is run
  THEN it MUST be equivalent to `--from now-7d --to now`

- GIVEN the grafanactl codebase after the prerequisite code change
  WHEN `grafanactl synth checks timeline --from now-1h --to now` is run
  THEN it MUST produce the same results as the current `--window 1h` behavior

### Flag Standardization (Code Change)

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

### slo-manage

- GIVEN a user asking "create me an SLO for my API"
  WHEN the slo-manage skill is triggered
  THEN the agent MUST determine query type via the decision table, build a YAML definition from the template, resolve the destination datasource UID, dry-run push, and then push

- GIVEN a user asking to update an existing SLO's objective
  WHEN the slo-manage skill is triggered
  THEN the agent MUST pull the current definition with `grafanactl slo definitions get <UUID>`, modify the requested fields, dry-run push, and then push

- GIVEN a user asking "pull my SLOs"
  WHEN the slo-manage skill is triggered
  THEN the agent MUST run `grafanactl slo definitions pull -d <dir>` and report the output directory

- GIVEN a user asking to delete an SLO
  WHEN the slo-manage skill is triggered
  THEN the agent MUST confirm the SLO identity and run `grafanactl slo definitions delete <UUID> -f`

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
  THEN it MUST list `[grafanactl, Bash, Read, Write, Edit]`

### slo-check-status

- GIVEN a user asking "how are my SLOs doing"
  WHEN the slo-check-status skill is triggered
  THEN the agent MUST run `grafanactl slo definitions status` and present a table of SLO health

- GIVEN a user asking about a specific SLO's status
  WHEN the slo-check-status skill is triggered
  THEN the agent MUST run `grafanactl slo definitions status <UUID> -o wide` to show SLI, budget, burn rate, SLI_1H, and SLI_1D

- GIVEN any SLO with status BREACHING in the status output
  WHEN the status step completes
  THEN the skill MUST show the SLO timeline and suggest using slo-investigate for deeper analysis

- GIVEN a user asking about SLO trends
  WHEN the slo-check-status skill is triggered
  THEN the agent MUST run `grafanactl slo definitions timeline [UUID]` with graph output

- GIVEN an SLO with status NODATA
  WHEN the status is displayed
  THEN the agent MUST note that recording rule metrics are unavailable and suggest checking the destination datasource configuration

### slo-investigate

- GIVEN a user saying "my SLO is breaching, investigate"
  WHEN the slo-investigate skill is triggered
  THEN the agent MUST execute the full investigation workflow: definition retrieval, status with `-o wide`, timeline rendering, PromQL dimensional breakdown, alert rule search, and runbook/dashboard extraction

- GIVEN an SLO with status OK
  WHEN the slo-investigate skill is triggered
  THEN the agent MUST report current health metrics (SLI, budget, burn rate) and stop (early exit)

- GIVEN an SLO with a ratio query and status BREACHING
  WHEN the investigation reaches dimensional breakdown
  THEN the agent MUST extract the success/total metric selectors and groupByLabels from the definition and run `grafanactl query` with those selectors grouped by the relevant dimensions

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

### slo-optimize

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
  THEN the agent MUST suggest adding groupByLabels for dimensional visibility (e.g., cluster, service, endpoint)

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

### synth-check-status

- GIVEN a user asking "are my checks healthy"
  WHEN the synth-check-status skill is triggered
  THEN the agent MUST run `grafanactl synth checks list` followed by `grafanactl synth checks status`

- GIVEN a check with status FAILING
  WHEN the status step completes
  THEN the skill MUST suggest viewing the timeline and mention the synth-investigate-check skill for deeper investigation

- GIVEN a user asking about trends for a specific check
  WHEN the synth-check-status skill is triggered
  THEN the agent MUST run `grafanactl synth checks timeline <ID>` with graph output

### synth-investigate-check

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
  THEN the agent MUST classify this as a "regional/network" failure pattern and map probe names to regions using `grafanactl synth probes list`

- GIVEN the references/failure-modes.md file
  WHEN inspected
  THEN it MUST contain exactly 8 failure modes (target down, regional/CDN, SSL/TLS, DNS resolution, timeout, content/assertion, private probe infra, rate limiting) with Signals, Likely Cause, and Next Action columns

- GIVEN the references/sm-promql-patterns.md file
  WHEN inspected
  THEN it MUST contain PromQL patterns for probe_success rate, HTTP phase latency (probe_http_duration_seconds), SSL cert expiry (probe_ssl_earliest_cert_expiry), and per-probe error rates

### synth-manage-checks

- GIVEN a user asking "create a check for my API"
  WHEN the synth-manage-checks skill is triggered
  THEN the agent MUST determine check type, list probes, build YAML from the template, dry-run push, and then push

- GIVEN a user asking "pull my SM checks"
  WHEN the synth-manage-checks skill is triggered
  THEN the agent MUST run `grafanactl synth checks pull -d <dir>`

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
  THEN it MUST list `[grafanactl, Bash, Read, Write, Edit]` (not just `[grafanactl, Bash]`)

## Negative Constraints

- Skills MUST NOT call Grafana APIs directly (e.g., via `curl` or HTTP libraries). All Grafana interaction MUST go through `grafanactl` commands.

- Skills MUST NOT explain what grafanactl is, what SLOs are, or what Synthetic Monitoring is. The user is an experienced operator.

- Skills MUST NOT ask the user for the Prometheus datasource UID as a first step. The skill MUST attempt auto-resolution first and only ask if auto-discovery fails.

- Skills MUST NOT use `--start`/`--end` flags for time ranges. All time-range commands MUST use `--from`/`--to` or `--window` (after the prerequisite code change adds these aliases).

- Skills MUST NOT interleave error handling within workflow steps. Error handling MUST be in a dedicated section at the end of the skill file.

- The slo-manage skill MUST NOT push SLO definition files without running `--dry-run` first.

- The synth-manage-checks skill MUST NOT push check files without running `--dry-run` first.

- The slo-optimize skill MUST NOT modify SLO definitions directly. It MUST present recommendations as advisory text and route to slo-manage for execution.

- Skills MUST NOT duplicate content from CLAUDE.md (e.g., provider architecture, command tree overview).

- Reference files MUST NOT exceed 200 lines each.

- SKILL.md files MUST NOT exceed 400 lines.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| PromQL queries fail due to missing datasource or metrics | Investigation and optimization skills produce NODATA results, reducing diagnostic value | Skills document graceful degradation: report what is available, suggest datasource configuration. FR-006 ensures auto-resolution is attempted first. |
| Complex check types (MultiHTTP, Browser, Scripted) have undocumented settings | synth-manage-checks cannot generate valid YAML for these types | Explicitly scoped out. check-types.md covers 5 standard types. Skill directs users to `pull` an existing check as a template for complex types. |
| SLO recording rule metrics (`grafana_slo_*`) return no data | slo-check-status and slo-investigate show NODATA for all SLOs | Skill documents this as a known state (recording rules may not be evaluating or may write to a different datasource). Suggests checking destination datasource config. |
| Probe names are case-sensitive; mismatch causes push failure | synth-manage-checks push fails with "probe not found" | Skill documents exact case sensitivity requirement. Recommends running `probes list` first and copy-pasting names. |
| Flag alias code change breaks existing scripts using `--start`/`--end` or `--window` | Users relying on old flag names see failures after update | Old flags remain as deprecated aliases, not removed. Only skills use the new `--from`/`--to` names. |
| Investigation log workflow gaps (no `slo status <uid>` shortcut, no SLO-to-alert mapping) | slo-investigate skill uses workarounds (pattern-matching alert rule names, manual PromQL) | Workarounds are documented in the skill. Future CLI improvements will simplify the workflow. |
| slo-optimize recommendations are generic or unhelpful | User loses trust in optimization suggestions | Recommendations MUST include supporting data (actual metric values, computed statistics). The skill provides advisory text, not automated changes, so the user retains judgment. |
| 7 skills create high maintenance burden | Skills drift out of sync with CLI changes | Each skill is self-contained. Cross-skill routing uses skill names, not shared code. Reference files centralize reusable content (templates, PromQL patterns). |
| SLO push upsert semantics differ from SM check push semantics | Agent uses wrong create/update pattern | FR-015 (SLO: UUID-based upsert) and FR-075 (SM: numeric metadata.name) document the distinct semantics per domain. |

## Open Questions

- [RESOLVED] Whether to standardize time range flags: Yes — all commands will support `--from`/`--to` (explicit range) and `--window` (convenience shorthand). SLO timeline gains `--from`/`--to` and `--window` (replacing `--start`/`--end`). SM timeline keeps `--window`, gains `--from`/`--to`. Old `--start`/`--end` flags remain as deprecated aliases. User approved small code changes for this.

- [RESOLVED] Whether synth-check-status and synth-investigate-check should be merged: Research report (Section 2) demonstrates clear trigger separation and different workflow shapes (linear vs. decision-branching). Keeping them separate.

- [RESOLVED] Whether SLO management should be in scope: User feedback explicitly requests 4 SLO use-cases including creation/management. slo-manage skill added with full CRUD support.

- [RESOLVED] How many SLO skills: User feedback specifies 4 distinct intents (manage, check-status, investigate, optimize). Each has different triggers, workflow shapes, and output requirements.

- [DEFERRED] Whether to add a `slo status <uid>` shortcut command: The investigation log identifies this gap. The slo-investigate skill works around it using `grafanactl slo definitions status <UUID>`. CLI improvement is separate work.

- [DEFERRED] How to handle SLO-to-alert-rule mapping without a dedicated command: The slo-investigate skill uses pattern matching on alert rule names (`grafanactl alert rules list -o json | jq select(.name | test("<slo-name>"))`). A future `grafanactl slo alerts <uid>` command would eliminate this workaround.

- [DEFERRED] Whether MultiHTTP, Browser, and Scripted check type examples should be added to check-types.md: Requires testing with real checks to verify undocumented settings map structures. Will iterate after initial skill delivery.

- [DEFERRED] Whether slo-optimize should support cross-SLO correlation analysis (e.g., detecting that multiple SLOs breach simultaneously due to shared infrastructure): Useful but adds significant complexity. Defer to a future iteration after single-SLO optimization is proven.

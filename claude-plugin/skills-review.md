# Bundled Skills: Best-Practices Review and Benchmark Categorisation

Review of the 22 skills under `claude-plugin/skills/` against [Anthropic's Agent Skills best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices), plus a categorisation of which skills can be measured against [o11y-bench-2.0](https://github.com/grafana/o11y-bench-2.0) scenarios. All fixes described here were applied in the same change set as this document.

## Part 1: Best-practices review

### Method

Each skill (SKILL.md + references) was scored against the best-practices checklist: frontmatter quality (third-person description stating what + when, trigger phrases), conciseness, body under 500 lines, progressive disclosure (references linked one level deep with when-to-read guidance, TOCs on files >100 lines), degrees of freedom matched to task fragility, workflow structure and feedback loops, consistent terminology, concrete examples, no time-sensitive content, one-default-not-a-menu, and explain-why over ALL-CAPS rules. Where command invocations looked suspect they were verified against the CLI reference docs and provider source.

### Systemic findings

These recurred across skills and are the most valuable output of the review:

1. **Factual drift from the real CLI surface** (worst offender: the synth family). Skills asserted flags, columns, thresholds, and output shapes that do not exist: an invented `--dry-run` flag on `synthetic-monitoring checks create/update`, a `-f` shorthand that isn't registered (`--force` only), wrong table column names, a made-up 50% OK/FAILING threshold (actual: alertSensitivity-based 95/90/75%), a YAML labels template in map form when the CheckSpec requires a list of `{name, value}` pairs, and jq pipelines written against a flat JSON shape when the command emits K8s envelopes. Skills need the same drift protection as `docs/reference/cli/` (see follow-ups).
2. **Invalid datasource-UID syntax in query commands** - 32+ occurrences across explore-datasources, investigate-alert, slo-investigate, slo-optimize, and synth-investigate-check used `gcx metrics query <uid> '<expr>'`. The expression is the positional arg; the datasource must be passed with `-d`. Every occurrence would have sent the UID as the PromQL expression. All fixed to `-d <uid>`. Ironically, debug-with-grafana's error-recovery.md documents this exact failure mode.
3. **Broken or missing workflow steps.** import-dashboards jumped from editing Go builders straight to `gcx resources push` (which reads manifests) with no render step; gcx-observability's Phase 5 pointed at the wrong sub-agent label; scaffold-project used the stale `gcx config set server` key form (now `grafana.server`/`grafana.token`).
4. **Oversized monolithic bodies.** gcx-observability (553 lines) and debug-with-grafana (641 lines) exceeded the 500-line limit. Both split: gcx-observability into a 115-line orchestration overview + 3 wave-grouped reference files; debug-with-grafana to 463 lines with example scenarios extracted to a reference.
5. **Orphaned or weakly-linked references.** slo-investigate's slo-promql-patterns.md was never linked from its SKILL.md (it could never be read); several skills referenced files as bare backticks rather than links with when-to-read guidance. Most reference files >100 lines lacked a TOC. All fixed.
6. **When-only descriptions.** Roughly half the descriptions stated when to trigger but not what the skill does. All now lead with a third-person capability statement, preserving the existing trigger phrases and sibling cross-routing.
7. **Time-sensitive content.** Hardcoded version pins ("Go 1.24+", "(v0.0.12)", converter version enumerations already out of date), "fork or future release" hedges for commands that ship today, and roadmap-speculation sections ("Three-Way Merge (Future)"). All removed or made version-agnostic.
8. **Generic tutorial content.** explore-datasources carried a 292-line LogQL tutorial (matcher operators, regex basics) and debug-with-grafana's query-patterns.md carried PromQL/bash basics - things the model already knows. Cut to gcx-specific content only (LogQL reference: 292 -> 84 lines; query-patterns: 654 -> 483).
9. **`allowed-tools` listed a nonexistent `gcx` tool** in 9 skills (`allowed-tools: [gcx, Bash, ...]`). gcx is invoked through Bash, not as a tool. The bogus entry was removed everywhere. Remaining inconsistency (flow-list vs comma-string form) left as is - both parse.

### Per-skill summary

Body line counts exclude nothing; "refs" = files under references/.

| Skill | Before | After | Key issues found and fixed |
|---|---|---|---|
| debug-with-grafana | 641 lines, 3 refs | 463 lines, 4 refs | over 500-line limit (scenarios extracted); generic PromQL/bash content cut from refs; ref-to-ref link removed; stale branch provenance removed; TOCs added |
| investigate-alert | 155 lines, 1 ref | 155 lines | 22 invalid positional-UID query invocations fixed; broken peak-value jq fixed; trigger phrases + oncall-triage cross-ref added to description; TOC added |
| oncall-triage | 136 lines | unchanged | clean pass - dense, verified command surface, good guardrails on bulk mutations |
| setup-gcx | 437 lines, 1 ref | 416 lines | second-person description fixed; duplication with configuration.md removed; Go version pin removed; token-flow example retitled as non-default path |
| gcx | 249 lines | 249 lines | stale "255KB" catalog size claim; duplicated trace commands slimmed |
| gcx-demo | 204 lines | unchanged | clean pass - notably good degrees-of-freedom design |
| gcx-observability | 553 lines | 115 lines, 3 new refs | over 500-line limit (split by execution wave); wrong Phase 5 agent label fixed; description had no trigger phrases; "SM checks" terminology unified |
| scaffold-project | 71 lines | 73 lines | when-only description; Go version pin; stale `config set server`/`token` key form fixed to `grafana.server`/`grafana.token` |
| create-dashboard | 293 lines | 283 lines | two overlapping quality checklists merged |
| manage-dashboards | 153 lines, 2 refs | 153 lines | roadmap-speculation sections removed from resource-model.md; TOCs added; when-to-read guidance added |
| import-dashboards | 105 lines | 109 lines | broken workflow fixed (missing `go run .` render step before push); stale version enumerations removed; manage-dashboards disambiguation added |
| generate-resource-stubs | 128 lines, 1 ref | 128 lines | stale SDK version pin in ref title; 8 one-liner sections collapsed to a table (+ missing piechart entry); TOC added |
| explore-datasources | 232 lines, 2 refs | 229 lines | 8 invalid positional-UID invocations fixed; nonexistent `targets` command reference removed; 292-line generic LogQL tutorial cut to 84 gcx-specific lines; duplication across refs removed |
| diagnose-entity-graph | 298 lines | 298 lines | "fork or future release" hedge for shipping commands removed; version floor removed; source-count claim made drift-proof |
| aio11y | 145 lines, 1 ref | 145 lines | tautological opener cut; ref converted to proper link |
| slo-check-status | 148 lines | 148 lines | when-only description fixed |
| slo-investigate | 193 lines, 1 ref | 195 lines | orphaned reference file now linked; wrong burn-rate maths fixed (14.4x = 2%/hour, not "budget in 2h"); duplicate PromQL block removed; positional-UID invocations fixed; TOC added |
| slo-manage | 197 lines, 1 ref | 197 lines | when-only description fixed; ref link + TOC added |
| slo-optimize | 265 lines | 265 lines | when-only description fixed; positional-UID invocations fixed |
| synth-check-status | 151 lines | 154 lines | wrong column names, made-up 50% threshold, broken NODATA verify command - all fixed against provider source; jq replaced with real `--job` glob flag |
| synth-investigate-check | 215 lines, 2 refs | 187 lines | broken jq name-lookup (wrong JSON shape) replaced; invented threshold fixed; positional-UID invocations fixed; duplication with failure-modes.md removed; TOC added |
| synth-manage-checks | 177 lines, 1 ref | 181 lines | invented `--dry-run` flag removed; labels template fixed to list form; `-f` -> `--force`; wrong metadata.name semantics fixed; frequency-range claim softened; TOC added |

All 22 skills now have bodies under 500 lines, valid frontmatter (verified against the gcx YAML parser and `go test ./internal/skills/... ./cmd/gcx/skills/...`), third-person descriptions with trigger phrases, and references linked one level deep with TOCs where warranted. Skill directory names and `name:` fields are unchanged, so `gcx skills install/update` behaviour is unaffected.

### Known items deliberately not fixed

- Pre-existing em-dashes in untouched prose (minimal-edit principle; new text uses hyphens).
- `metadata.name` semantics in manage-dashboards' resource-model.md ("human-readable name" vs UID-like identifier) - flagged as possibly inaccurate, needs verification against app-platform API semantics.
- Cross-skill alias divergence: gcx skill uses `synthetic-monitoring`, gcx-demo uses the `synth` alias. Both valid; each skill internally consistent.
- Example model names in aio11y (`gpt-4o`, etc.) will age but are format illustrations, not assertions.

## Part 2: o11y-bench-2.0 categorisation

### How the bench grades

Each task is a natural-language statement graded by an LLM rubric; high-weight criteria are anchored to live query facts (PromQL/LogQL/TraceQL executed against the bench's synthetic Grafana stack: Prometheus, Loki, Tempo, plus the Grafana API). Categories and current task counts: dashboarding (20), grafana_api (6), investigation (11), loki_query (10), prometheus_query (16), tempo_query (20).

This means a skill is bench-measurable if its job is answering questions or producing artefacts against metrics, logs, traces, dashboards, or the Grafana API. Skills that drive Cloud-only product APIs (SLO plugin, Synthetic Monitoring, IRM/OnCall, Asserts/KG, AI Observability) cannot run against the bench stack at all.

### Category A: measurable against existing bench scenarios

Wire gcx + the skill into the bench agent container, run each task suite with-skill vs without-skill (and vs no-gcx baseline), compare rubric scores, tokens, and wall-clock.

| Skill | Bench categories | Example scenarios |
|---|---|---|
| debug-with-grafana | investigation, prometheus_query, loki_query, tempo_query | incident-triage, service-degradation-rca, payment-error-blast-radius, find-slow-requests, trace-error-analysis |
| explore-datasources | grafana_api, prometheus_query (discovery), loki_query (discovery) | list-datasources, get-datasource-details, promql-discover-http-metric, traceql-discover-orders-error-attributes |
| gcx (core routing skill) | all six | any task - it routes intent to commands; measure as an always-installed companion to the others |
| create-dashboard | dashboarding | all 20 dashboard-create-*/add-* tasks |
| manage-dashboards | grafana_api, dashboarding (edit tasks) | search-dashboards, inspect-dashboard-queries, audit-service-overview-*, dashboard-add-* |
| investigate-alert (partial) | investigation | scenarios are metric/log-driven rather than alert-driven; usable only if a firing-alert variant is added to the bench |

Suggested measurement protocol per suite: three configurations (no gcx; gcx installed, no skills; gcx + skill), N runs each for variance, compare mean rubric score, pass rate, tokens, duration. The bench's `scenario_time.txt` pinning keeps reruns comparable.

### Category B: not bench-measurable, but objectively script-checkable

Output is files/code/exit codes; build custom evals (e.g. via the skill-creator eval harness) rather than o11y-bench:

| Skill | Objective check |
|---|---|
| import-dashboards | generated Go compiles; rendered manifest round-trips against the source dashboard |
| generate-resource-stubs | stubs compile (`go build`) without modification, as the skill itself claims |
| scaffold-project | scaffold tree exists; `go mod tidy` and render succeed |
| setup-gcx | `gcx config check` exit code; resulting config state via `gcx config view` |
| slo-manage | YAML validates via `slo definitions push --dry-run`; end state via list |
| synth-manage-checks | client-side validation passes; API accepts; end state via checks list/status |
| aio11y | evaluator/rule creation succeeds; `evaluators test` returns a score |
| oncall-triage (mutations) | action-verb result envelopes (`matched == succeeded + skipped + failed`) |

These need a live (or recorded) Grafana Cloud stack with the relevant products enabled - that is the wiring cost, not the grading cost.

### Category C: judgment-based output, needs an LLM judge (human spot-checks)

| Skill | What needs judging | Suggested approach |
|---|---|---|
| slo-check-status / slo-investigate / slo-optimize | investigation narrative, recommendation quality | o11y-bench-style rubrics with query-fact anchors against a Cloud stack with the SLO plugin; the numeric claims (budget, burn rate) are fact-checkable, the advice needs a judge |
| synth-check-status / synth-investigate-check | failure-mode classification + diagnosis narrative | classification is constrained to 8 enumerated modes, so semi-checkable; narrative needs a judge |
| diagnose-entity-graph | 9-point diagnostic report conclusions | per-check verdicts are fact-checkable; overall report needs a judge |
| gcx-observability | end-to-end setup quality across 12 phases | per-phase resources are verifiable by list/get/dry-run (the skill mandates verify-after-create); orchestration quality needs a judge |
| gcx-demo | narrated tour quality (coverage, pacing, adaptation) | human judge; only the read-only invariant is mechanically checkable |
| create-dashboard (visual half) | layout/readability of the result | PNG snapshot + vision-model judge; pipeline half is already objective (validate/push/render exit codes) |

A full human judge is only really needed for gcx-demo and the visual half of create-dashboard; everything else can use fact-anchored LLM rubrics exactly like o11y-bench does, with humans spot-checking rubric quality.

## Follow-ups

1. **Wire Category A into o11y-bench** - gcx binary + `gcx skills install` in the agent container, bench Grafana as the configured context. Compare no-gcx / gcx-only / gcx+skill.
2. **Drift protection for skills** - the dominant failure mode was skills asserting CLI facts that had drifted. Consider a CI check that extracts `gcx ...` invocations from `claude-plugin/skills/**` and validates commands/flags against the command tree (similar to `mise run reference-drift`).
3. **Trigger-accuracy evals** - descriptions were improved by hand; the skill-creator description-optimisation loop (20 should/should-not-trigger queries per skill) would validate the routing between sibling skills (slo-*, synth-* families).
4. **Verify `metadata.name` semantics** in manage-dashboards' resource-model.md.
5. **Alert-driven bench scenario** - investigate-alert has no matching scenario; contributing a firing-alert task to o11y-bench would make it measurable.

# Phases 8-11: Dashboards, Cost Optimization, GitOps, Review

Custom dashboards and cost optimization run in Wave A; GitOps export and the observability review run last (Wave C). See SKILL.md for the wave plan.

## Contents

- [Phase 8: Custom Dashboards](#phase-8-custom-dashboards)
- [Phase 9: Cost Optimization via Adaptive Telemetry](#phase-9-cost-optimization-via-adaptive-telemetry)
- [Phase 10: GitOps Export](#phase-10-gitops-export)
- [Phase 11: Observability Review](#phase-11-observability-review)

---

## Phase 8: Custom Dashboards

Mark task in_progress.

**Pre-check - skip resources that already exist:**
List existing folders and dashboards:
```bash
gcx resources get folders
gcx resources get dashboards
```
If the app folder already exists, capture its UID and skip creation. Skip any dashboard that already exists in the folder by title.

**Step 1 - create folder** (needed before dashboards):
Get an example folder manifest and customize it:
```bash
gcx resources examples Folder
```
Write folder YAML, then push:
```bash
gcx resources push -f folder.yaml --dry-run
gcx resources push -f folder.yaml
```
List folders to confirm and capture the UID.

**Step 2 - parallel: one agent per dashboard** (generate + push simultaneously):

Generate dashboards covering:
- SLO burn rates across all journeys
- Error rate + latency percentiles (p50/p95/p99)
- Request volume + top errors
- Frontend RUM (if configured in Phase 3 - verify with `gcx frontend apps list`)
- k6 load test results

Each agent:
1. Gets a dashboard example: `gcx resources examples Dashboard`
2. Customizes the dashboard JSON with appropriate panels and queries
3. Writes `dashboard-<name>.yaml`
4. Pushes: `gcx resources push -f dashboard-<name>.yaml --dry-run` then `gcx resources push -f dashboard-<name>.yaml`
5. Verifies: `gcx resources get dashboards` filtered by folder UID

Launch all dashboard agents simultaneously. Mark task completed.

---

## Phase 9: Cost Optimization via Adaptive Telemetry

Mark task in_progress.

**Pre-check - skip resources that already exist:**
Discover the adaptive telemetry command groups and list existing rules:
```bash
gcx metrics adaptive --help
gcx logs adaptive --help
gcx traces adaptive --help
```

**All three steps are independent - launch in parallel:**

- **Agent A** - adaptive metrics:
  Discover the adaptive-metrics commands (`gcx metrics adaptive --help`).
  List recommendations, review with user, sync rules if approved, list to confirm they were applied.

- **Agent B** - adaptive logs:
  Discover the adaptive-logs commands (`gcx logs adaptive --help`).
  List patterns and recommendations, create adaptive log rules if beneficial, list to confirm.

- **Agent C** - adaptive traces:
  Discover the adaptive-traces commands (`gcx traces adaptive --help`).
  List recommendations, apply rules if beneficial, list to confirm.

Wait for all three. Report savings estimates and cardinality reduction. Mark task completed.

---

## Phase 10: GitOps Export

Mark task in_progress.

**Pre-check - check if export already exists:**
List files in the export directory (default: `./grafana/`). If the directory exists and contains YAML files, run a dry-run push to check for drift:
```bash
gcx resources push ./grafana/ --dry-run
```
If no drift is detected, the export is up to date - skip and report to the user.

Ask the user where in their repo to place the export (default: `./grafana/`).

**Parallel:**

- **Agent A** - export (run_in_background: true, can be slow):
  Pull all resources to the chosen directory:
  ```bash
  gcx resources pull -p ./grafana/
  ```

- **Agent B** - prepare CI snippet while export runs:
  Generate a ready-to-paste GitHub Actions step or Makefile target that runs:
  ```bash
  gcx resources push ./grafana/ --dry-run
  ```
  This detects drift between the repo and live Grafana.

Wait for Agent A. Then verify round-trip:
```bash
gcx resources push ./grafana/ --dry-run
ls ./grafana/
```

Mark task completed.

---

## Phase 11: Observability Review

Mark task in_progress.

**Step 1 - comprehensive signal health check:**
Run `gcx instrumentation status` and `gcx setup status` for overall health. Report all signal statuses. If any signal is unhealthy, investigate before continuing.

**Step 2 - Validate test definitions against actual signals - parallel:**

- **Agent A** - SLO pass/fail status: list all SLOs (`gcx slo definitions list`) and check reports (`gcx slo reports list`). Flag any SLO already burning error budget - investigate before declaring setup complete.

- **Agent B** - k6 schedule verification: list all k6 schedules (`gcx k6 schedules list`) and cross-reference with k6 load tests (`gcx k6 load-tests list`). Flag any test without a schedule - schedules are required.

- **Agent C** - synthetic check health: list all synthetic checks (`gcx synthetic-monitoring checks list`) and check status for each (`gcx synthetic-monitoring checks status <id>`). Confirm all are enabled and showing recent results.

Wait for all agents. Then synthesize a **prioritized recommendations list**:
- k6 tests missing schedules -> add schedule immediately
- Journeys missing custom spans -> add OTEL SDK instrumentation
- Services with only auto-instrumentation -> add profiling
- Frontend journeys -> add k6 browser test
- SLOs with actual error rates near target -> tighten target or investigate

Mark task completed.

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
Folders don't ship a `resources list-examples` template. Check the server's schema
(`gcx resources list-types folders`) and write a minimal manifest — `metadata.name`
(a stable slug) plus `spec.title` is enough. Then push:
```bash
gcx resources push -p folder.yaml --dry-run
gcx resources push -p folder.yaml
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
1. Starts from the live schema (`gcx resources list-types dashboards`) — dashboards
   don't ship a `resources list-examples` template. Set the target folder via the
   `grafana.app/folder` metadata annotation (the folder UID captured in Step 1).
   The `create-dashboard` skill covers authoring in depth if it is available.
2. Customizes the dashboard spec with appropriate panels and queries
3. Writes `dashboard-<name>.yaml`
4. Pushes: `gcx resources push -p dashboard-<name>.yaml --dry-run` then `gcx resources push -p dashboard-<name>.yaml`
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
  Review recommendations with the user (`gcx metrics adaptive recommendations list`,
  with `diff` to preview changes), sync rules if approved, then confirm they were
  applied.

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

**Pick the managed kind set first.** The GitOps directory is an apply-capable
set, not an archive: it holds only explicitly selected resource kinds that
both pull successfully *and* pass `gcx resources push --dry-run` (read-only
kinds can pull fine yet still fail apply, so "it pulled" is not enough). Use
the same kind selectors for the export, the push preflight, and the drift
re-pull. If the user also wants a full-stack archival snapshot, pull it to a
separate directory — and never run the archival directory through the apply
preflight.

**Pre-check - check if export already exists:**
List files in the export directory (default: `./grafana/`). If it already
contains an export, run the drift check under Agent B instead of re-exporting;
if it shows no differences, the export is up to date - skip and report to the
user.

Ask the user where in their repo to place the export (default: `./grafana/`).

**Parallel:**

- **Agent A** - export (run_in_background: true, can be slow):
  Pull the managed kinds to the chosen directory, pinned to YAML (the default
  output format varies by mode), then confirm the result is apply-capable.
  Pass each kind as its own selector argument — space-separated, never
  comma-joined (`slos checks` below is an illustrative managed set):
  ```bash
  gcx resources pull slos checks -p ./grafana/ -o yaml
  gcx resources push -p ./grafana/ --dry-run
  ```
  If a kind errors on pull or fails the dry-run, move it out of the managed
  set (the archival snapshot is the place for it) and repeat until both steps
  pass cleanly.

- **Agent B** - prepare CI snippet while export runs:
  Generate a ready-to-paste GitHub Actions step or Makefile target with two
  separate steps over the same managed kind selectors:
  ```bash
  # Validation preflight: simulates create/update of the checked-in manifests
  # against the live stack. It catches manifests that no longer apply; it does
  # NOT compare live state against the repo.
  gcx resources push -p ./grafana/ --dry-run

  # Drift check: re-pull the managed kinds — the same selector set as the
  # export, each kind as its own argument — into a fresh, empty temp
  # directory and diff; a reused directory with stale files would show
  # false drift.
  tmp="$(mktemp -d)"
  gcx resources pull slos checks -p "$tmp" -o yaml
  diff -r ./grafana/ "$tmp"
  ```
  Any pull error, skipped kind, or preflight failure makes the check
  incomplete — report it as such, not as drift.

Wait for Agent A. Then report the managed kind set to the user:
```bash
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

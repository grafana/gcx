---
name: gcx-observability
description: >
  (Experimental) End-to-end observability setup for Grafana Cloud. Covers
  instrumentation, SLOs, alerting, synthetic monitoring, k6 load testing,
  IRM on-call, dashboards, cost optimization, and GitOps export.
user-invocable: true
argument-hint: "[phases]"
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent, AskUserQuestion, TaskCreate, TaskUpdate, TaskList, TaskGet
---

You are helping the user implement comprehensive Grafana Cloud observability for their application using a **test-driven** approach. Use `gcx` to automate setup.

**Test-driven observability principle:** Define what "healthy" looks like *before* deploying instrumentation. Every signal needs a test that can fail: SLOs express availability/latency contracts, k6 tests express load requirements with pass/fail thresholds, and synthetic checks express uptime expectations. Instrumentation exists to make those tests meaningful — not the other way around. Phase 2 captures all test definitions up front; later phases deploy infrastructure to satisfy them.

Work interactively — explain each phase, generate YAML using the resource's `example` subcommand as a template, confirm before creating anything, and validate success.

**Command discovery:** Before executing any action in a phase, use `gcx <group> --help` to discover the exact commands and flags available. Use `gcx commands --flat -o json` to see all command groups. Never assume a command's exact syntax — always discover it first. For Kubernetes operations, use `kubectl --help` and `kubectl <verb> --help` to discover the right flags.

**Parallelism rules (follow strictly):**
- Use `TaskCreate` to register every unit of work before starting anything, so the user can see progress.
- Use the `Agent` tool to run independent operations concurrently. Launch multiple agents in a single message whenever their inputs don't depend on each other.
- Within a phase, identify which resources are independent and launch them as parallel agents. Only serialize when there is a true dependency (e.g. a contact point must exist before a notification policy references it).
- Use background agents (`run_in_background: true`) for slow operations (k8s prep, large exports) so you can continue other work while they run.
- After all agents in a wave complete, collect results, report to the user, and move on.

---

## Step 1: Select Phases

If the user passed arguments (`$ARGUMENTS`), use them directly as the selected phases — do not show the menu. `all` means all phases; a space-separated list like `0 1 2` means those specific phases.

Otherwise, show the following menu and ask which phases to run:

```
Grafana Cloud Observability Setup
══════════════════════════════════

  Phase 0   Bootstrap              Verify gcx config + stack auth
  Phase 1   Discovery & Context    Gather app info (clusters, namespaces, journeys)
  Phase 2   Test Definitions       Define SLOs, k6 thresholds, synthetic checks FIRST
  Phase 3   Instrumentation        Alloy collector, setup instrumentation, Faro frontend
  Phase 4   SLO-Based Alerting     Wire alert rules, contact points, policies
  Phase 5   Synthetic Monitoring   Deploy uptime checks (defined in Phase 2)
  Phase 6   k6 Load Testing        Deploy load tests + schedules (defined in Phase 2)
  Phase 7   IRM Setup              Oncall integrations, escalation chains, schedules
  Phase 8   Custom Dashboards      Dashboards via gcx resources push
  Phase 9   Cost Optimization      Adaptive metrics/logs/traces for cardinality control
  Phase 10  GitOps Export          Export all resources as declarative YAML
  Phase 11  Observability Review   Validate signals, find gaps, recommend next steps

Enter phases to run (e.g. "0 1 2" or "all"):
```

Once phases are selected, **immediately create a task for every selected phase** using `TaskCreate` before executing anything. This gives the user a live progress view.

---

## Step 2: Execute Selected Phases

Phases have dependencies:
- Phase 0 must complete before anything else.
- Phase 1 must complete before Phases 2–11 (provides context).
- Phase 2 must complete before Phases 3–6 (test definitions drive instrumentation and alerting).
- Phase 3 should complete before Phase 4 (signals must flow before SLOs are meaningful).
- Phase 4 must complete before Phase 7 (IRM wires into alerting contact points).
- Phases 5, 6, 8, 9 are independent of each other and of Phase 7 — run them in parallel after Phase 3.
- Phase 10 must be last (exports everything created).
- Phase 11 must be last (validates everything).

**Verification principle:** After every create operation, verify the resource exists and is healthy using list or get. Do not mark a phase completed until all resources pass verification. If a resource fails verification, debug before moving on.

**Idempotency principle:** At the start of every phase, check what already exists before creating anything. If a resource with the expected name already exists, skip creation and go straight to verification. If a phase is partially complete, resume from the first missing resource — never re-create resources that are already healthy.

**Recommended parallel execution plan (after Phases 0–3):**

```
Wave A (parallel): Phases 4, 5, 6, 8, 9
Wave B (after Wave A): Phase 7  (needs Phase 4 contact points)
Wave C (after Wave B): Phases 10, 11  (parallel with each other)
```

Launch Wave A agents in a single message. Do not wait for one to finish before starting another.

Within each phase, also parallelize at the resource level (see per-phase instructions below).

---

### Phase 0: Bootstrap

Mark task in_progress. Run sequentially (everything depends on this).

Run `gcx config check` to verify the stack is initialized and authenticated. Then run `gcx config view` to capture the stack URL and context details.

If not configured: ask for the Grafana instance URL and an API token (service account with Admin role), then set up a context:

```bash
gcx config set contexts.<name>.grafana.server <url>
gcx config set contexts.<name>.grafana.token <token>
gcx config use-context <name>
gcx config check
```

Store: stack URL, context name. Mark task completed.

---

### Phase 1: Discovery & Context

Mark task in_progress. Ask the user all questions in a **single `AskUserQuestion` call** (don't ask one at a time):

1. Application name and brief description
2. K8s cluster(s) and namespaces
3. Frontend stack (React / Vue / Angular / vanilla JS / none)
4. Key user journeys (e.g. "login", "checkout", "search") — list them
5. Critical API endpoints for synthetic checks and load tests
6. On-call team structure (names/emails, time zones, escalation order)

Store all answers in memory — every subsequent phase references them. Mark task completed.

---

### Phase 2: Test Definitions

Mark task in_progress.

> **This is the test-driven foundation.** Before any infrastructure is deployed, define the contracts that describe a healthy system. These definitions will be referenced and validated in every subsequent phase.

**Pre-check — skip files that already exist:**
List local files matching `slo-*.yaml`, `k6-test-*.js`, `k6-schedule-*.yaml`, and `check-*.yaml`. For any files already present, skip writing them and use the existing versions in later phases. Only write files that are missing.

Ask the user a **single `AskUserQuestion`** to confirm/adjust these defaults:

- SLO targets per journey (default: 99.9% availability, p95 latency < 500ms over 28d)
- k6 load profile per endpoint (default: 10 VUs, 30s, p95 < 500ms threshold)
- k6 schedule (default: every 6 hours — **schedules are required, not optional**)
- Synthetic check frequency (default: 30s for critical, 60s for standard)
- Synthetic check assertions (default: status=200, latency < 500ms)

**Step 1 — Create SLO definitions (one per journey, parallel):**

For each journey `J` from Phase 1, launch an agent that:
- Discovers the SLO command group (`gcx slo --help`, `gcx slo definitions --help`) to find available subcommands and flags.
- Runs `gcx slo definitions example` to get a template if available, then customizes it: name, availability target, latency target, 28d window.
- Writes the result to `slo-J.yaml`.

Do **not** create the SLOs yet — Phase 4 does that after signals are flowing. Store all `slo-*.yaml` files for Phase 4.

**Step 2 — Create k6 test scripts (one per endpoint, parallel):**

For each critical endpoint from Phase 1, write a `k6-test-<endpoint>.js` script with:
- 10 VUs, 30s duration
- Thresholds: p95 latency < 500ms, failure rate < 1%
- A default function that hits the endpoint and checks status=200 and response time < 500ms

Also discover the k6 schedules command group (`gcx k6 schedules --help`) and run the schedules example subcommand if available. Customize it for a 6-hour frequency and write to `k6-schedule-<endpoint>.yaml`.

Store all scripts and schedule YAMLs for Phase 6.

**Step 3 — Create synthetic check definitions (one per endpoint, parallel):**

For each critical endpoint, discover the synthetic monitoring checks command group (`gcx synth checks --help`) and check for an example subcommand. Customize it: target=real URL, frequency=30s for critical / 60s for standard, assertions: status=200 and latency < 500ms. Do NOT set basicMetricsOnly: true. Write to `check-<endpoint>.yaml`.

Store all `check-*.yaml` files for Phase 5.

Show a summary of all test definitions created. Mark task completed.

---

### Phase 3: Instrumentation

Mark task in_progress.

**Pre-check — skip if already deployed:**
Run `gcx setup instrumentation status` to check current signal status. Also check whether Alloy pods are already running in the monitoring namespace using kubectl (list pods filtered by alloy labels). If Alloy is running and infrastructure signals are healthy, skip Step 1. If app signals are also healthy, skip the rest and mark the phase completed.

**Pre-check — application must be running in Kubernetes first.**

Before deploying Alloy, use kubectl to list deployments, daemonsets, and statefulsets in the application namespace. If no workloads are found, stop and ask the user to deploy their application first — autodiscovery and instrumentation will only detect workloads that exist at the time Alloy runs.

**Pre-check — container image compatibility for observability**

Scan the project's Dockerfiles for patterns that interfere with observability tooling (eBPF, profiling, log collection, debugging). For each Dockerfile found, check for and warn about:

- **Alpine / musl-based runtime**: breaks eBPF uprobe attachment (Beyla), profiling, and SSL inspection. Recommend `debian:bookworm-slim` or `ubuntu:24.04`.
- **Scratch / distroless runtime**: missing shared libraries, shell, and debug tools. Recommend a minimal Debian-based image instead.
- **Stripped binaries**: (`strip`, `-s -w` ldflags) removes symbol tables needed by eBPF and profilers. Recommend keeping symbols.
- **No signal handling**: missing `tini` or equivalent init — can cause zombie processes and missed graceful shutdown signals, leading to metric gaps.

If issues are found, list them and ask the user whether to fix before proceeding.

---

**Step 1 — Deploy Alloy collector (sequential, everything else depends on this):**

Use `gcx fleet collectors --help` to discover collector management commands. Create an Alloy collector configuration:

```bash
gcx fleet collectors create -f alloy-collector.yaml
```

After Alloy is deployed, use kubectl to verify Alloy pods are Running and Ready (not CrashLoopBackOff). Check that a Service exposing port 4317 exists in the monitoring namespace. If not, create one targeting Alloy pods.

Use `gcx fleet pipelines --help` to discover pipeline management. List pipelines (`gcx fleet pipelines list`). If no pipeline contains an OTLP receiver on port 4317, create or update a pipeline to add one:

```bash
gcx fleet pipelines create -f pipeline.yaml
# or
gcx fleet pipelines update <name> -f pipeline.yaml
```

Then wait for infrastructure signals to appear by polling `gcx setup instrumentation status` (timeout 5 minutes). If this times out, debug the Alloy deployment before continuing.

---

**Step 2 — Parallel wave — launch all four agents simultaneously in one message:**

- **Agent A** — Instrumentation discovery + apply:
  Run `gcx setup instrumentation discover --cluster <cluster>` to find instrumentable workloads.
  Then `gcx setup instrumentation show <cluster>` to get the current config as a manifest.
  Enable full observability for discovered workloads in the manifest: tracing, logging, profiling.
  Apply the updated config: `gcx setup instrumentation apply -f config.yaml --dry-run` then `gcx setup instrumentation apply -f config.yaml`.
  Verify with `gcx setup instrumentation status`.

- **Agent B** — Fleet pipelines verification:
  List pipelines (`gcx fleet pipelines list`) to confirm pipeline exists and is receiving data.
  Verify collectors are healthy: `gcx fleet collectors list`.

- **Agent C** — Faro frontend observability (skip if no frontend stack from Phase 1):
  Discover the Faro command group (`gcx faro --help`, `gcx faro apps --help`).
  List existing Faro apps: `gcx faro apps list`. If an app for this project already exists, skip creation.
  Otherwise, create a Faro app configured for the application URL and name:
  ```bash
  gcx faro apps create -f faro-app.yaml
  ```
  Verify: `gcx faro apps list` to confirm the app was created and capture the app ID.
  If the frontend uses sourcemaps, upload them: `gcx faro apps apply-sourcemap <app-name> -f <sourcemap>`.

- **Agent D** — Synthetic checks (early deployment for traffic seeding):
  Deploy the `check-*.yaml` files from Phase 2 now, before instrumentation is fully verified. For each endpoint, check if the check already exists (`gcx synth checks list`); if not, create it: `gcx synth checks create -f check-<endpoint>.yaml`. List checks to confirm each is enabled with probes assigned.
  > **Purpose:** SM checks start probing endpoints immediately, generating real HTTP traffic that flows through Alloy. This seeds the telemetry pipeline so Step 3's signal verification has live data.
  > If endpoints are private, first list available probes (`gcx synth probes list`), identify private probes, and ensure they are online before creating checks.

Wait for all four agents. Report combined results.

> **Note:** For SDK-based instrumentation, use the OTLP endpoint reported by the collector configuration. No additional credentials needed — apps send OTLP to Alloy's in-cluster endpoint.

---

**Step 3 — Verify app signals are flowing after instrumentation:**
Poll `gcx setup instrumentation status` (timeout 5 minutes). SM checks deployed in Step 2 should already be generating traffic, making this verification reliable. If this times out, check that instrumentation was applied correctly and that SM checks are active and targeting the correct endpoints.

Mark task completed.

After Wave A (Phases 4–6, 8–9) completes, do a final signal check using `gcx setup instrumentation status`. If signals are unhealthy, check that app deployments have OTEL instrumentation and are sending to Alloy.

---

### Phase 4: SLO-Based Alerting

Mark task in_progress.

> **Best practice:** Always route alerts from Grafana Alertmanager → Grafana IRM → notification channels (Slack, PagerDuty, email, etc.). Never wire contact points directly to end channels. IRM provides deduplication, grouping, escalation policies, and on-call routing that raw Alertmanager cannot. Phase 7 completes this wiring.

**Pre-check — skip resources that already exist:**
List SLOs (`gcx slo definitions list`), alert rules (`gcx alert rules list`), and alert groups (`gcx alert groups list`). For each journey, skip its SLO and rule group if they already exist by name.

For contact points, notification policies, and mute timings, use the Grafana provisioning API via `gcx api`:
```bash
gcx api /api/v1/provisioning/contact-points
gcx api /api/v1/provisioning/notification-policies
gcx api /api/v1/provisioning/mute-timings
```
Skip creation of any that already exist.

**Step 1 — parallel: one agent per user journey**, using the `slo-J.yaml` files from Phase 2:

For each journey `J`, launch an agent that:
- Creates the SLO: `gcx slo definitions push slo-J.yaml --dry-run` then `gcx slo definitions push slo-J.yaml`. List SLOs to confirm.
- Creates burn-rate alert rules as K8s resources. Get an example: `gcx resources examples AlertRule`. Build 1h/6h/24h burn-rate rules and push them:
  ```bash
  gcx resources push -f alert-rules-J.yaml --dry-run
  gcx resources push -f alert-rules-J.yaml
  ```
  List rules to confirm: `gcx alert rules list`.

Launch all journey agents simultaneously. Wait for all to complete.

**Step 2 — sequential (depends on journeys existing):**

Create a contact point targeting the IRM integration webhook (to be created in Phase 7). Use the Grafana provisioning API via `gcx api`:

```bash
# Create contact point
gcx api /api/v1/provisioning/contact-points -X POST -d @contact-point.json

# Verify
gcx api /api/v1/provisioning/contact-points

# Create/update notification policy routing SLO alerts to that contact point
gcx api /api/v1/provisioning/notification-policies -X PUT -d @notification-policy.json

# Verify
gcx api /api/v1/provisioning/notification-policies
```

**Step 3 — parallel with Step 2 (independent):**

Create a mute timing via the provisioning API:
```bash
gcx api /api/v1/provisioning/mute-timings -X POST -d @mute-timing.json
gcx api /api/v1/provisioning/mute-timings
```

Launch mute-timings agent at the same time as Step 2. Mark task completed.

---

### Phase 5: Synthetic Monitoring

Mark task in_progress.

> SM checks were deployed early in Phase 3 (Step 2, Agent C) to seed traffic for instrumentation verification. This phase validates that all checks are healthy, producing data, and covers all required check types.

**Verify and complete check coverage — parallel, one agent per endpoint:**

List all existing checks: `gcx synth checks list`.

For each endpoint, launch an agent that:
- Gets the check by name/ID (`gcx synth checks get <name>`) to verify: target field matches the intended endpoint exactly (scheme, host, path), probes list is non-empty, and the check is enabled.
- Checks status: `gcx synth checks status <id>` to confirm the check is producing recent results.
- If the check is missing entirely (e.g. Phase 3 Agent C failed), create it now from `check-<endpoint>.yaml`.

Ensure full check type coverage across all endpoints — not just HTTP. Add any missing check types in parallel:
- DNS checks for critical domain resolution
- TCP checks for database or service port connectivity
- HTTP checks with relevant headers and methods (POST for write endpoints, not just GET)

Ensure full metrics are collected on all checks (do not set `basicMetricsOnly: true`).

Wait for all agents. Mark task completed.

---

### Phase 6: k6 Load Testing

Mark task in_progress.

**Pre-check — skip resources that already exist:**
Discover the k6 command group (`gcx k6 --help`) and list existing projects (`gcx k6 projects list`), tests (`gcx k6 tests list`), and schedules (`gcx k6 schedules list`). If a project with the expected name exists, capture its ID and skip creation. Skip test and schedule creation for any endpoint that already has them.

**Step 1 — parallel:**

- **Agent A** — create k6 project: `gcx k6 projects create -f project.yaml`, then `gcx k6 projects list` to confirm and capture the project ID.

- **Agent B** — confirm test artifacts from Phase 2: verify all `k6-test-<endpoint>.js` scripts and `k6-schedule-<endpoint>.yaml` files exist from Phase 2.

Wait for Agent A (need project ID). Then **parallel — one agent per endpoint:**

Each agent:
- Creates the k6 test from the script file, lists tests to confirm and capture the test ID.
- Updates the schedule YAML with the real test ID, creates the schedule, lists schedules to confirm it references the correct test ID.

> **Schedules are mandatory.** Every load test must run on a recurring schedule so regressions are caught automatically. Use a minimum frequency of every 6 hours.

Mark task completed.

---

### Phase 7: IRM Setup

Mark task in_progress. Requires Phase 4 contact points to exist.

> **This phase completes the alerting -> IRM routing.** The contact point created in Phase 4 will be updated to point to the IRM integration webhook, ensuring all alerts flow through IRM for routing, escalation, and on-call management.

**Pre-check — skip resources that already exist:**
Discover the oncall command group (`gcx oncall --help`) and list integrations (`gcx oncall integrations list`), escalation chains (`gcx oncall escalation-chains list`), schedules (`gcx oncall schedules list`), and routes (`gcx oncall routes list`). Skip creation of any that already exist; capture IDs and webhook URLs from existing resources. Even if the integration already exists, still verify the Phase 4 contact point is pointing to its webhook URL.

**Step 1 — parallel (independent of each other):**

- **Agent A** — integration + escalation chain:
  Discover oncall integration and escalation-chain subcommands (`gcx oncall integrations --help`, `gcx oncall escalation-chains --help`). Get examples if available, customize, create each, list to confirm and capture the integration webhook URL.

- **Agent B** — schedules + shifts:
  Discover oncall schedules and shifts subcommands (`gcx oncall schedules --help`, `gcx oncall shifts --help`). Get examples if available, customize for the on-call team from Phase 1, create each, list to confirm.

Wait for both. Then **Step 2** (needs integration webhook URL from Agent A):

Create a route: `gcx oncall routes create -f route.yaml`, list to confirm. Then update the Phase 4 contact point to use the IRM webhook URL:

```bash
gcx api /api/v1/provisioning/contact-points/<uid> -X PUT -d @contact-point-updated.json
gcx api /api/v1/provisioning/contact-points
```

Verify the webhook URL is correct. Mark task completed.

---

### Phase 8: Custom Dashboards

Mark task in_progress.

**Pre-check — skip resources that already exist:**
List existing folders and dashboards:
```bash
gcx resources get folders
gcx resources get dashboards
```
If the app folder already exists, capture its UID and skip creation. Skip any dashboard that already exists in the folder by title.

**Step 1 — create folder** (needed before dashboards):
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

**Step 2 — parallel: one agent per dashboard** (generate + push simultaneously):

Generate dashboards covering:
- SLO burn rates across all journeys
- Error rate + latency percentiles (p50/p95/p99)
- Request volume + top errors
- Frontend RUM (if Faro was configured in Phase 3 — verify with `gcx faro apps list`)
- k6 load test results

Each agent:
1. Gets a dashboard example: `gcx resources examples Dashboard`
2. Customizes the dashboard JSON with appropriate panels and queries
3. Writes `dashboard-<name>.yaml`
4. Pushes: `gcx resources push -f dashboard-<name>.yaml --dry-run` then `gcx resources push -f dashboard-<name>.yaml`
5. Verifies: `gcx resources get dashboards` filtered by folder UID

Launch all dashboard agents simultaneously. Mark task completed.

---

### Phase 9: Cost Optimization via Adaptive Telemetry

Mark task in_progress.

**Pre-check — skip resources that already exist:**
Discover the adaptive telemetry command groups and list existing rules:
```bash
gcx metrics adaptive --help
gcx logs adaptive --help
gcx traces adaptive --help
```

**All three steps are independent — launch in parallel:**

- **Agent A** — adaptive metrics:
  Discover the adaptive-metrics commands (`gcx metrics adaptive --help`).
  List recommendations, review with user, sync rules if approved, list to confirm they were applied.

- **Agent B** — adaptive logs:
  Discover the adaptive-logs commands (`gcx logs adaptive --help`).
  List patterns and recommendations, create adaptive log rules if beneficial, list to confirm.

- **Agent C** — adaptive traces:
  Discover the adaptive-traces commands (`gcx traces adaptive --help`).
  List recommendations, apply rules if beneficial, list to confirm.

Wait for all three. Report savings estimates and cardinality reduction. Mark task completed.

---

### Phase 10: GitOps Export

Mark task in_progress.

**Pre-check — check if export already exists:**
List files in the export directory (default: `./grafana/`). If the directory exists and contains YAML files, run a dry-run push to check for drift:
```bash
gcx resources push ./grafana/ --dry-run
```
If no drift is detected, the export is up to date — skip and report to the user.

Ask the user where in their repo to place the export (default: `./grafana/`).

**Parallel:**

- **Agent A** — export (run_in_background: true, can be slow):
  Pull all resources to the chosen directory:
  ```bash
  gcx resources pull -d ./grafana/
  ```

- **Agent B** — prepare CI snippet while export runs:
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

### Phase 11: Observability Review

Mark task in_progress.

**Step 1 — comprehensive signal health check:**
Run `gcx setup instrumentation status` and `gcx setup status` for overall health. Report all signal statuses. If any signal is unhealthy, investigate before continuing.

**Step 2 — Validate test definitions against actual signals — parallel:**

- **Agent A** — SLO pass/fail status: list all SLOs (`gcx slo definitions list`) and check reports (`gcx slo reports list`). Flag any SLO already burning error budget — investigate before declaring setup complete.

- **Agent B** — k6 schedule verification: list all k6 schedules (`gcx k6 schedules list`) and cross-reference with k6 tests (`gcx k6 tests list`). Flag any test without a schedule — schedules are required.

- **Agent C** — synthetic check health: list all synthetic checks (`gcx synth checks list`) and check status for each (`gcx synth checks status <id>`). Confirm all are enabled and showing recent results.

Wait for all agents. Then synthesize a **prioritized recommendations list**:
- k6 tests missing schedules -> add schedule immediately
- Journeys missing custom spans -> add OTEL SDK instrumentation
- Services with only auto-instrumentation -> add profiling
- Frontend journeys -> add k6 browser test
- SLOs with actual error rates near target -> tighten target or investigate

Mark task completed.

---

## Final Summary

After all tasks are completed:

1. Call `TaskList` to confirm all tasks are marked completed.
2. Show a summary table:

```
Resource Type              Count   Status
─────────────────────────────────────────
SLOs                         3     ok
Alerting rule groups         4     ok
Contact points               1     ok
Synthetic checks             5     ok
k6 tests                     3     ok
k6 schedules                 3     ok  (every test must have one)
IRM integrations             1     ok
Faro apps                    1     ok  (if frontend stack)
Dashboards                   4     ok
Adaptive metrics rules       N     ok
Adaptive logs rules          N     ok
...
```

3. Show stack URL from `gcx config view`.
4. Next recommended action: commit the export directory and add the CI drift-check step.

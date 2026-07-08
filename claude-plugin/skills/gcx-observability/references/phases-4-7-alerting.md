# Phases 4-7: Alerting, Synthetic Monitoring, k6, IRM

SLO-based alerting, synthetic check coverage, k6 load testing, and IRM on-call setup. Phases 4, 5, and 6 run in parallel (Wave A); Phase 7 runs after Phase 4 (Wave B). See SKILL.md for the wave plan.

## Contents

- [Phase 4: SLO-Based Alerting](#phase-4-slo-based-alerting)
- [Phase 5: Synthetic Monitoring](#phase-5-synthetic-monitoring)
- [Phase 6: k6 Load Testing](#phase-6-k6-load-testing)
- [Phase 7: IRM Setup](#phase-7-irm-setup)

---

## Phase 4: SLO-Based Alerting

Mark task in_progress.

> **Best practice:** Always route alerts from Grafana Alertmanager -> Grafana IRM -> notification channels (Slack, PagerDuty, email, etc.). Never wire contact points directly to end channels. IRM provides deduplication, grouping, escalation policies, and on-call routing that raw Alertmanager cannot. Phase 7 completes this wiring.

**Pre-check - skip resources that already exist:**
List SLOs (`gcx slo definitions list`), alert rules (`gcx alert rules list`), and alert groups (`gcx alert groups list`). For each journey, skip its SLO and rule group if they already exist by name.

For contact points, notification policies, and mute timings, use the Grafana provisioning API via `gcx api`:
```bash
gcx api /api/v1/provisioning/contact-points
gcx api /api/v1/provisioning/notification-policies
gcx api /api/v1/provisioning/mute-timings
```
Skip creation of any that already exist.

**Step 1 - parallel: one agent per user journey**, using the `slo-J.yaml` files from Phase 2:

For each journey `J`, launch an agent that:
- Creates the SLO: `gcx slo definitions push slo-J.yaml --dry-run` then `gcx slo definitions push slo-J.yaml`. List SLOs to confirm.
- Creates burn-rate alert rules as K8s resources. Get an example: `gcx resources examples AlertRule`. Build 1h/6h/24h burn-rate rules and push them:
  ```bash
  gcx resources push -p alert-rules-J.yaml --dry-run
  gcx resources push -p alert-rules-J.yaml
  ```
  List rules to confirm: `gcx alert rules list`.

Launch all journey agents simultaneously. Wait for all to complete.

**Step 2 - sequential (depends on journeys existing):**

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

**Step 3 - parallel with Step 2 (independent):**

Create a mute timing via the provisioning API:
```bash
gcx api /api/v1/provisioning/mute-timings -X POST -d @mute-timing.json
gcx api /api/v1/provisioning/mute-timings
```

Launch mute-timings agent at the same time as Step 2. Mark task completed.

---

## Phase 5: Synthetic Monitoring

Mark task in_progress.

> Synthetic checks were deployed early in Phase 3 (Step 2, Agent D) to seed traffic for instrumentation verification. This phase validates that all checks are healthy, producing data, and covers all required check types.

**Verify and complete check coverage - parallel, one agent per endpoint:**

List all existing checks: `gcx synthetic-monitoring checks list`.

For each endpoint, launch an agent that:
- Gets the check by name/ID (`gcx synthetic-monitoring checks get <name>`) to verify: target field matches the intended endpoint exactly (scheme, host, path), probes list is non-empty, and the check is enabled.
- Checks status: `gcx synthetic-monitoring checks status <id>` to confirm the check is producing recent results.
- If the check is missing entirely (e.g. Phase 3 Agent D failed), create it now from `check-<endpoint>.yaml`.

Ensure full check type coverage across all endpoints - not just HTTP. Add any missing check types in parallel:
- DNS checks for critical domain resolution
- TCP checks for database or service port connectivity
- HTTP checks with relevant headers and methods (POST for write endpoints, not just GET)

Ensure full metrics are collected on all checks (do not set `basicMetricsOnly: true`).

Wait for all agents. Mark task completed.

---

## Phase 6: k6 Load Testing

Mark task in_progress.

**Pre-check - skip resources that already exist:**
Discover the k6 command group (`gcx k6 --help`) and list existing projects (`gcx k6 projects list`), load tests (`gcx k6 load-tests list`), and schedules (`gcx k6 schedules list`). If a project with the expected name exists, capture its ID and skip creation. Skip test and schedule creation for any endpoint that already has them.

**Step 1 - parallel:**

- **Agent A** - create k6 project: `gcx k6 projects create -f project.yaml`, then `gcx k6 projects list` to confirm and capture the project ID.

- **Agent B** - confirm test artifacts from Phase 2: verify all `k6-test-<endpoint>.js` scripts and `k6-schedule-<endpoint>.yaml` files exist from Phase 2.

Wait for Agent A (need project ID). Then **parallel - one agent per endpoint:**

Each agent:
- Creates the k6 test from the script file, lists tests to confirm and capture the test ID.
- Updates the schedule YAML with the real test ID, creates the schedule, lists schedules to confirm it references the correct test ID.

> **Schedules are mandatory.** Every load test must run on a recurring schedule so regressions are caught automatically. Use a minimum frequency of every 6 hours.

Mark task completed.

---

## Phase 7: IRM Setup

Mark task in_progress. Requires Phase 4 contact points to exist.

> **This phase completes the alerting -> IRM routing.** The contact point created in Phase 4 will be updated to point to the IRM integration webhook, ensuring all alerts flow through IRM for routing, escalation, and on-call management.

**Pre-check - skip resources that already exist:**
Discover the oncall command group (`gcx irm oncall --help`) and list integrations (`gcx irm oncall integrations list`), escalation chains (`gcx irm oncall escalation-chains list`), schedules (`gcx irm oncall schedules list`), and routes (`gcx irm oncall routes list`). Skip creation of any that already exist; capture IDs and webhook URLs from existing resources. Even if the integration already exists, still verify the Phase 4 contact point is pointing to its webhook URL.

**Step 1 - parallel (independent of each other):**

- **Agent A** - integration + escalation chain:
  Discover oncall integration and escalation-chain subcommands (`gcx irm oncall integrations --help`, `gcx irm oncall escalation-chains --help`). Get examples if available, customize, create each, list to confirm and capture the integration webhook URL.

- **Agent B** - schedules + shifts:
  Discover oncall schedules and shifts subcommands (`gcx irm oncall schedules --help`, `gcx irm oncall shifts --help`). Get examples if available, customize for the on-call team from Phase 1, create each, list to confirm.

Wait for both. Then **Step 2** (needs integration webhook URL from Agent A):

Create a route via the API: `gcx api /api/v1/routes -X POST -d @route.yaml`, then `gcx irm oncall routes list` to confirm. Then update the Phase 4 contact point to use the IRM webhook URL:

```bash
gcx api /api/v1/provisioning/contact-points/<uid> -X PUT -d @contact-point-updated.json
gcx api /api/v1/provisioning/contact-points
```

Verify the webhook URL is correct. Mark task completed.

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

For contact points, notification policies, and mute timings, use the native alert commands:
```bash
gcx alert contact-points list
gcx alert notification-policies get
gcx alert mute-timings list
```
Skip creation of any that already exist.

**Step 1 - parallel: one agent per user journey**, using the `slo-J.yaml` files from Phase 2:

For each journey `J`, launch an agent that:
- Ensures `slo-J.yaml` enables burn-rate alerting in the SLO definition itself
  (the SLO spec's `alerting` section with fast-burn/slow-burn rules) — the SLO
  plugin then generates and manages the burn-rate alert rules server-side.
  Do not hand-author AlertRule manifests: there is no
  `gcx resources examples AlertRule` template, and gcx's alert provider is
  read-only for rules, so `gcx resources push` cannot create them.
- Creates the SLO: `gcx slo definitions push slo-J.yaml --dry-run` then `gcx slo definitions push slo-J.yaml`. List SLOs to confirm.
- Confirms the generated burn-rate rules appeared: `gcx alert rules list`.

Launch all journey agents simultaneously. Wait for all to complete.

**Step 2 - sequential (depends on journeys existing):**

Create a contact point targeting the IRM integration webhook (to be created in Phase 7), using the native alert commands:

```bash
# Create contact point (JSON/YAML file, or - for stdin)
gcx alert contact-points create -f contact-point.yaml

# Verify
gcx alert contact-points list

# Route SLO alerts to that contact point. `set` REPLACES the entire policy
# tree, so export the current tree first as a restore point.
gcx alert notification-policies get -o json > notification-policy-backup.json
gcx alert notification-policies set -f notification-policy.yaml --force

# Verify
gcx alert notification-policies get
```

**Step 3 - parallel with Step 2 (independent):**

Create a mute timing with the native command:
```bash
gcx alert mute-timings create -f mute-timing.yaml
gcx alert mute-timings list
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

Ensure full metrics are collected on all checks (do not set `basicMetricsOnly: true`). Trust the read-back, not the write: if `gcx synthetic-monitoring checks get <id> -o json` still shows `basicMetricsOnly: true` after an update, report that full metrics are unavailable for that check instead of claiming success or updating it again.

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

Create the route with the native command (`gcx irm oncall routes --help` for flags): `gcx irm oncall routes create -f route.yaml`, then `gcx irm oncall routes list` to confirm. Then update the Phase 4 contact point to use the IRM webhook URL:

```bash
gcx alert contact-points update <uid> -f contact-point-updated.yaml
gcx alert contact-points list
```

Verify the webhook URL is correct. Mark task completed.

---
name: gcx-observability
description: >
  (Experimental) End-to-end observability setup for Grafana Cloud using gcx.
  Covers instrumentation, SLOs, alerting, synthetic monitoring, k6 load
  testing, IRM on-call, dashboards, cost optimization, and GitOps export.
  Use when the user wants to set up observability for an application from
  scratch or run a full observability rollout - phrases like "set up
  monitoring", "instrument my app", "add observability", or "onboard my
  service to Grafana Cloud".
user-invocable: true
argument-hint: "[phases]"
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, Agent, AskUserQuestion, TaskCreate, TaskUpdate, TaskList, TaskGet
---

You are helping the user implement comprehensive Grafana Cloud observability for their application using a **test-driven** approach. Use `gcx` to automate setup.

**Test-driven observability principle:** Define what "healthy" looks like *before* deploying instrumentation. Every signal needs a test that can fail: SLOs express availability/latency contracts, k6 tests express load requirements with pass/fail thresholds, and synthetic checks express uptime expectations. Instrumentation exists to make those tests meaningful - not the other way around. Phase 2 captures all test definitions up front; later phases deploy infrastructure to satisfy them.

Work interactively - explain each phase, generate YAML using the resource's `example` subcommand as a template, confirm before creating anything, and validate success.

**Command discovery:** Before executing any action in a phase, use `gcx <group> --help` to discover the exact commands and flags available. Use `gcx commands --flat -o json` to see all command groups. Never assume a command's exact syntax - always discover it first. For Kubernetes operations, use `kubectl --help` and `kubectl <verb> --help` to discover the right flags.

**Parallelism rules:**
- Use `TaskCreate` to register every unit of work before starting anything, so the user can see progress.
- Use the `Agent` tool to run independent operations concurrently. Launch multiple agents in a single message whenever their inputs don't depend on each other.
- Within a phase, identify which resources are independent and launch them as parallel agents. Only serialize when there is a true dependency (e.g. a contact point must exist before a notification policy references it).
- Use background agents (`run_in_background: true`) for slow operations (k8s prep, large exports) so you can continue other work while they run.
- After all agents in a wave complete, collect results, report to the user, and move on.

---

## Step 1: Select Phases

If the user passed arguments (`$ARGUMENTS`), use them directly as the selected phases - do not show the menu. `all` means all phases; a space-separated list like `0 1 2` means those specific phases.

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
- Phase 1 must complete before Phases 2-11 (provides context).
- Phase 2 must complete before Phases 3-6 (test definitions drive instrumentation and alerting).
- Phase 3 should complete before Phase 4 (signals must flow before SLOs are meaningful).
- Phase 4 must complete before Phase 7 (IRM wires into alerting contact points).
- Phases 5, 6, 8, 9 are independent of each other and of Phase 7 - run them in parallel after Phase 3.
- Phase 10 must be last (exports everything created).
- Phase 11 must be last (validates everything).

**Verification principle:** After every create operation, verify the resource exists and is healthy using list or get. Do not mark a phase completed until all resources pass verification. If a resource fails verification, debug before moving on.

**Idempotency principle:** At the start of every phase, check what already exists before creating anything. If a resource with the expected name already exists, skip creation and go straight to verification. If a phase is partially complete, resume from the first missing resource - never re-create resources that are already healthy.

**Recommended parallel execution plan (after Phases 0-3):**

```
Wave A (parallel): Phases 4, 5, 6, 8, 9
Wave B (after Wave A): Phase 7  (needs Phase 4 contact points)
Wave C (after Wave B): Phases 10, 11  (parallel with each other)
```

Launch Wave A agents in a single message. Do not wait for one to finish before starting another.

Within each phase, also parallelize at the resource level (see the per-phase instructions in references/).

---

## Phase Instructions

The detailed per-phase instructions (pre-checks, commands, parallel agent breakdowns, verification steps) live in reference files. Read the file covering a phase before executing it - e.g. read `phases-0-3-foundation.md` at the start, and `phases-4-7-alerting.md` when Wave A begins.

| Phases | Read | Covers |
|--------|------|--------|
| 0-3 | [references/phases-0-3-foundation.md](references/phases-0-3-foundation.md) | Bootstrap, discovery & context, test definitions, instrumentation |
| 4-7 | [references/phases-4-7-alerting.md](references/phases-4-7-alerting.md) | SLO-based alerting, synthetic monitoring, k6 load testing, IRM |
| 8-11 | [references/phases-8-11-finalize.md](references/phases-8-11-finalize.md) | Custom dashboards, cost optimization, GitOps export, review |

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

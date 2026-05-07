---
name: gcx-demo
description: >
  Run a narrated, read-only demo tour of gcx for customer or colleague
  presentations. Showcases the breadth of gcx across every Grafana Cloud
  product area — resources, datasources, metrics, logs, traces, SLOs,
  alerts, synthetic monitoring, IRM, k6, fleet, and more. All commands are
  strictly read-only. Trigger when the user says "demo gcx", "show off gcx",
  "customer demo", "gcx tour", or "/gcx-demo".
user-invocable: true
argument-hint: "[--context <name>]"
allowed-tools: Bash, AskUserQuestion
---

# gcx Demo Tour

Deliver a narrated, read-only showcase of gcx across every Grafana Cloud
product area. All commands are `list`, `get`, `query`, or `status` — nothing
is created, modified, or deleted.

## Principles

- **Read-only only.** Never use `push`, `create`, `update`, `delete`, `edit`,
  or `pull` with local paths. If a command requires write scope, skip it and
  note why.
- **Parallel by default.** Run independent queries in one message. Only
  sequence when a later command needs a previous result.
- **Adapt to the stack.** Discover datasource UIDs, available providers, and
  what resources exist before presenting. Never hardcode UIDs.
- **Narrate.** After each section, explain what was shown and why it matters
  to a customer. Lead with value, not syntax.
- **Graceful degradation.** If a command fails (missing cloud.token, missing
  scope, empty result), note it briefly and continue.

---

## Step 0: Resolve Context

Check which stack the demo targets. If `$ARGUMENTS` contains `--context <name>`,
use that context for all commands via the `--context` flag. Otherwise use the
active context.

```bash
gcx config current-context
gcx config check
```

Announce the target stack to the user before running anything else.

---

## Step 1: Provider Landscape

Show all registered providers in one command. This is the opening shot — it
communicates breadth before anything else.

```bash
gcx providers list
```

After output: narrate the count and highlight that each provider maps to a
distinct Grafana Cloud product, with a consistent verb model across all of
them.

---

## Step 2: Parallel Discovery Wave

Run all of the following in a single parallel message — they are independent:

```bash
# K8s-native resources
gcx resources get dashboards -o wide --no-truncate
gcx resources get folders --no-truncate

# Cloud provider resources
gcx datasources list
gcx slo definitions status
gcx synth checks list
gcx synth probes list
gcx alert instances list --state firing
gcx irm oncall schedules list
```

After output, narrate each area:

**Dashboards & Folders**: gcx speaks Grafana's Kubernetes-compatible API
natively. Every dashboard is a K8s resource — listable, pushable, pullable,
validateable. The `AGE` and `URL` columns in `-o wide` make deep linking
trivial. A GitOps pipeline using `gcx resources push` keeps dashboards as code.

**Datasources**: A single `gcx datasources list` shows every connected
datasource — cloud, PDC-tunneled, and third-party. All of them are queryable
directly from the CLI.

**SLOs**: Status, error budget remaining, SLI, and burn rate at a glance.
Scriptable for release gates — fail a deploy if error budget is below a
threshold.

**Synthetic Monitoring**: Browser and HTTP checks running k6 scripts from a
global network of probes. The same probes list shows coverage — regions,
public/private, capabilities.

**Firing Alerts**: Live alert instances filtered by state. Labels, annotations,
and runbook URLs included — pipe to Slack, PagerDuty, or your own tooling.

**OnCall Schedules**: Who's on-call right now, from which schedule. Useful in
runbooks and automation ("page the current on-call").

---

## Step 3: Discover Signal Datasources

Before querying metrics, logs, and traces, resolve the correct datasource UIDs
for this stack. Run in parallel:

```bash
gcx datasources list -o json 2>/dev/null
```

From the output, extract the UID for:
- The primary Prometheus datasource (type `prometheus`, name contains `prom` or
  `grafanacloud-prom`; prefer the one with uid `grafanacloud-prom` if present)
- The primary Loki datasource (type `loki`, name contains `logs` or
  `grafanacloud-logs`; prefer uid `grafanacloud-logs` if present)
- The primary Tempo datasource (type `tempo`, name contains `traces` or
  `grafanacloud-traces`; prefer uid `grafanacloud-traces` if present)

Store these as PROM_UID, LOKI_UID, TEMPO_UID. If a type is absent, skip that
signal section and note it.

---

## Step 4: Live Signal Queries

Run the three signal queries in parallel (substitute actual UIDs from Step 3):

```bash
# Live PromQL — top 5 jobs by active targets
gcx metrics query 'topk(5, count by (job) (up))' -d <PROM_UID>

# Live LogQL — most recent log lines across all services
gcx logs query '{service_name=~".+"}' --limit 5 -d <LOKI_UID>

# Live TraceQL — most recent traces
gcx traces query '{}' --limit 5 -d <TEMPO_UID>
```

After output, narrate:

**Metrics (Prometheus)**: Any PromQL expression, against any Prometheus
datasource on the stack, direct from the terminal. No browser needed. Useful
for ad-hoc debugging, CI health checks, or scripted reporting.

**Logs (Loki)**: Live LogQL. The same queries you write in Explore — available
in scripts, CI pipelines, and agent workflows. Pipe with `--json` for field
selection without `jq`.

**Traces (Tempo)**: Recent distributed traces — TraceQL from the CLI. Use
`--open` to jump into Grafana Explore with the same query, or `--share-link`
to print a shareable URL.

---

## Step 5: Alert Rules Deep Dive

```bash
gcx alert rules list --no-truncate
```

After output, narrate: Alert rules are rich objects — query, state, labels,
annotations, runbook URL, last evaluation time. All of it queryable
programmatically. A single command replaces navigating three UI pages. Filter
by folder, group, or state with flags.

---

## Step 6: Cloud Provider Commands

Run in parallel — these need cloud.token and cloud.stack to be configured.
If they are not, skip gracefully and note it:

```bash
gcx k6 load-tests list --no-truncate
gcx fleet pipelines list --no-truncate
```

After output (if successful), narrate:

**k6 Load Tests**: k6 Cloud load test catalog alongside your Grafana stack.
Trigger runs, list recent results, and correlate load test timing with live
metrics — all from one CLI.

**Fleet Management**: Alloy collector pipelines defined as code, deployed to
thousands of collectors via fleet matching rules. `gcx fleet pipelines list`
shows the live pipeline configurations running on your collector fleet.

If either command failed due to missing cloud credentials, note: "These
commands require a cloud access policy token (`gcx config set cloud.token
<TOKEN>`) and stack slug (`gcx config set cloud.stack <SLUG>`). On a
fully-configured context they work out of the box."

---

## Step 7: Resource Schema Discovery

```bash
gcx resources schemas
```

After output, narrate: gcx knows the full schema of every K8s-native resource
type on the connected stack — not just Grafana's built-in types, but any
installed plugin that exposes K8s resources. `gcx resources examples <Kind>`
produces a ready-to-push template for any of them. This is how agents and
humans alike build resources without guessing at field names.

---

## Step 8: Final Summary

Present a summary table like this (adapt counts to actual output):

```
Area                  Command                               What you saw
────────────────────────────────────────────────────────────────────────────
Providers             gcx providers list                    N registered providers
Dashboards            gcx resources get dashboards -o wide  N dashboards with deep links
Folders               gcx resources get folders             N folders
Datasources           gcx datasources list                  N datasources (N types)
SLOs                  gcx slo definitions status            N SLOs — status + error budget
Synthetic Monitoring  gcx synth checks list                 N checks across N probes
Synthetic Probes      gcx synth probes list                 N global probe locations
Firing Alerts         gcx alert instances list --state firing  N firing right now
Alert Rules           gcx alert rules list                  Full rules with queries + labels
OnCall Schedules      gcx irm oncall schedules list         N schedules — on-call now shown
k6 Load Tests         gcx k6 load-tests list                N load tests
Fleet Pipelines       gcx fleet pipelines list              N Alloy pipeline configs
Live Metrics          gcx metrics query '...'               PromQL against live Prometheus
Live Logs             gcx logs query '...'                  LogQL against live Loki
Live Traces           gcx traces query '{}'                 TraceQL against live Tempo
Resource Schemas      gcx resources schemas                 All K8s resource types + schemas
```

Close with: "One binary, one context, every Grafana Cloud product. All
scriptable, all pipelineable — and with `--dry-run` on mutations for safe
GitOps workflows."

---

## Error Handling

| Situation | Action |
|-----------|--------|
| `config check` fails | Stop. Ask user to run `gcx config check` and fix the context before continuing. |
| Signal datasource not found | Skip that signal section, note which type was missing. |
| `cloud.token` / `cloud.stack` missing | Skip k6 and fleet sections, note what's needed. |
| Auth scope missing (403) | Note the missing scope, skip that command, continue. |
| Empty list (0 resources) | Report "none found" — not an error; continue. |
| Any other command error | Print the error summary, skip the section, continue. |

Never abort the demo for a single command failure. Collect all errors and
surface them at the end.

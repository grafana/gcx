---
name: manage-known-errors
description: >
  Manage known, expected, or accepted Asserts insights — suppress them so
  they stop firing, or raise their thresholds so they only fire above a
  custom limit. Use when the user says an alert is "expected", "known",
  "intentional", "supposed to fire", or "we know about that one";
  when they ask to "silence", "suppress", "ignore", "ack", or "mute"
  an Asserts insight; or when they say a service is "supposed to be slow"
  or otherwise want to raise the bar on an assertion. Trigger phrases
  include: "suppress this assertion", "silence this insight", "ignore
  the X breach on Y", "set a custom threshold for X", "raise the
  threshold for X", "this latency is expected", "this error is known".
  For diagnosing why an insight fires in the first place, use
  diagnose-entity-graph. For checking SLO health (a different system
  altogether), use slo-check-status.
allowed-tools: [gcx, Bash]
---

# Manage Known Asserts Errors

Acknowledge known/expected Asserts insights so they stop creating noise.
Two mechanisms, both shipped with gcx and both reversible.

## Core Principles

1. Use gcx commands exclusively — do not edit Grafana alert rules directly
2. Confirm the insight is *intended* before silencing; if the user is
   uncertain, route to `diagnose-entity-graph` first
3. Use `-o yaml` to inspect a generated config before applying it
4. Suppression and custom-threshold workflows are both reversible —
   keep the resource name predictable so deletion is trivial later

## Decision: Suppress vs. Custom Threshold

| User intent | Mechanism |
|---|---|
| "This will always fire, ignore it forever" | **Suppression** — silences the insight regardless of value |
| "It's fine up to X, only alert above X" | **Custom threshold** — keeps the assertion live but raises the bar |

When in doubt, suppression is faster to apply, custom threshold is
more honest about the system's behaviour. Both can be deleted later.

## Workflow A: Suppress an insight

Use when the user wants to silence the insight permanently regardless
of its value.

### Step 1 — Identify the insight

Find the entity that's firing it and read the labels from the
firing-alert record:

```bash
# Locate the entity (cross-type search if the user didn't name the type).
gcx kg entities list --insight name=<InsightName> -o json --json name,scope,assertion

# Inspect the entity to capture the alert labels.
gcx kg entities inspect --type <Type> --name <Name> --env <Env> --namespace <NS> -o json
```

The `timeLines[].labels[]` field on the inspect response contains
the labels used by the live alert: `alertname`, `job`,
`asserts_request_context`, `asserts_request_type`, etc.

### Step 2 — Build the suppression config

`disabledAlertConfigs` is a list; each entry has a `name` (the
suppression's own identifier) and a `matchLabels` map. Match as
tightly as possible — over-broad suppression silences unrelated
alerts:

```yaml
disabledAlertConfigs:
  - name: known-<service>-<endpoint>-<assertion>
    matchLabels:
      alertname: <ErrorRatioBreach | LatencyAverageBreach | …>
      job: <namespace/service>
      asserts_request_context: <method-and-path-from-the-alert>
```

### Step 3 — Apply

```bash
gcx kg suppressions create -f suppressions.yaml
```

Or pipe inline:

```bash
echo 'disabledAlertConfigs:
  - name: known-foo
    matchLabels:
      alertname: ErrorRatioBreach
      job: namespace/service' | gcx kg suppressions create
```

### Step 4 — Verify

```bash
gcx kg suppressions list
```

The new entry should appear. Resolving the firing alert in Grafana
takes one alert-evaluation cycle (~30 s by default).

### Reverse

```bash
gcx kg suppressions delete <name>
```

## Workflow B: Custom threshold on an insight family

Use when the user wants the assertion to remain live but only fire
above a value they specify.

Asserts assertion rules read a `<rule>:custom_threshold` recording
rule before falling back to the automatic baseline. Pushing a
recording rule with that name overrides the threshold for the
matching `(env, service, job, request_context, ...)` tuple.

### The naming convention

Six standard custom-threshold rules exist on every Asserts stack:

```
asserts:error:ratio:custom_threshold
asserts:request:rate:custom_threshold
asserts:request:cardinality:custom_threshold
asserts:latency:average:custom_threshold
asserts:latency:p95:custom_threshold
asserts:latency:p99:custom_threshold
```

Pick the one matching the assertion family the user wants to tune.
Example: `LatencyP99ErrorBuildup` reads from
`asserts:latency:p99:custom_threshold`.

### Step 1 — Confirm the rule the assertion uses

```bash
# Find the firing alert's rule UID (from kg entities inspect, as above).
gcx alert rules get <UID> -o yaml
```

Read the rule's `expr` field — it shows which `:custom_threshold`
recording rule the alert checks against.

### Step 2 — Draft the recording rule

The standard pattern multiplies the source ratio/latency series by
zero to preserve all labels, then adds the desired threshold:

```yaml
groups:
  - name: custom-thresholds-<service>
    rules:
      - record: asserts:<family>:custom_threshold
        expr: |
          (
            asserts:<family>{
              asserts_env="<env>",
              service="<service>",
              namespace="<namespace>",
              asserts_request_type="inbound",
              asserts_request_context="<METHOD /path>",
              asserts_source="<source-from-the-alert>"
            } * 0
          ) + <threshold-value>
```

For error-ratio assertions, the value is unitless (0–1 + slack —
a value of `2` permanently suppresses since the ratio never exceeds 1).
For latency assertions, the value is the threshold in the same units
as the underlying recording rule (typically seconds for `*:average`,
`*:p95`, `*:p99`).

### Step 3 — Apply

```bash
gcx kg rules create -f custom-thresholds.yaml
```

### Step 4 — Verify

```bash
gcx kg rules list
gcx metrics query 'asserts:<family>:custom_threshold{service="<service>"}' --since 5m
```

The custom-threshold series should appear with the value you set,
and the alert should resolve within one evaluation cycle.

### Reverse

`gcx kg rules` is whole-payload — to remove just one custom
threshold, re-push the rule group without it (or push an empty
group).

## Anti-pattern: do not "disable and replace" the base alert rule

Some assistants will suggest editing or disabling the Asserts-managed
base assertion rule. Do not. Those rules are plugin-managed and will
be overwritten on the next Asserts plugin update — your edits are
silent-loss-prone. The `:custom_threshold` mechanism above is the
supported override path.

## Output Format

For both workflows:

```
Suppress | Custom threshold for: <InsightName> on <service> (<scope>)

Generated config:
  <yaml block>

Applied: gcx kg <suppressions|rules> create
Verification:
  - <listing command output>
  - <metrics query output>

Reverse with: gcx kg <suppressions|rules> delete ...
```

Lead with what changed. Keep the YAML block in the output so the
user can copy it into their GitOps repo if they want it tracked.

## Error Handling

| Error | Action |
|---|---|
| `kg suppressions create` — invalid YAML | Show the parse error; re-emit the YAML with the user's confirmation |
| `kg suppressions list` — empty after create | The push silently no-op'd. Re-check the `-f` flag and YAML structure |
| `kg rules create` — push succeeds but recording rule has no data | Selector likely doesn't match any live series. Verify with `gcx metrics query 'asserts:<family>{...}' --since 5m` using the same labels |
| `kg entities inspect` — no `grafana_rule_uid` in labels | The insight isn't backed by a Grafana-managed rule. Suppression still works on the labels; custom threshold may not apply |
| Auth errors | Check context: `gcx config view` |

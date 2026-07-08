---
name: diagnose-entity-graph
description: >
  Diagnose Entity Graph problems: missing entities, missing edges, disconnected
  clusters, or filtering issues. Use when the user reports that Entity Graph
  doesn't look right, services are missing, edges aren't appearing, or
  environments can't be filtered. Triggers for: "entity graph is empty",
  "services missing from entity graph", "no edges in entity graph",
  "disconnected services", "can't filter entity graph", "entity graph not
  working", "diagnose entity graph", "debug knowledge graph".
---

# Diagnose Entity Graph

Systematic diagnosis of Entity Graph problems using gcx commands. Follow the
steps in order — each step narrows the cause. Be direct and report findings
concisely.

## Start here: read the Orientation block

`gcx kg diagnose` prints an Orientation block above its check table. Read it
first. It tells you which (if any) of the five common Entity Graph scenarios
the run matches:

- "I see no entities at all"
- "Some expected entities are missing"
- "I see entities with no edges"
- "I see disconnected clusters of entities"
- "I can't filter to the entities I want"

When the Orientation block names a matched scenario, follow its `Next:`
commands. The per-step playbook below is the fallback for runs where no
scenario matched or the matched scenario's hints didn't resolve the issue.

## Reading Diagnose Output

Treat `gcx kg diagnose`'s verdicts as authoritative for the queries it
ran. Two non-obvious classifications:

- **`WARN — metric exists … but no series match the requested
  env/namespace scope`** means the metric is flowing on the stack but
  doesn't carry the scoped label value. This is a label-mapping issue
  (asserts_env vs. deployment_environment, etc.), not a missing-data
  issue. Investigate the label pipeline (Step 6) before suggesting the
  user enable new telemetry.
- **`FAIL — no data` (without the WARN above)** means the metric was
  not found, even unscoped — the integration / recording rule is
  genuinely absent.

For ad-hoc PromQL outside `kg diagnose`, apply the same discipline:
re-run the query without the env / namespace filter before concluding
the data is missing.

If a user reports an entity by name, see Step 7's entity-existence
workflow before assuming it exists on this stack.

## Prerequisites

gcx must be installed and configured with a valid context. `gcx kg diagnose`
bundles most of the per-step checks below; the individual commands produce
equivalent results when you need to drill into one check.

```bash
gcx config view
gcx kg status
```

If `kg status` returns an error, use the `setup-gcx` skill first.

## Step 1: Stack Health

```bash
gcx kg status
```

**Check:** `status` must be `"complete"` and `enabled` must be `true`. If not,
the Knowledge Graph hasn't been onboarded — stop here and direct the user to
the Asserts app onboarding flow.

**Shortcut:** `gcx kg diagnose` runs this plus all subsequent checks in parallel.

## Step 2: Entity Counts and Scopes

```bash
gcx kg summary --since 1h
gcx kg meta scopes
```

**Check:** entity counts should be > 0. The `meta scopes` output shows
available `env`, `site`, and `namespace` values.

If scoping to a specific environment, note the exact `env` value — you'll
use it in all subsequent queries.

## Step 3: Source Metrics in Mimir

Check whether the raw telemetry that feeds Entity Graph exists. Raw Tempo
metrics use `deployment_environment`, not `asserts_env`.

Note the label shape difference between the two metrics: `traces_target_info`
describes a single service so it has one `deployment_environment` label;
`traces_service_graph_request_total` describes an edge between two services
and exposes the env on both sides as `client_deployment_environment` and
`server_deployment_environment` — there is no unified `deployment_environment`
label.

```bash
# Service identity (OTel traces)
gcx metrics query 'count(traces_target_info)' --since 1h
gcx metrics query 'count(traces_target_info{deployment_environment="ENV"})' --since 1h

# Call data (inter-service HTTP/gRPC)
gcx metrics query 'count(traces_service_graph_request_total)' --since 1h
# Filter on server side (use client_deployment_environment for outbound view):
gcx metrics query 'count(traces_service_graph_request_total{server_deployment_environment="ENV"})' --since 1h
```

**Interpret:**
- Both have data → traces are flowing. Continue to Step 4.
- Both empty → no OTel traces for this environment. Entities may still exist
  via Prometheus scraping. Continue to Step 4.

For more specific verdicts on this metric pair (Tempo metrics generation
disabled, broken trace context propagation, service-name collision via
self-loop edges), run `gcx kg diagnose --env ENV` and read the check
results — the command encodes the detection logic and emits a targeted
recommendation per case.

## Step 4: Recording Rules

Recording rules convert raw metrics into the `asserts:*` metrics that Entity
Graph consumes. These use `asserts_env`, not `deployment_environment`.

```bash
# Entity discovery (central to how services appear)
gcx metrics query 'count(asserts:mixin_workload_job{asserts_env="ENV"})' --since 1h

# CALLS edges
gcx metrics query 'count(asserts:relation:calls{asserts_env="ENV"})' --since 1h

# Request rate KPI
gcx metrics query 'count(asserts:request:rate5m{asserts_env="ENV"})' --since 1h
```

**Interpret:**
- `asserts:mixin_workload_job` has data but `asserts:relation:calls` doesn't →
  entities are discovered but no edges exist. Continue to Step 5.
- All empty → recording rules aren't producing output. Check Step 6 (labels).
- All have data → pipeline is healthy. For a specific missing service, go to Step 7.

## Step 5: Edge Source Analysis

CALLS edges can come from many sources, not just OTel traces:

| Source | Input Metric | Requires Traces? |
|--------|-------------|-----------------|
| `app_o11y_servicegraph` | `traces_service_graph_request_total` | Yes |
| `springboot` | `http_server_requests_seconds_count` | No |
| `nginx_ingress` | `nginx_ingress_controller_requests` | No |
| `istio` | `istio_requests_total` | No |
| `aws_rds` | CloudWatch RDS metrics | No |
| `aws_dynamodb` | CloudWatch DynamoDB metrics | No |
| `aws_s3` | CloudWatch S3 metrics | No |
| `aws_applicationelb` | CloudWatch ALB metrics | No |
| `azure_flexible_server` | Azure DB metrics | No |
| `kafka_exporter` | Kafka exporter metrics | No |
| `dbo11y_*` | Database observability metrics | No |

```bash
# What edge sources are active on this stack?
gcx metrics labels --label asserts_source

# Check common Prometheus-based sources for a namespace:
gcx metrics query 'count(http_server_requests_seconds_count{namespace="NS"})' --since 1h
gcx metrics query 'count(nginx_ingress_controller_requests{namespace="NS"})' --since 1h
gcx metrics query 'count(istio_requests_total{namespace="NS"})' --since 1h
```

**Critical: Check for the asserts_env gap.** If a source metric exists but has
no `asserts_env` label, the recording rules silently drop it. This is the most
common reason for "metrics present but no edges":

```bash
# For each source that returned data above, check if it has asserts_env:
gcx metrics query 'count(istio_requests_total{asserts_env!=""})' --since 1h
gcx metrics query 'count(http_server_requests_seconds_count{asserts_env!=""})' --since 1h
gcx metrics query 'count(nginx_ingress_controller_requests{asserts_env!=""})' --since 1h
```

If the metric exists but the `asserts_env!=""` query returns "No data", the
Mimir relabeling rules don't cover this source. The fix is to add a relabeling
rule that maps `namespace` or another label to `asserts_env` for this metric.

**Interpret:**
- No edge sources for this environment → edges are expected to be missing.
  Services need tracing or one of the Prometheus-based sources above.
- Edge source exists but missing `asserts_env` → relabeling gap. Recording
  rules require `asserts_env!=""` and will silently ignore this data.
- If services are discovered via JMX (`job` contains `jmx`) → JMX alone
  cannot produce edges. Spring Boot Actuator or OTel tracing is needed.

**Shortcut:** `gcx kg diagnose` detects this gap automatically and warns
when edge source metrics exist but lack `asserts_env`.

**Most common fix:** If metrics have `deployment_environment` but not
`asserts_env`, the Asserts environment mapping is misconfigured. Go to
**Asserts app → Configuration → Connect Environment → Prometheus** and set
the environment label to `deployment_environment`. This tells the Mimir
relabeling pipeline to derive `asserts_env` from `deployment_environment`
on **all** incoming metrics — not just `target_info`.

**If metrics lack both `deployment_environment` AND `asserts_env`:** The
scrape pipeline needs to add `deployment_environment` first. In Alloy, use
`prometheus.relabel` to copy `namespace` (or another label) to
`deployment_environment` before `remote_write`. Then configure the Connect
Environment page as above.

**Alternative path:** Enable OTel tracing to get edges via
`traces_service_graph_request_total` instead. Tempo generates this metric
server-side with `asserts_env` already populated, bypassing the Mimir
relabeling pipeline entirely.

## Step 6: Label Pipeline

The most common issue: `deployment_environment` isn't mapped to `asserts_env`.

```bash
gcx metrics labels --label deployment_environment
gcx metrics labels --label asserts_env
```

**Check:** Every `deployment_environment` value should have a corresponding
`asserts_env` value. If one is missing, the Mimir relabeling rules aren't
configured for that environment.

Extra `asserts_env` values (like AWS account IDs) that don't match any
`deployment_environment` are normal — they come from non-OTel sources.

**Shortcut:** `gcx kg diagnose labels` automates this cross-reference.

## Step 7: Per-Service Investigation

For a specific missing or edge-less service:

```bash
# Find in graph
gcx kg entities query "MATCH (s:Service {name: \"SERVICE\"}) RETURN s" --since 1h

# Check relationships
gcx kg entities query "MATCH (s:Service {name: \"SERVICE\"})-[r]-(other) RETURN s, r, other" --since 1h

# Source metrics
gcx metrics query 'count(traces_service_graph_request_total{client="SERVICE"})' --since 1h
gcx metrics query 'count(traces_service_graph_request_total{server="SERVICE"})' --since 1h

# Recording rule output
gcx metrics query 'count(asserts:relation:calls{service="SERVICE"})' --since 1h
gcx metrics query 'count(asserts:mixin_workload_job{service="SERVICE"})' --since 1h
```

**Interpret:**
- Found via Cypher but no relationships → check source metrics above.
- `server` series exist but `asserts:relation:calls` doesn't → recording rule
  label mismatch (check `asserts_env` and `namespace`).
- Not found via Cypher → check `traces_target_info{service_name="SERVICE"}`.
- Leaf services (queue consumers, processors) correctly have no outgoing edges.

**Shortcut:** `gcx kg diagnose service SERVICE --env ENV` runs all checks and
produces an interpreted diagnosis with suggested next steps. It also
detects two common patterns that present as "missing entities":

- **Service-name collision** (multiple workloads share one `service.name`,
  collapsing into one entity).
- **Env-scope split** (workloads in the same namespace disagree on
  `deployment.environment`, so cross-env calls don't render as edges).

Read the diagnose check's `Recommendation` for the specific fix.

## Producing a Report

Summarize findings as:

1. **Stack health** — KG enabled and complete?
2. **Entity count** — how many for the scoped environment?
3. **Discovery path** — OTel traces, Prometheus scrape, or cloud integration?
4. **Trace data** — do `traces_target_info` and `traces_service_graph_request_total` exist?
5. **Edge data** — does `asserts:relation:calls` exist? Which `asserts_source` values?
6. **Alternative edge sources** — Spring Boot, nginx, Istio, cloud integrations available?
7. **Label mapping** — `deployment_environment` correctly mapped to `asserts_env`?
8. **Conclusion** — expected state or configuration issue?
9. **Recommendations** — what would fix it?

When recommending a fix, set expectations on convergence time. The metrics
the Knowledge Graph reads from (`asserts:*` recording rules, and the
`traces_*` series Tempo generates) are time-series with a query lookback
window — old data with the broken state will keep appearing in queries
for at least 5–15 minutes after the fix is applied. The Entity Graph UI
should fully stabilize on the corrected state within that window.

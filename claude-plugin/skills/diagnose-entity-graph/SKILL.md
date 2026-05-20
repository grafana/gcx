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

## Prerequisites

gcx must be installed (v0.2.14+) and configured with a valid context. If
`gcx kg diagnose` is available (fork or future release), use it as a shortcut
where noted. Otherwise, the individual commands below produce equivalent results.

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
gcx kg health --since 1h
gcx kg meta scopes
```

**Check:** `totalEntities` should be > 0. The `meta scopes` output shows
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

**Filter out phantom edges.** When Tempo's metrics generator sees a SERVER-kind
span with no incoming `traceparent`, it synthesizes a placeholder edge
with `client="user"` and `connection_type="virtual_node"`. These represent
"some external caller" — they are not real service-to-service edges. When
counting inter-service edges for diagnosis, filter them out:

```promql
traces_service_graph_request_total{client!="user"}
# or equivalently
traces_service_graph_request_total{connection_type!="virtual_node"}
```

If the *only* edges in the env have `client="user"`, treat this as a strong
signal that outgoing trace propagation is broken — see Step 4.

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
- Both have data → traces are flowing. Continue to Step 5.
- `traces_target_info` exists but `traces_service_graph_request_total` doesn't
  (with `client!="user"` filter applied) → either Tempo server-side metrics
  generation isn't enabled, or outgoing trace context isn't being propagated
  by your services. Go to Step 4.
- Service-graph contains only `client="user"` edges → broken trace context
  propagation. Go to Step 4.
- Service-graph shows self-loop edges (`client == server`) for a service
  that shouldn't be calling itself → strong indicator of `service.name`
  collision across multiple workloads. Inspect the colliding service in
  Step 8.
- Both empty → no OTel traces for this environment. Entities may still exist
  via Prometheus scraping. Continue to Step 5.

**Shortcut:** `gcx kg diagnose --env ENV` checks all five metrics automatically.

## Step 4: Trace Context Propagation

When `gcx kg diagnose --env <env>` emits a
**`Trace context propagation: FAIL`** check, services are emitting
spans but the calls between them aren't carrying `traceparent`
headers — so Tempo's metrics generator can't link the spans into
edges. Only phantom `client="user"` edges (Tempo's synthetic
"external caller") form.

The diagnose check's `Recommendation` already names the common
causes and per-language remediation hints — read it as the
primary playbook. Supplementary notes for less-common cases:

### When propagation looks broken but auto-instrumentation IS installed

- The application bypasses the instrumented HTTP client (raw socket
  calls, a third-party SDK using its own transport, a non-standard
  client library). Audit outbound network calls in the upstream
  service's code path.
- No `TracerProvider` was registered, so context isn't tracked at
  all. Confirm via `gcx traces query` that spans on the *receiving*
  service have a parent SpanID.

### Convergence after a fix

After the propagation fix lands, expect
`traces_service_graph_request_total{client!="user"}` to populate
within 1–2 minutes; recording-rule output in Step 5 follows ~5–10
minutes after that.

## Step 5: Recording Rules

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
  entities are discovered but no edges exist. Continue to Step 6.
- All empty → recording rules aren't producing output. Check Step 7 (labels).
- All have data → pipeline is healthy. For a specific missing service, go to Step 8.

## Step 6: Edge Source Analysis

CALLS edges can come from 11 sources, not just OTel traces:

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

**Shortcut:** `gcx kg diagnose` now detects this gap automatically and warns
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

## Step 7: Label Pipeline

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

## Step 8: Per-Service Investigation

For a specific missing or edge-less service:

```bash
# Find in graph
gcx kg cypher "MATCH (s:Service {name: \"SERVICE\"}) RETURN s" --since 1h

# Check relationships
gcx kg cypher "MATCH (s:Service {name: \"SERVICE\"})-[r]-(other) RETURN s, r, other" --since 1h

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
produces an interpreted diagnosis with suggested next steps.

### "I expected N services but I see fewer" — service-name collision

When the user reports fewer entities than expected, suspect that
multiple workloads share one `service.name`:

```bash
gcx metrics query 'count by (service_name) (traces_target_info{deployment_environment="ENV"})' --since 1h
```

Fewer rows than expected workloads = collision. The self-loop signal
from Step 3 reinforces it: a `{client="X", server="X"}` entry in
`traces_service_graph_request_total` for a service with no reason to
call itself is cross-service traffic between same-named workloads
collapsing into a self-loop on the merged entity.

**Fix:** set distinct `OTEL_SERVICE_NAME` (or `service.name` via
`OTEL_RESOURCE_ATTRIBUTES`) per workload. Usual culprit is a default
Helm chart value that wasn't templated per release.

### "My graph is split into disconnected clusters" — env-scope split

When the user expects a connected service graph but sees disconnected
clusters, different workloads likely disagree on
`deployment.environment`. Entity Graph scopes by env, so a call from
a service in env A to one in env B doesn't render as a connection.

```bash
gcx kg cypher "MATCH (s:Service {namespace: \"NAMESPACE\"}) RETURN s LIMIT 50" --since 1h
```

If two services in the same k8s namespace have different `scope.env`
values, that's the split.

**Fix:** align `deployment.environment` (via `OTEL_RESOURCE_ATTRIBUTES`)
across all workloads. Old data with the wrong env will age out over
the next 5–15 minutes.

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

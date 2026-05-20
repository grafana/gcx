---
name: diagnose-entity-graph
description: >
  Diagnose Entity Graph and Asserts UI problems: missing entities, missing
  edges, disconnected clusters, filtering issues, and empty UI tabs
  (Kubernetes, CPU, Memory). Use when the user reports that Entity Graph
  doesn't look right, services are missing, edges aren't appearing,
  environments can't be filtered, or specific Asserts panels are blank
  despite the integration being installed. Triggers for: "entity graph is
  empty", "services missing from entity graph", "no edges in entity graph",
  "disconnected services", "can't filter entity graph", "entity graph not
  working", "diagnose entity graph", "debug knowledge graph",
  "Kubernetes tab is empty", "no CPU/memory data in Asserts",
  "Memory tab shows nothing", "saturation chart is blank".
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
- `traces_target_info` exists but `traces_service_graph_request_total` doesn't →
  Tempo server-side metrics generation may not be enabled.
- Both empty → no OTel traces for this environment. Entities may still exist
  via Prometheus scraping. Continue to Step 4.

**Shortcut:** `gcx kg diagnose --env ENV` checks all five metrics automatically.

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

### Service→Database edges: a special case

Service→Database CALLS edges require the calling service to emit
OpenTelemetry database-client spans (so Tempo's service graph
processor can see `db.system` / `db.statement` / `db.operation` on
outbound calls). HTTP auto-instrumentation alone is not enough —
the language-specific DB instrumentation library must be installed
and registered too.

When `gcx kg diagnose` emits a **`DB instrumentation: WARN`** check
(present when a Tempo datasource is available), no service on the
stack is emitting DB-client spans. The check's `Recommendation`
field names the relevant library per language and the Beyla
ROUTES alternative; read it as the primary playbook.

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

### The `image` label is also load-bearing

When `gcx kg diagnose` emits a **`Container label drift: WARN`**
check, cAdvisor `container_*` metrics in the named namespace are
arriving with `image=""`. The downstream `asserts:resource`
recording rule for `cpu:usage` and `memory:usage` filters on
`image!=""` to exclude pause containers, so dropping the `image`
label silently empties the Asserts UI's Kubernetes / CPU / Memory
tabs for that namespace. The check's `Recommendation` field names
the typical source (an Alloy `extraMetricProcessingRules` block
with `labeldrop regex = "image.*"`, often added for cardinality
reduction) and the fix.

A complementary `Resource coverage` check runs alongside it: when
the `asserts:resource` recording rule produces some types but is
missing expected ones (e.g. has `cpu:throttle` but no `cpu:usage`),
the K8s tab's CPU panel will be empty even though the underlying
rule is partially working. The two checks together tell the user
which UI tab is broken (Resource coverage) and why (Container label
drift, or another label-pipeline issue).

## Step 7: Per-Service Investigation

Before investigating *why* a service is broken, confirm it exists.
If a user names an entity that doesn't appear in the graph at all,
that is itself the finding — report it directly, don't fabricate a
diagnosis. When the type is uncertain, use a cross-type Cypher
query so the limit is enforced server-side rather than after a
client-side fan-out:

```bash
gcx kg entities query "MATCH (n) WHERE n.name CONTAINS 'SERVICE' RETURN n LIMIT 10" --since 1h
```

If that query returns no results and
`traces_target_info{service_name="SERVICE"}` is also empty, the
entity is not on this stack. State that conclusion plainly; the
correct next question is whether the user meant a different stack,
env, or spelling.

For a service that *does* exist but appears missing or edge-less:

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
produces an interpreted diagnosis with suggested next steps.

### Split-identity entities

When `gcx kg diagnose` emits a **`Split identity: WARN`** check,
one physical workload has been discovered as two distinct Service
entities — the OTel `service.name` and the k8s workload name
disagree. The OTel-named entity has no k8s metadata; the k8s-named
entity has no trace data. The check's `Recommendation` names the
fix path (align `OTEL_SERVICE_NAME` with the k8s deployment, or
configure Asserts relabel rules to reconcile).

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

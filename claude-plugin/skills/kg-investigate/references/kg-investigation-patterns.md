# KG Investigation Patterns

Detailed reference for `kg-investigate` skill. Contains Cypher patterns, SAAFE scoring, and Asserts metric reference.

---

## Cypher Query Patterns

### Anchor + all neighbours (start here)

```cypher
MATCH (s:Service {name:'api-server'})-[r]-(t) RETURN s,r,t LIMIT 50
```

### Upstream dependency chain

```cypher
MATCH (s:Service {name:'api-server'})-[:ROUTES|CALLS*1..3]->(up) RETURN s,up LIMIT 30
```

### Downstream dependents (who calls us)

```cypher
MATCH (caller)-[:ROUTES|CALLS]->(s:Service {name:'api-server'}) RETURN caller,s LIMIT 30
```

### Service → Pod → Node (infra drill-down)

```cypher
MATCH (s:Service {name:'api-server'})-[:RUNS_ON]->(p:Pod)-[:SCHEDULED_ON]->(n:Node) RETURN s,p,n LIMIT 20
```

### Peer services (same type, same namespace — for comparison)

```cypher
MATCH (s:Service) WHERE s.namespace='asserts' AND s.name <> 'api-server' RETURN s LIMIT 20
```

### Discover relationship types from an entity

```cypher
MATCH (s:Service {name:'api-server'})-[r]->(d) RETURN type(r), labels(d), d.name LIMIT 20
```

### Entities with active insights (health triage)

```cypher
MATCH (s:Service) WHERE s.asserts_active_assertion = true RETURN s LIMIT 50
```

---

## SAAFE Scoring

Score entities by **new** insights only — those whose onset timestamp falls within or just before the incident window. Chronic insights (present throughout the window) indicate pre-existing conditions, not incident causes.

| Category | Score | What it signals |
|---|---|---|
| Failure | +5 | Crashes, restarts, OOM, pod evictions |
| Error | +4 | Application errors, HTTP 5xx, exception rates |
| Saturation | +3 | CPU/memory/queue near limit, resource exhaustion |
| Anomaly | +2 | Statistical deviation from baseline (request rate, latency) |
| Amend | +1 | Deploy, config change, scaling event — correlation only, not causation |

**Scoring tiers:**
- HIGH: score ≥ 5
- MEDIUM: score 2–4
- LOW: score 1 (Amend-only — note but do not prioritize)
- None: score 0 — skip unless it is a confirmed upstream of a HIGH entity

**Amend rule**: A deploy that coincides with an incident is a testable hypothesis, not a confirmed cause. Promote Amend to leading hypothesis only when raw metrics show degradation starting at or after the deploy time.

---

## Asserts Recording Rule Reference

Pre-computed. Do NOT apply `rate()`. Labels come from entity scope + properties.

### Standard label mappings

| Asserts label | Source | Value |
|---|---|---|
| `asserts_env` | `entity.scope.env` | e.g. `ops-eu-south-0` |
| `asserts_site` | `entity.scope.site` | e.g. `eu-south-0` |
| `namespace` | `entity.scope.namespace` | e.g. `asserts` |
| `job` | `entity.properties.job` | e.g. `asserts/api-server` |
| `workload` | `entity.properties.workload` | e.g. `api-server` |

### Metric reference

```promql
# Golden signals
asserts:request:rate5m{asserts_env="...",namespace="...",job="..."}
asserts:error:ratio{asserts_env="...",namespace="...",job="..."}
asserts:latency:average{asserts_env="...",namespace="...",job="..."}
asserts:latency:p99{asserts_env="...",namespace="...",job="..."}

# Resources (by workload, not job)
asserts:resource{asserts_env="...",namespace="...",workload="...",asserts_resource_type="cpu:usage"}
asserts:resource{asserts_env="...",namespace="...",workload="...",asserts_resource_type="memory:usage"}
asserts:resource{asserts_env="...",namespace="...",workload="...",asserts_resource_type="cpu:throttle"}

# Active alerts / amend events
asserts:alerts{asserts_env="...",namespace="...",asserts_alert_category="amend"}
asserts:alerts{asserts_env="...",namespace="...",asserts_alert_category="error"}
```

---

## Log Query Patterns

Without telemetry configs, derive the stream selector from entity properties. Try these selectors in order:

```logql
# 1. By job (most specific for scrape-job-based services)
{job="<scope.namespace>/<entity.name>"}

# 2. By namespace + app label
{namespace="<scope.namespace>",app="<entity.name>"}

# 3. By namespace + container
{namespace="<scope.namespace>",container="<entity.properties.container>"}

# 4. By namespace + workload
{namespace="<scope.namespace>",pod=~"<entity.properties.workload>-.*"}
```

Discover what labels are actually available:
```bash
gcx logs labels <loki-uid> -o json
```

### Log line filters by SAAFE category

```logql
# Error (most common)
{...} |~ "(?i)(error|exception|failed|5[0-9]{2})" | limit 50

# Saturation
{...} |~ "(?i)(timeout|slow|queue full|backpressure|circuit.open|exhausted)" | limit 50

# Failure
{...} |~ "(?i)(crash|restart|oom|out of memory|killed|panic|fatal|terminated)" | limit 50

# Amend (deploy/config changes)
{...} |~ "(?i)(deploy|started version|config reload|update applied)" | limit 50

# Combined for multi-category
{...} |~ "(?i)(error|timeout|exception|failed|crash|oom)" | limit 50
```

---

## Trace Query Patterns

```traceql
# By OTel service name (from entity.properties.otel_service)
{resource.service.name="<otel_service>",resource.namespace="<namespace>"}

# Filter for errors only
{resource.service.name="<otel_service>"} | status = error

# Filter for slow spans
{resource.service.name="<otel_service>"} | duration > 1s
```

---

## Common Investigation Flows

### High error ratio on a service

1. `gcx kg search entities <name>` → confirm entity + scope
2. `gcx kg traverse` anchor + neighbours with `--with-insights` → identify which entities have Error/Failure insights
3. `gcx metrics query` `asserts:error:ratio` for the anchor and highest-scored neighbours
4. Compare error ratio onset time vs Amend insight timestamps
5. `gcx logs query` with error filter on anchor + any upstream with high score
6. Conclusion: identify the upstream that first showed error increase

### Latency spike

1. `gcx kg traverse` anchor + `--with-insights` → check for Saturation/Anomaly insights
2. `gcx metrics query` `asserts:latency:p99` and `asserts:latency:average` on anchor
3. `gcx kg traverse` upstream chain → find which dependency the latency tracks
4. `gcx metrics query` upstream latency to confirm propagation direction
5. `gcx metrics query` `asserts:resource` (cpu, memory) on saturated entities

### Pod/node issue suspected

1. `gcx kg traverse` Service → Pod → Node chain
2. `gcx metrics query` `asserts:resource` on pod's Node (by `workload` or `instance`)
3. `gcx logs query` on the Node or pod with failure/oom filters
4. Check sibling pods on the same node (peer comparison)

### Deploy suspected cause (Amend insight)

1. Confirm deploy time from Amend insight onset
2. `gcx metrics query` `asserts:alerts{asserts_alert_category="amend"}` for exact timestamp
3. `gcx metrics query` error ratio and latency — compare window before vs after deploy time
4. If no degradation at deploy time: rule out deploy, look for other causes
5. If degradation starts at deploy time: promote to leading hypothesis, check image/version in entity properties

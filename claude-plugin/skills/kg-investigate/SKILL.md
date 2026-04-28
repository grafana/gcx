---
name: kg-investigate
description: Investigate service health incidents and alerts using the Grafana Knowledge Graph (Asserts). Use when the user asks to investigate why a service is degraded, what a SAAFE insight means, what is connected to a service, or what is the blast radius of an incident. Trigger on phrases like "investigate api-server", "why is checkout failing", "what's wrong with my service", "explain this alert", "what's connected to X".
allowed-tools: [Bash, gcx]
---

# Knowledge Graph Investigator

KG-first investigation: map topology and health before touching raw signals. For experienced SREs — no hand-holding, no narration between tool calls.

## Core Principles

1. **Topology first** — use `gcx kg traverse` with `--with-insights` to map the blast radius before running metric or log queries. KG gives topology AND health in one call.
2. **SAAFE insights are leads, not facts** — every insight requires raw signal confirmation before appearing in the conclusion.
3. **New vs chronic** — only insights that started near the incident time are likely causes. Ignore chronic signals.
4. **Entity properties are your query labels** — `gcx kg traverse` returns entity properties (`job`, `workload`, `namespace`) and scope (`env`, `site`). Use these directly in metric/log queries. Never guess label values.
5. **KG informs, evidence decides** — if raw signals contradict KG findings, follow the evidence.

---

## Phase 1: Scope, Metadata, and Entity Discovery

### 1a. Load KG metadata (schema + telemetry configs)

```bash
gcx kg metadata -o json
```

This single call returns: entity type schema (types + properties), scope values (env/site/namespace), and telemetry drilldown configs (log/trace/profile). Use the telemetry configs in Phase 3 instead of guessing label mappings — each config entry maps entity properties to datasource labels exactly.

If you only need specific sections: `--schema`, `--scopes`, `--logs`, `--traces`, `--profiles` (default: all).

### 1b. Discover available scopes

```bash
gcx kg scopes list -o json
```

Cross-reference scope values against the user's query. Prefer the most specific match — namespace over env when both could apply. (The `scopes` section from `gcx kg metadata` contains the same data.)

### 1c. Find the anchor entity

```bash
gcx kg search entities <name> -o json
# Narrow by type or scope if needed:
gcx kg search entities <name> --type Service --env <env> -o json
```

Pick the best match. Note its `type`, `name`, and `scope` (env/namespace/site) — you will pass these as scope flags to every subsequent `gcx kg traverse` call. Also note any entity properties (`job`, `workload`, `otel_service`) — cross-reference against the telemetry configs loaded in step 1a to know which datasource labels to use.

---

## Phase 2: Topology Mapping (2–3 traverse calls)

Always pass `--env` and `--namespace` (from entity scope) to scope the graph. Always use `--with-insights` on the first call.

### Call 1 — Anchor + immediate neighbours (with insights)

```bash
gcx kg traverse \
  "MATCH (s:<Type> {name:'<name>'})-[r]-(t) RETURN s,r,t LIMIT 50" \
  --env <env> --namespace <ns> --with-insights -o json
```

This gives you topology AND SAAFE health signals in one query. Extract:
- Entity properties: `job`, `workload`, `otel_service`, `image`, `version`
- Entity scope: `env`, `site`, `namespace`
- Insights per entity: name, severity, category (Saturation/Anomaly/Amend/Failure/Error)

### Call 2 — Upstream dependencies (what the anchor depends on)

```bash
gcx kg traverse \
  "MATCH (s:<Type> {name:'<name>'})-[:ROUTES|CALLS*1..3]->(up) RETURN s,up LIMIT 30" \
  --env <env> --namespace <ns> -o json
```

### Call 3 — Hosting infrastructure (if issue may be infra-related)

```bash
gcx kg traverse \
  "MATCH (s:<Type> {name:'<name>'})-[:RUNS_ON]->(p:Pod)-[:SCHEDULED_ON]->(n:Node) RETURN s,p,n LIMIT 20" \
  --env <env> --namespace <ns> -o json
```

### Discovering relationship types

If unsure which relationship types exist, query the schema first:

```bash
gcx kg traverse \
  "MATCH (s:<Type> {name:'<name>'})-[r]->(d) RETURN type(r), labels(d), d.name LIMIT 20" \
  --env <env> --namespace <ns> -o json
```

### Early exit

If the first traverse returns zero relevant entities, adjust scope and try one more query with broader scope or a different entity type. If the second also returns nothing, the entity may not be in KG — skip to Phase 3 raw signals directly.

### Score the blast radius

After the topology calls, score entities by NEW insights only (started near incident time):

| SAAFE category | Score |
|---|---|
| Failure | +5 |
| Error | +4 |
| Saturation | +3 |
| Anomaly | +2 |
| Amend (deploy/config change) | +1 |

An Amend insight means "something changed" — never treat it as a confirmed cause without raw metric evidence of causation.

Record the scored blast radius and your initial hypotheses before proceeding to raw signals.

---

## Phase 3: Raw Signal Investigation

Work through scored entities highest to lowest. Skip entities with score 0.

### Discover datasources

```bash
gcx datasources list -o json
# Filter by type if needed:
gcx datasources list --type prometheus -o json
gcx datasources list --type loki -o json
```

### Metrics — Asserts recording rules (preferred)

These pre-computed gauges cover the standard SRE golden signals. Do NOT wrap in `rate()`.

Use entity scope for environment filters and entity properties for service selectors:

```bash
# Request rate (req/s)
gcx metrics query <prom-uid> \
  'asserts:request:rate5m{asserts_env="<scope.env>",namespace="<scope.namespace>",job="<prop.job>"}' \
  --since <window> -o json

# Error ratio
gcx metrics query <prom-uid> \
  'asserts:error:ratio{asserts_env="<scope.env>",namespace="<scope.namespace>",job="<prop.job>"}' \
  --since <window> -o json

# Average latency
gcx metrics query <prom-uid> \
  'asserts:latency:average{asserts_env="<scope.env>",namespace="<scope.namespace>",job="<prop.job>"}' \
  --since <window> -o json

# P99 latency
gcx metrics query <prom-uid> \
  'asserts:latency:p99{asserts_env="<scope.env>",namespace="<scope.namespace>",job="<prop.job>"}' \
  --since <window> -o json

# CPU usage
gcx metrics query <prom-uid> \
  'asserts:resource{asserts_env="<scope.env>",namespace="<scope.namespace>",asserts_resource_type="cpu:usage",workload="<prop.workload>"}' \
  --since <window> -o json

# Memory usage
gcx metrics query <prom-uid> \
  'asserts:resource{asserts_env="<scope.env>",namespace="<scope.namespace>",asserts_resource_type="memory:usage",workload="<prop.workload>"}' \
  --since <window> -o json
```

**Amend insights** — when investigating a deploy or config change:

```bash
gcx metrics query <prom-uid> \
  'asserts:alerts{asserts_env="<scope.env>",asserts_alert_category="amend",namespace="<scope.namespace>"}' \
  --since <window> -o json
```

**Metric discovery** — when the Asserts recording rules return no data or you need raw metrics:

```bash
gcx metrics metadata --metric asserts:request -o json
gcx metrics labels <prom-uid> --since 1h -o json
```

### Logs

**With telemetry configs** (from `gcx kg metadata --logs`): find the matching log config by `match` criteria, then build the Loki stream selector from the `entityProperty→logLabel` mapping. Example: if config maps `job → job` and `namespace → namespace`, use `{job="<prop.job>",namespace="<prop.namespace>"}`.

**Without telemetry configs**: derive the Loki stream selector from entity properties. Use `gcx logs labels` to discover what's available:

```bash
# Discover available Loki labels
gcx logs labels <loki-uid> -o json

# Common stream selector patterns — try in order until one returns data:
# For services with job property:
gcx logs query <loki-uid> '{job="<scope.namespace>/<prop.job-basename>"}' --since <window> -o json

# For k8s workloads:
gcx logs query <loki-uid> '{namespace="<scope.namespace>",app="<entity-name>"}' --since <window> -o json

# For k8s containers:
gcx logs query <loki-uid> '{namespace="<scope.namespace>",container="<prop.container>"}' --since <window> -o json
```

Add line filters guided by SAAFE categories from the traverse insights:
- **Saturation**: `|~ "(?i)(timeout|slow|queue|backpressure|exhausted|circuit)"`
- **Anomaly**: `|~ "(?i)(unexpected|anomal|unusual)"`
- **Failure**: `|~ "(?i)(crash|restart|oom|killed|panic|fatal)"`
- **Error**: `|~ "(?i)(error|exception|failed|5[0-9]{2})"`
- **Amend**: `|~ "(?i)(deploy|restart|config|update|version)"`

Combine when multiple categories apply: `|~ "(?i)(timeout|error|exception|failed)"`.

### Traces

**With telemetry configs** (from `gcx kg metadata --traces`): use `entityProperty→traceLabel` mappings to build the TraceQL resource selector. Example: config maps `otel_service → resource.service.name` and `namespace → resource.k8s.namespace.name`, so use `{resource.service.name="<prop.otel_service>",resource.k8s.namespace.name="<scope.namespace>"}`.

**Without telemetry configs** (OTel defaults):
```bash
gcx traces query <tempo-uid> \
  '{resource.service.name="<prop.otel_service>",resource.namespace="<scope.namespace>"}' \
  --since <window> -o json
```

If `otel_service` property is not present, try entity name directly as `service.name`.

### Profiles

**With telemetry configs** (from `gcx kg metadata --profiles`): use `entityProperty→profileLabel` mappings. Example: config maps `namespace → namespace`, `cluster → cluster`.

**Without telemetry configs** (OTel defaults):
```bash
gcx profiles query <pyroscope-uid> \
  '{service_name="<prop.otel_service>",namespace="<scope.namespace>"}' \
  --profile-type cpu --since <window> -o json
```

---

## Phase 4: Convergence

- **Before turn 15**: have hypotheses and be actively testing with raw signals.
- **Before turn 25**: stop opening new investigation branches. Confirm or disprove existing ones.
- **Before turn 30**: write the report even if some hypotheses remain open.

Once you can address all completion checklist items, conclude immediately.

### Completion checklist

Before concluding, confirm you can answer:
- **Timeline**: onset, trigger, and impact timestamps
- **Blast radius**: confirmed affected entities, confirmed unaffected
- **Ranked explanations**: leading explanation with raw signal evidence
- **Propagation**: how it cascaded (if applicable)
- **Ruled out**: hypotheses eliminated with evidence
- **Unresolved**: gaps that limit confidence

Use "leading explanation" not "root cause" unless fully evidenced by raw signals.

---

## Output Format

```
Entity: <Type>/<name> in <env>/<namespace>

Blast radius:
  HIGH (<score>): <entity> — <insight summary>
  MEDIUM (<score>): <entity> — <insight summary>
  Unaffected: <entities>

Timeline:
  <time>: onset — <what changed or first signal>
  <time>: impact — <what downstream broke>
  <time>: [recovery if applicable]

Leading explanation:
  <1-2 sentences with evidence references>

Competitors investigated:
  - <hypothesis>: <disproven/open> — <why>

Unresolved:
  - <gap that limits confidence>

Next actions:
  1. <most specific actionable step>
  2. <follow-up or escalation>
```

---

## Error Handling

- **`gcx kg traverse` returns empty**: try broader scope (drop namespace, keep env) or use `gcx kg search entities` to confirm the entity exists.
- **`gcx kg search entities` returns many results**: narrow with `--type Service` or `--env <env>`.
- **Asserts recording rules return no data**: fall back to raw Prometheus metrics; use `gcx metrics metadata` to discover metric names.
- **`gcx logs query` returns no data**: try alternate stream selector patterns from entity properties; check `gcx logs labels` for available labels.
- **`gcx datasources list` returns no matching type**: the datasource may use a different type string — run without `--type` and scan the full list.

## Reference

For SAAFE scoring detail, Cypher patterns, and Asserts metric reference, see:
- [`references/kg-investigation-patterns.md`](references/kg-investigation-patterns.md)

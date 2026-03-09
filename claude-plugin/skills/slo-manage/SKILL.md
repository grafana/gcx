---
name: slo-manage
description: Use when the user wants to create, update, pull, push, or delete SLO definitions. Trigger on phrases like "create an SLO", "update SLO objective", "push SLO", "pull SLOs", "delete SLO", or "GitOps sync SLOs". For checking SLO health or status, use slo-check-status instead. For investigating a breaching SLO, use slo-investigate instead.
allowed-tools: [grafanactl, Bash, Read, Write, Edit]
---

# SLO Management

Create, update, sync, and delete SLO definitions using grafanactl.

## Core Principles

1. Use grafanactl commands exclusively — do not call Grafana APIs directly
2. Always run `--dry-run` before any push operation; proceed only if dry-run succeeds
3. Trust the user's expertise — skip explanations of SLO concepts
4. Use `-o json` for agent processing; default table/yaml for user display
5. Auto-resolve datasource UIDs; only ask if auto-discovery fails

## Query Type Decision Table

Select query type based on what the user describes:

| User describes | Query type |
|----------------|------------|
| "percentage of successful requests", "success rate", "error rate" | `ratio` |
| "raw PromQL expression", "custom metric formula" | `freeform` |
| "metric above/below threshold", "latency under X ms", "availability percentage" | `threshold` |

## Workflow 1: Create New SLO

### Step 1: Determine query type using the decision table above

### Step 2: Resolve destination datasource UID

```bash
grafanactl datasources list --type prometheus
```

Use the UID from the output. If multiple Prometheus datasources exist, ask the user which to use.

### Step 3: Build YAML from the appropriate template

See `references/slo-templates.md` for complete templates. Key structure:

```yaml
apiVersion: slo.ext.grafana.app/v1alpha1
kind: SLO
metadata:
  name: ""        # leave empty for new SLO (server assigns UUID on create)
spec:
  name: "my-api-availability"
  description: "API availability over 28 days"
  query:
    type: ratio   # freeform | ratio | threshold
    ratio:        # field matches type
      successMetric:
        prometheusMetric: http_requests_total{status!~"5.."}
      totalMetric:
        prometheusMetric: http_requests_total
      groupByLabels: [cluster, service]
  objectives:
    - value: 0.999   # 0.9 to 0.9999 typical range
      window: 28d    # 7d | 14d | 28d | 30d
  destinationDatasource:
    uid: <prometheus-uid>
```

### Step 4: Validate with dry-run, then push

```bash
grafanactl slo definitions push slo.yaml --dry-run
grafanactl slo definitions push slo.yaml
```

**Push semantics:**
- `metadata.name` empty → always creates (server assigns UUID)
- `metadata.name` set to UUID → upsert (updates if exists, creates if not)

After creation, server assigns UUID. Run `grafanactl slo definitions list` to confirm.

## Workflow 2: Update Existing SLO

### Step 1: Get current definition

```bash
grafanactl slo definitions get <UUID> -o yaml > slo.yaml
```

### Step 2: Modify the YAML file

Edit the relevant fields (objective value, query, alerting, etc.).
Do not modify `metadata.name` (UUID) or `readOnly` fields.

### Step 3: Dry-run, then push

```bash
grafanactl slo definitions push slo.yaml --dry-run
grafanactl slo definitions push slo.yaml
```

## Workflow 3: GitOps Sync (Pull/Push)

### Pull all SLOs to disk

```bash
grafanactl slo definitions pull -d ./slos
# Writes to ./slos/SLO/<uuid>.yaml
```

### Push directory of SLOs

```bash
grafanactl slo definitions push ./slos/SLO/*.yaml --dry-run
grafanactl slo definitions push ./slos/SLO/*.yaml
```

## Workflow 4: Delete SLO

### Step 1: Confirm SLO identity

```bash
grafanactl slo definitions list
grafanactl slo definitions get <UUID>
```

Confirm the UUID and name with the user before deletion.

### Step 2: Delete

```bash
grafanactl slo definitions delete <UUID> -f
```

Use `-f` to skip confirmation prompt when running in agent mode.

## Configuration Guidance

**Objective values** (stored as 0–1, displayed as percentage):
- Typical range: 0.9 (90%) to 0.9999 (99.99%)
- Common starting points: 0.99 (99%), 0.999 (99.9%), 0.9999 (99.99%)

**Window options:** `7d`, `14d`, `28d`, `30d`
- 28d is most common; matches many SLO frameworks
- Shorter windows (7d) react faster but have higher variance

**Alerting best practices:**
- `fastBurn`: Pages on-call (high burn rate, short window — catches rapid budget consumption)
- `slowBurn`: Creates tickets (low burn rate, long window — catches gradual degradation)

**Labels:** Use consistent label keys (team, service, environment, tier) for filtering and grouping.

**GroupByLabels** (ratio/threshold queries): Add labels like `cluster`, `service`, `endpoint` for dimensional breakdown in status and investigation.

## Output Format

After create/update:
```
SLO: <name>
UUID: <uuid>
Status: Created | Updated
Objective: <value>% over <window>
Datasource: <uid>
```

After pull:
```
Pulled <N> SLO definitions to <dir>/SLO/
```

After delete:
```
Deleted: <uuid> (<name>)
```

## Error Handling

- **Push fails with 400**: Check YAML structure matches template; verify `destinationDatasource.uid` is valid
- **Push fails with 404 on update**: UUID in `metadata.name` not found; check with `grafanactl slo definitions list`
- **Pull creates empty directory**: No SLOs in this context; check `grafanactl config view` for active context
- **Datasource list returns empty**: No Prometheus datasources configured; ask user for UID manually
- **Dry-run shows unexpected diff**: Show diff to user and ask for confirmation before proceeding
- **Delete fails with 404**: UUID already deleted or wrong UUID; verify with `grafanactl slo definitions list`

# SLO Investigation Log: Git Sync Provisioning Error Rate

**Date**: 2026-03-09
**SLO UID**: `yfxvco8oud0wknbcqs0e8`
**Context**: `ops` (gcx)
**Outcome**: Ignorable — caused by `prod-me-central-1` meltdown

---

## SLO Definition

- **Name**: Git Sync Provisioning: Error Rate
- **Target**: 99.5% success over 28d
- **Query type**: Ratio
  - Success: `mt_app_apiserver_request_total{group="provisioning.grafana.app", namespace="grafana-apps", code!~"5.*"}`
  - Total: `mt_app_apiserver_request_total{group="provisioning.grafana.app", namespace="grafana-apps"}`
- **Group by**: `cluster`
- **Destination datasource**: `grafanacloud-prom`
- **Owner**: `grafana_app_platform_squad`
- **Alerting**: Fast burn (critical) + slow burn (critical)

## Resources from SLO Annotations

- **Runbook**: https://github.com/grafana/deployment_tools/blob/master/docs/grafana-app-platform/runbooks.md#git-sync-provisioning-error-rate
- **Dashboard**: https://ops.grafana-ops.net/goto/af08hy05v4mwwa

## Investigation Steps Taken

### 1. Get SLO definition
```bash
gcx slo definitions get yfxvco8oud0wknbcqs0e8 -o json
```
Gives full SLO spec including queries, objectives, alerting config, annotations.

### 2. Query raw metrics — 5xx by cluster (graph + table)
```bash
# Visual overview
gcx query -d grafanacloud-prom \
  -e 'sort_desc(sum by (cluster) (rate(mt_app_apiserver_request_total{group="provisioning.grafana.app", namespace="grafana-apps", code=~"5.."}[5m])))' \
  --from now-1h --to now --step 1m -o graph

# Status code breakdown
gcx query -d grafanacloud-prom \
  -e 'sum by (code) (rate(mt_app_apiserver_request_total{group="provisioning.grafana.app", namespace="grafana-apps", code=~"5.."}[5m]))' \
  --from now-30m --to now --step 1m -o table
```
Found: 500s and 503s, primarily from `prod-me-central-1`.

### 3. Quantify total 5xx per cluster over 6h
```bash
gcx query -d grafanacloud-prom \
  -e 'sort_desc(sum by (cluster) (increase(mt_app_apiserver_request_total{group="provisioning.grafana.app", namespace="grafana-apps", code=~"5.."}[6h])))' \
  -o table
```
Result: `prod-me-central-1` = 89 errors, `prod-eu-west-0` = 1, all others = 0.

### 4. Find SLO-generated alert rules
```bash
gcx alert rules list -o json | jq '[.[] | select(.name | test("Git Sync Provisioning.*Error"; "i"))]'
```
Found group: "Git Sync Provisioning: Error Rate (Grafana SLO) rules" — 13 recording rules, all `inactive`.

### 5. Fetch runbook
```bash
gh api repos/grafana/deployment_tools/contents/docs/grafana-app-platform/runbooks.md \
  --jq '.content' | base64 -d | sed -n '/git-sync-provisioning-error-rate/,/^## /p'
```
Runbook covers: service CPU/memory, job controller logs, dependency errors (Folder/Dashboard/IAM API servers).

## Findings

- **Root cause**: `prod-me-central-1` cluster meltdown (known, ignorable)
- **No other clusters affected** meaningfully
- All SLO recording rules were `inactive` state with `health: ok`
- Recording rule metrics (`grafana_slo_sli_*`) returned no data when queried — unclear if this is expected when inactive or a separate issue

## gcx Gaps / Improvement Suggestions

1. **`gcx slo` needs a status/report command per SLO**: `gcx slo reports list` didn't allow filtering by SLO UID easily. A `gcx slo status <uid>` that shows current burn rate, error budget remaining, and which clusters are breaching would shortcut the entire investigation.

2. **SLO → alert rule mapping is missing**: There's no way to go from an SLO UID to its generated alert rules directly. Had to search alert rules by group name pattern matching. A `gcx slo alerts <uid>` command would help.

3. **`gcx slo definitions get` should include current state**: The definition output has no runtime info — no current error budget, no burn rate, no firing status. Adding a `--with-status` flag or a separate `gcx slo status` command would eliminate the need for manual PromQL queries.

4. **Recording rule data gap**: The SLO recording rules existed but produced no queryable metrics (`grafana_slo_sli_*` returned "No data"). Either the recording rules aren't evaluating, or the metrics are written to a different destination than `grafanacloud-prom`. The tool should surface this discrepancy.

5. **Query flag naming**: `gcx query` uses `--from`/`--to` (not `--start`/`--end`). This is fine but worth noting since Grafana UI and some APIs use different naming.

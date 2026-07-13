---
title: Costs and billing
---

# Costs and billing

gcx itself is free and open source. However, several gcx commands operate
Grafana Cloud products that are **billed based on usage**. Creating, updating,
or invoking those products through gcx counts toward your Grafana Cloud bill
exactly as if you had used the Grafana UI or API directly.

!!! note

    [Grafana Assistant pricing](https://grafana.com/docs/grafana-cloud/machine-learning/assistant/pricing/)
    explicitly counts token usage made through the gcx CLI. Automated or
    agent-driven `gcx assistant` invocations consume tokens the same way
    interactive use does.

## Billable products reachable from gcx

| Product | Billing unit | gcx commands | Details |
|---|---|---|---|
| Grafana Assistant | Tokens consumed per request (usage-based) | `gcx assistant prompt`, `gcx assistant dashboard` | [Assistant pricing](https://grafana.com/docs/grafana-cloud/machine-learning/assistant/pricing/) |
| Synthetic Monitoring | Test executions (per probe, per run) plus resulting metrics and logs | `gcx synthetic-monitoring checks create` / `update` | [Synthetic Monitoring invoice](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/synthetic-monitoring-invoice/) |
| Performance Testing (k6) | Virtual User Hours (VUh); browser VUs are billed at a higher rate | `gcx k6 load-tests create`, `gcx k6 test-run emit --apply` | [Performance Testing invoice](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/performance-testing-invoice/) |
| IRM (OnCall + Incident) | Monthly active IRM users | `gcx irm oncall …`, `gcx irm incidents …` write actions | [IRM invoice](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/irm-invoice/) |

Notes on the table:

- **Assistant Investigations** (`gcx assistant investigations …`) is in public
  preview and currently has no charge.
- **IRM**: a user counts as active when they are on an OnCall schedule or
  escalation chain, change alert group status, or create or edit incidents —
  actions gcx can perform on a user's behalf.
- Each plan includes free monthly allowances (for example, included Assistant
  tokens per active user and included Synthetic Monitoring executions). For
  current amounts and rates, refer to [Grafana Cloud pricing](https://grafana.com/pricing/).

## What is not billed

- Querying metrics, logs, traces, and profiles (`gcx metrics query`,
  `gcx logs query`, and so on) — Grafana Cloud bills telemetry ingestion and
  retention, not queries.
- Managing dashboards, folders, datasources, alert rules, and other resources
  (`gcx resources push` / `pull`, `gcx datasources`, `gcx alert`).
- Adaptive Metrics, Logs, and Traces commands — these are cost-*reduction*
  features.

## Monitor your usage

Grafana Cloud provides usage dashboards, cost attribution, and usage alerts so
you can watch spend before it lands on an invoice. See the
[Cost Management and Billing documentation](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/).

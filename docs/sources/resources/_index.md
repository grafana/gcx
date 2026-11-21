---
aliases:
  - ./guides/available-resources/ # /docs/grafana/<GRAFANA_VERSION>/ai-tools/grafana-cli/gcx/guides/available-resources/
title: Work with your resources
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Grafana and Grafana Cloud resources you can manage with gcx, and the commands to use for each.
keywords:
  - gcx
  - Grafana CLI
  - resources
weight: 7
---

# Work with your resources

`gcx` lets you manage resources across Grafana and Grafana Cloud, from dashboards and alert rules to SLOs, on-call schedules, and synthetic checks.

You can work with resources in two ways:

- [Declarative resource workflows](#declarative-resource-workflows): Manage resources as JSON or YAML files with a single set of commands.
- [Product-specific commands](#product-specific-commands): Use dedicated commands for each Grafana area or Grafana Cloud product.

You can also [query your telemetry](#query-telemetry), such as metrics, logs, traces, and profiles, from the command line.

{{< admonition type="note" >}}
For the Grafana roles, RBAC actions, and Grafana Cloud access policy scopes each area requires, refer to [Roles and permissions](../permissions/).
{{< /admonition >}}

## Declarative resource workflows

Use [`gcx resources`](../cli-reference/#gcx-resources) to pull, push, inspect, edit, validate, and delete resources as JSON or YAML manifests. The same commands work for Grafana-native resources, such as `dashboards`, `folders`, `alertrules`, `routingtrees`, `templategroups`, and `timeintervals`, and for supported Grafana Cloud provider resources.

To find out which resource types your instance serves, run [`gcx resources list-types`](../cli-reference/#gcx-resources-list-types). To view example manifests, run [`gcx resources list-examples`](../cli-reference/#gcx-resources-list-examples).

For step-by-step workflows, refer to [Resource management workflows](./manage-resources/).

## Product-specific commands

Each Grafana area and Grafana Cloud product also has its own commands to create, inspect, update, and operate its resources. Areas that also list `gcx resources` in the table support both ways of working.

The following table lists the main Grafana areas and Grafana Cloud products you can manage with `gcx`. For the complete list of commands, refer to the [CLI reference](../cli-reference/) or run the command with `--help`.

| Area | What you can manage | Command | Guides |
| --- | --- | --- | --- |
| **Adaptive Telemetry** | Rules, policies, and recommendations for Adaptive Metrics, Logs, Traces, and Profiles, and log archiving (experimental) | [`gcx metrics adaptive`](../cli-reference/#gcx-metrics-adaptive), [`gcx logs adaptive`](../cli-reference/#gcx-logs-adaptive), [`gcx logs archive`](../cli-reference/#gcx-logs-archive), [`gcx traces adaptive`](../cli-reference/#gcx-traces-adaptive), [`gcx profiles adaptive`](../cli-reference/#gcx-profiles-adaptive) | |
| **Agent Observability** | AI agents, conversations, evaluations, model cards, and prompt templates | [`gcx agento11y`](../cli-reference/#gcx-agento11y) | |
| **Alerting** | Alert rules, contact points, notification policies, and related alerting configuration | [`gcx alert`](../cli-reference/#gcx-alert), [`gcx resources`](../cli-reference/#gcx-resources) | <ul><li>[Lint resources](./lint-resources/)</li><li>[Use gcx to manage alerting resources](https://grafana.com/docs/grafana/<GRAFANA_VERSION>/alerting/set-up/provision-alerting-resources/gcx-provisioning/)</li></ul> |
| **Annotations** | Annotations on dashboards and panels, organization-wide annotations, and annotation tags | [`gcx annotations`](../cli-reference/#gcx-annotations) | |
| **Application Observability** | Monitored services and their settings | [`gcx appo11y`](../cli-reference/#gcx-appo11y) | |
| **Dashboards** | Dashboards, their version history, and rendered snapshots | [`gcx dashboards`](../cli-reference/#gcx-dashboards), [`gcx resources`](../cli-reference/#gcx-resources) | <ul><li>[Dashboards as code](./dashboards-as-code/)</li><li>[Lint resources](./lint-resources/)</li></ul> |
| **Data sources** | Data source configurations and health checks | [`gcx datasources`](../cli-reference/#gcx-datasources) | |
| **Database Observability** | Monitored database instances | [`gcx dbo11y`](../cli-reference/#gcx-dbo11y) | |
| **Error Tracking** | Projects (experimental) | `gcx error-tracking` | |
| **Fleet Management** | Collectors and configuration pipelines | [`gcx fleet`](../cli-reference/#gcx-fleet) | |
| **Folders** | Folders | [`gcx resources`](../cli-reference/#gcx-resources) | |
| **Frontend Observability** | Frontend apps, source maps, and user sessions | [`gcx frontend`](../cli-reference/#gcx-frontend) | |
| **Grafana Assistant** | Conversations, investigations, MCP servers, and Assistant Watchers | [`gcx assistant`](../cli-reference/#gcx-assistant) | <ul><li>[Manage Assistant Watchers](./manage-assistant-watchers/)</li><li>[Check Assistant usage and limits](../guides/assistant-usage/)</li></ul> |
| **Grafana Cloud platform** | Organization memberships and Grafana Cloud stacks | [`gcx cloud`](../cli-reference/#gcx-cloud) | |
| **Instrumentation** | Kubernetes clusters, cluster apps, and monitored services | [`gcx instrumentation`](../cli-reference/#gcx-instrumentation) | |
| **IRM Incidents** | Incidents and post-incident reviews | [`gcx irm incidents`](../cli-reference/#gcx-irm-incidents), [`gcx resources`](../cli-reference/#gcx-resources) | [Incident descriptions](./manage-resources/#incident-descriptions) |
| **IRM OnCall** | Schedules, escalation chains, integrations, and other OnCall configuration | [`gcx irm oncall`](../cli-reference/#gcx-irm-oncall) | [Manage OnCall notification rules](./manage-oncall-notification-rules/) |
| **Knowledge Graph** | Entities, relationships, and the rules that shape them | [`gcx kg`](../cli-reference/#gcx-kg) | |
| **Machine Learning** | Forecasts, holidays, outlier detectors, and their alerts | [`gcx ml`](../cli-reference/#gcx-ml), [`gcx resources`](../cli-reference/#gcx-resources) | <ul><li>[Use gcx to manage Machine Learning resources](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/additional-configuration/gcx/)</li><li>[Manage Machine Learning resources as code](./manage-machine-learning/)</li></ul> |
| **Performance Testing (k6)** | Projects, load tests, schedules, and test runs | [`gcx k6`](../cli-reference/#gcx-k6) | |
| **SLOs** | SLO definitions and reports | [`gcx slo`](../cli-reference/#gcx-slo), [`gcx resources`](../cli-reference/#gcx-resources) | [Use gcx to manage SLO resources](https://grafana.com/docs/grafana-cloud/observe-and-act/alert-and-measure-reliability/slo/set-up/gcx/) |
| **Synthetic Monitoring** | Checks, probes, and saved queries | [`gcx synthetic-monitoring`](../cli-reference/#gcx-synthetic-monitoring), [`gcx resources`](../cli-reference/#gcx-resources) | |

## Query telemetry

Use the signal commands to query your data sources directly from the command line:

| Signal | Command | Guides |
| --- | --- | --- |
| **Metrics** | [`gcx metrics`](../cli-reference/#gcx-metrics) | |
| **Logs** | [`gcx logs`](../cli-reference/#gcx-logs) | |
| **Traces** | [`gcx traces`](../cli-reference/#gcx-traces) | |
| **Profiles** | [`gcx profiles`](../cli-reference/#gcx-profiles) | [Compare profiles](../guides/compare-profiles/) |

To continue in Grafana, add `--share-link` or `--open` to a query command to print or open the query in Explore. On the `query` commands and `gcx profiles diff`, add `--drilldown-link` or `--open-drilldown` to use the matching Drilldown app instead; the `metrics` commands have no Drilldown equivalent. If Drilldown can't show the query, `gcx` uses the Explore link.

To investigate an empty dashboard or missing application telemetry, refer to [Diagnose missing telemetry](../guides/diagnose-missing-telemetry/).

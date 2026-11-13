---
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
weight: 1
---

# Work with your resources

`gcx` lets you manage resources across Grafana and Grafana Cloud, from dashboards and alert rules to SLOs, on-call schedules, and synthetic checks.

You can work with resources in two ways:

- [Declarative resource workflows](#declarative-resource-workflows): Manage resources as JSON or YAML files with a single set of commands.
- [Product-specific commands](#product-specific-commands): Use dedicated commands for each Grafana area or Grafana Cloud product.

You can also [query your telemetry](#query-telemetry), such as metrics, logs, traces, and profiles, from the command line.

## Declarative resource workflows

Use [`gcx resources`](../../cli-reference/#gcx-resources) to pull, push, inspect, edit, validate, and delete resources as JSON or YAML manifests. The same commands work for Grafana-native resources, such as `dashboards`, `folders`, `alertrules`, `routingtrees`, `templategroups`, and `timeintervals`, and for supported Grafana Cloud provider resources.

To find out which resource types your instance serves, run [`gcx resources list-types`](../../cli-reference/#gcx-resources-list-types). To view example manifests, run [`gcx resources list-examples`](../../cli-reference/#gcx-resources-list-examples).

For step-by-step workflows, refer to [Manage resources](../manage-resources/).

## Product-specific commands

Each Grafana area and Grafana Cloud product also has its own commands to create, inspect, update, and operate its resources. Areas that also list `gcx resources` in the table support both ways of working.

The following table lists the main Grafana areas and Grafana Cloud products you can manage with `gcx`. For the complete list of commands, refer to the [CLI reference](../../cli-reference/) or run the command with `--help`.

| Area | What you can manage | Command | Guides |
| --- | --- | --- | --- |
| **Dashboards** | Dashboards, their version history, and rendered snapshots | [`gcx dashboards`](../../cli-reference/#gcx-dashboards), [`gcx resources`](../../cli-reference/#gcx-resources) | [Dashboards as code](../dashboards-as-code/), [Lint resources](../lint-resources/) |
| **Folders** | Folders | [`gcx resources`](../../cli-reference/#gcx-resources) | |
| **Data sources** | Data source configurations and health checks | [`gcx datasources`](../../cli-reference/#gcx-datasources) | |
| **Alerting** | Alert rules, contact points, notification policies, and related alerting configuration | [`gcx alert`](../../cli-reference/#gcx-alert), [`gcx resources`](../../cli-reference/#gcx-resources) | [Lint resources](../lint-resources/) |
| **SLOs** | SLO definitions and reports | [`gcx slo`](../../cli-reference/#gcx-slo), [`gcx resources`](../../cli-reference/#gcx-resources) | |
| **IRM Incidents** | Incidents and post-incident reviews | [`gcx irm incidents`](../../cli-reference/#gcx-irm-incidents), [`gcx resources`](../../cli-reference/#gcx-resources) | [Incident descriptions](../manage-resources/#incident-descriptions) |
| **IRM OnCall** | Schedules, escalation chains, integrations, and other OnCall configuration | [`gcx irm oncall`](../../cli-reference/#gcx-irm-oncall) | [Manage OnCall notification rules](../manage-oncall-notification-rules/) |
| **Machine Learning** | Forecasts, holidays, outlier detectors, and their alerts | `gcx ml`, [`gcx resources`](../../cli-reference/#gcx-resources) | [Manage Machine Learning resources](../manage-machine-learning/) |
| **Adaptive Telemetry** | Rules, policies, and recommendations for Adaptive Metrics, Logs, Traces, and Profiles | [`gcx metrics adaptive`](../../cli-reference/#gcx-metrics-adaptive), [`gcx logs adaptive`](../../cli-reference/#gcx-logs-adaptive), [`gcx traces adaptive`](../../cli-reference/#gcx-traces-adaptive), [`gcx profiles adaptive`](../../cli-reference/#gcx-profiles-adaptive) | |
| **Synthetic Monitoring** | Checks, probes, and saved queries | [`gcx synthetic-monitoring`](../../cli-reference/#gcx-synthetic-monitoring), [`gcx resources`](../../cli-reference/#gcx-resources) | |
| **Performance Testing (k6)** | Projects, load tests, schedules, and test runs | [`gcx k6`](../../cli-reference/#gcx-k6) | |
| **Fleet Management** | Collectors and configuration pipelines | [`gcx fleet`](../../cli-reference/#gcx-fleet) | |
| **Frontend Observability** | Frontend apps, source maps, and user sessions | [`gcx frontend`](../../cli-reference/#gcx-frontend) | |
| **Application Observability** | Monitored services and their settings | [`gcx appo11y`](../../cli-reference/#gcx-appo11y) | |
| **Database Observability** | Monitored database instances | [`gcx dbo11y`](../../cli-reference/#gcx-dbo11y) | |
| **Instrumentation** | Kubernetes clusters, cluster apps, and monitored services | [`gcx instrumentation`](../../cli-reference/#gcx-instrumentation) | |
| **Grafana Assistant** | Conversations, investigations, MCP servers, and Assistant Watchers | [`gcx assistant`](../../cli-reference/#gcx-assistant) | [Manage Assistant Watchers](../manage-assistant-watchers/), [Check Assistant usage and limits](../assistant-usage/) |
| **Agent Observability** | AI agents, conversations, evaluations, and prompt templates | [`gcx agento11y`](../../cli-reference/#gcx-agento11y) | |
| **Knowledge Graph** | Entities, relationships, and the rules that shape them | [`gcx kg`](../../cli-reference/#gcx-kg) | |
| **Grafana Cloud platform** | Organization memberships and Grafana Cloud stacks | [`gcx cloud`](../../cli-reference/#gcx-cloud) | |

## Query telemetry

Use the signal commands to query your data sources directly from the command line:

| Signal | Command | Guides |
| --- | --- | --- |
| **Metrics** | [`gcx metrics`](../../cli-reference/#gcx-metrics) | |
| **Logs** | [`gcx logs`](../../cli-reference/#gcx-logs) | |
| **Traces** | [`gcx traces`](../../cli-reference/#gcx-traces) | |
| **Profiles** | [`gcx profiles`](../../cli-reference/#gcx-profiles) | [Compare profiles](../compare-profiles/) |

To investigate an empty dashboard or missing application telemetry, refer to [Diagnose missing telemetry](../diagnose-missing-telemetry/).

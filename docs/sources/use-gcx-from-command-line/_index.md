---
aliases:
  - ./guides/available-resources/ # /docs/grafana/<GRAFANA_VERSION>/ai-tools/grafana-cli/gcx/guides/available-resources/
title: Use gcx from the command line
labels:
  products:
    - cloud
    - enterprise
    - oss
description: How gcx commands are structured, how to select a context and control the output, and the resources and telemetry you can work with from the command line.
keywords:
  - gcx
  - Grafana CLI
  - command line
  - resources
  - commands
weight: 5
---

# Use `gcx` from the command line

Run `gcx` commands from a terminal or a script to manage resources across Grafana and Grafana Cloud, from dashboards and alert rules to SLOs, on-call schedules, and synthetic checks, and to query your telemetry.

This page explains how `gcx` commands work, then lists what you can manage and query.

## Before you begin

- [Install `gcx`](../installation/).
- [Authenticate `gcx`](../authentication/) against your Grafana instance or Grafana Cloud stack.
- Optional: [Manage gcx contexts and configuration settings](../configuration/) if you work with more than one instance or environment.

{{< admonition type="note" >}}
For the Grafana roles, RBAC actions, and Grafana Cloud access policy scopes each area requires, refer to [Roles and permissions](../authentication/permissions/).
{{< /admonition >}}

## How `gcx` commands work

`gcx` commands follow the pattern `gcx AREA NOUN VERB`, for example:

```bash
gcx slo definitions list
gcx resources push ./dashboards/
gcx logs query --from=1h
gcx irm oncall schedules get my-schedule
```

- The **area** is a Grafana area or Grafana Cloud product, such as `slo`, `alert`, or `irm`.
- The **noun** is the type of object in that area.
- The **verb** is the action, such as `list`, `get`, `create`, `update`, or `delete`.

To explore the commands:

- Run `gcx --help` to list every area, or `gcx AREA --help` to list the commands in one area.
- Run `gcx help-tree` to print a compact tree of the command hierarchy. Add a command path, such as `gcx help-tree resources`, or `--depth 2` to limit the output.
- Refer to the [CLI reference](../cli-reference/) for every command, flag, and environment variable.

### Select a context

A context stores the Grafana instance, credentials, and defaults that `gcx` uses. By default, commands run against the current context. To use another one:

- Add `--context NAME` to a single command, for example `gcx --context staging resources get dashboards`.
- Run `gcx config use-context NAME` to change the current context.
- Run `gcx config list-contexts` to list the contexts you have.

For how to define contexts, refer to [Manage gcx contexts and configuration settings](../configuration/).

### Control the output

Commands write results to standard output and diagnostics, such as warnings and progress, to standard error. This lets you pipe a result to another tool without extra text.

- Use `-o` or `--output` to choose a format. Most commands support `text`, `json`, and `yaml`. Many also support `wide`, and query commands can support formats such as `table`, `csv`, and `graph`. Run `gcx COMMAND --help` for the formats a command supports.
- Use `--json FIELD1,FIELD2` to return only specific fields. Use `--json ?` to list the fields a command offers.
- Use `--jq EXPRESSION` to filter JSON output with a jq expression.
- Use `--no-color` to disable color and `--no-truncate` to stop table columns from being shortened. `gcx` turns truncation off automatically when the output is piped.
- Use `-v`, `-vv`, or `-vvv` to print more diagnostic output on standard error, up to every HTTP request at `-vvv`.

For example, to list dashboards as JSON, or to discover which fields you can select:

```bash
gcx resources get dashboards -o json
gcx resources get dashboards --json ?
```

When an AI coding agent runs `gcx`, defaults such as the output format change. Refer to [Use gcx with AI agents](../use-gcx-with-agents/).

### Check the exit code

Every command returns an exit code, so scripts can react to the outcome without parsing messages.

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | General error |
| `2` | Usage error, such as an unknown command, a bad flag, or a missing required flag |
| `3` | Authentication or authorization failure |
| `4` | Partial failure: some resources succeeded and others failed, or lint reported violations |
| `5` | Cancelled, for example with Ctrl+C or a declined confirmation |
| `6` | Incompatible Grafana version (Grafana earlier than version 12) |

Run `gcx help exit-codes` for example scenarios.

## Work with your resources

You can work with resources in two ways:

- [Declarative resource workflows](#declarative-resource-workflows): Manage resources as JSON or YAML files with a single set of commands.
- [Product-specific commands](#product-specific-commands): Use dedicated commands for each Grafana area or Grafana Cloud product.

You can also [query your telemetry](#query-telemetry), such as metrics, logs, traces, and profiles, from the command line.

### Declarative resource workflows

Use [`gcx resources`](../cli-reference/#gcx-resources) to pull, push, inspect, edit, validate, and delete resources as JSON or YAML manifests. The same commands work for Grafana-native resources, such as `dashboards`, `folders`, `alertrules`, `routingtrees`, `templategroups`, and `timeintervals`, and for supported Grafana Cloud provider resources.

To find out which resource types your instance serves, run [`gcx resources list-types`](../cli-reference/#gcx-resources-list-types). To view example manifests, run [`gcx resources list-examples`](../cli-reference/#gcx-resources-list-examples).

For step-by-step workflows, refer to [Search, edit, pull, and push resources](./manage-resources/).

### Product-specific commands

Each Grafana area and Grafana Cloud product also has its own commands to create, inspect, update, and operate its resources. Areas that also list `gcx resources` in the table support both ways of working.

The following table lists the main Grafana areas and Grafana Cloud products you can manage with `gcx`. For the complete list of commands, refer to the [CLI reference](../cli-reference/) or run the command with `--help`.

| Area | What you can manage | Command | Guides |
| --- | --- | --- | --- |
| **Adaptive Telemetry** | Rules, policies, and recommendations for Adaptive Metrics, Logs, Traces, and Profiles, and log archiving (experimental) | [`gcx metrics adaptive`](../cli-reference/#gcx-metrics-adaptive), [`gcx logs adaptive`](../cli-reference/#gcx-logs-adaptive), [`gcx logs archive`](../cli-reference/#gcx-logs-archive), [`gcx traces adaptive`](../cli-reference/#gcx-traces-adaptive), [`gcx profiles adaptive`](../cli-reference/#gcx-profiles-adaptive) | |
| **Agent Observability** | AI agents, conversations, evaluations, model cards, and prompt templates | [`gcx agento11y`](../cli-reference/#gcx-agento11y) | |
| **Alerting** | Alert rules, contact points, notification policies, and related alerting configuration | [`gcx alert`](../cli-reference/#gcx-alert), [`gcx resources`](../cli-reference/#gcx-resources) | <ul><li>[Lint dashboards and alert rules](./lint-resources/)</li><li>[Use gcx to manage alerting resources](https://grafana.com/docs/grafana/<GRAFANA_VERSION>/alerting/set-up/provision-alerting-resources/gcx-provisioning/)</li></ul> |
| **Annotations** | Annotations on dashboards and panels, organization-wide annotations, and annotation tags | [`gcx annotations`](../cli-reference/#gcx-annotations) | |
| **Application Observability** | Monitored services and their settings | [`gcx appo11y`](../cli-reference/#gcx-appo11y) | |
| **Dashboards** | Dashboards, their version history, and rendered snapshots | [`gcx dashboards`](../cli-reference/#gcx-dashboards), [`gcx resources`](../cli-reference/#gcx-resources) | <ul><li>[Dashboards as code](./dashboards-as-code/)</li><li>[Lint dashboards and alert rules](./lint-resources/)</li></ul> |
| **Data sources** | Data source configurations and health checks | [`gcx datasources`](../cli-reference/#gcx-datasources) | |
| **Database Observability** | Monitored database instances | [`gcx dbo11y`](../cli-reference/#gcx-dbo11y) | |
| **Fleet Management** | Collectors and configuration pipelines | [`gcx fleet`](../cli-reference/#gcx-fleet) | |
| **Folders** | Folders | [`gcx resources`](../cli-reference/#gcx-resources) | |
| **Frontend Observability** | Frontend apps, source maps, and user sessions | [`gcx frontend`](../cli-reference/#gcx-frontend) | |
| **Grafana Assistant** | Conversations, investigations, MCP servers, Watchers, and change review checks | [`gcx assistant`](../cli-reference/#gcx-assistant) | <ul><li>[Manage Watchers](./manage-assistant-watchers/)</li><li>[Manage Assistant change review checks](./manage-change-review-checks/)</li><li>[Check Assistant usage and limits](../guides/assistant-usage/)</li></ul> |
| **Grafana Cloud platform** | Organization memberships and Grafana Cloud stacks | [`gcx cloud`](../cli-reference/#gcx-cloud) | |
| **Grafana Pages** | Live pages on Grafana data: drafts and published pages | [`gcx pages`](../cli-reference/#gcx-pages) | [Build Grafana Pages](./build-grafana-pages/) |
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

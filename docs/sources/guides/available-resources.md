---
title: Available resources
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

# Available resources

You can manage Grafana and Grafana Cloud resources with `gcx` in two ways:

- **Declarative resource workflows ([`gcx resources`](../../cli-reference/#gcx-resources)):** Pull, push, inspect, edit, validate, and delete resources as JSON or YAML manifests—including Grafana-native resources (such as `dashboards`, `folders`, `alertrules`, `routingtrees`, `templategroups`, and `timeintervals`) and supported Grafana Cloud provider resources. Run [`gcx resources list-types`](../../cli-reference/#gcx-resources-list-types) to inspect the resource types served by your instance and [`gcx resources list-examples`](../../cli-reference/#gcx-resources-list-examples) to view example manifests.
- **Product-specific commands:** Create, inspect, update, and operate resources per Grafana area or Grafana Cloud product.

The following table lists all resources available in the [CLI reference](../../cli-reference/):

| Area | Resources | Command |
| --- | --- | --- |
| **Dashboards** | Dashboards, dashboard version history (`versions`), and rendered PNG snapshots (`snapshot`) | [`gcx dashboards`](../../cli-reference/#gcx-dashboards), [`gcx resources`](../../cli-reference/#gcx-resources) |
| **Folders** | Folders (`folders`) | [`gcx resources`](../../cli-reference/#gcx-resources) |
| **Data sources** | Data source configurations, schemas, and health checks (plus direct queries across supported data source types) | [`gcx datasources`](../../cli-reference/#gcx-datasources) |
| **Alerting** | Grafana-managed alert rules (`rules` / `alertrules`), rule groups (`groups`), alert instances (`instances`), state history (`state-history`), data source-managed rule groups and namespaces (`ruler`), contact points (`contact-points`), mute timings (`mute-timings`), notification policies (`notification-policies`), routing trees (`routing-trees` / `routingtrees`), notification history (`notification-history`), notification templates (`templates`), template groups (`template-groups` / `templategroups`), and time intervals (`time-intervals` / `timeintervals`) | [`gcx alert`](../../cli-reference/#gcx-alert), [`gcx resources`](../../cli-reference/#gcx-resources) |
| **SLOs** | SLO definitions (`definitions` / `slos`) and SLO reports (`reports`) | [`gcx slo`](../../cli-reference/#gcx-slo), [`gcx resources`](../../cli-reference/#gcx-resources) |
| **IRM — Incidents** | Incidents, incident activity (`activity`), severities (`severities`), contexts (`list-contexts`), and post-incident reviews (`get-pir`) | [`gcx irm incidents`](../../cli-reference/#gcx-irm-incidents), [`gcx resources`](../../cli-reference/#gcx-resources) |
| **IRM — OnCall** | Alert groups (`alert-groups`), escalation chains (`escalation-chains`), escalation policies and steps (`escalation-policies`), integrations and templates (`integrations`), organizations (`organizations`), resolution notes (`resolution-notes`), routes (`routes`), schedules (`schedules`), shifts (`shifts`), shift swaps (`shift-swaps`), Slack channels (`slack-channels`), teams (`teams`), user groups (`user-groups`), users (`users`), and outgoing webhooks (`webhooks`) | [`gcx irm oncall`](../../cli-reference/#gcx-irm-oncall) |
| **Adaptive Metrics** | Aggregation rules (`rules`), recommendations (`recommendations`), segments (`segments`), and exemptions (`exemptions`) | [`gcx metrics adaptive`](../../cli-reference/#gcx-metrics-adaptive) |
| **Adaptive Logs** | Drop rules (`drop-rules`), exemptions (`exemptions`), log patterns (`patterns`), and segments (`segments`) | [`gcx logs adaptive`](../../cli-reference/#gcx-logs-adaptive) |
| **Adaptive Traces** | Tail-sampling policies (`policies`) and recommendations (`recommendations`) | [`gcx traces adaptive`](../../cli-reference/#gcx-traces-adaptive) |
| **Synthetic Monitoring** | Checks (`checks` / `checks.syntheticmonitoring`), probes (`probes`), saved queries (`queries`), and check suggestions (`suggestions`) | [`gcx synthetic-monitoring`](../../cli-reference/#gcx-synthetic-monitoring), [`gcx resources`](../../cli-reference/#gcx-resources) |
| **Performance Testing (k6)** | Projects (`projects`), load tests and scripts (`load-tests`), schedules (`schedules`), test runs (`runs`, `test-run`), private load zones (`load-zones`), and environment variables (`env-vars`) | [`gcx k6`](../../cli-reference/#gcx-k6) |
| **Fleet Management** | Collectors (`collectors`), configuration pipelines (`pipelines`), and tenant limits (`tenant`) | [`gcx fleet`](../../cli-reference/#gcx-fleet) |
| **Frontend Observability** | Frontend apps (`apps`), sourcemaps (`apply-sourcemap`, `list-sourcemaps`, `delete-sourcemap`), and user sessions (`sessions`) | [`gcx frontend`](../../cli-reference/#gcx-frontend) |
| **Application Observability** | Monitored services and service maps (`services`), operations (`operations`), service overrides (`overrides`), and settings (`settings`) | [`gcx appo11y`](../../cli-reference/#gcx-appo11y) |
| **Database Observability** | Monitored database instances (`instances`) | [`gcx dbo11y`](../../cli-reference/#gcx-dbo11y) |
| **Instrumentation** | Kubernetes clusters and cluster apps (`clusters`, `clusters apps`), and monitored service filters (`services`) | [`gcx instrumentation`](../../cli-reference/#gcx-instrumentation) |
| **Grafana Assistant** | Conversations (`conversation`), investigations (`investigations`), MCP servers (`mcp-servers`), and Assistant Watchers (`watchers`) | [`gcx assistant`](../../cli-reference/#gcx-assistant) |
| **Agent Observability** | AI agents (`agents`), conversations (`conversations`), saved conversations (`saved-conversations`), collections (`collections`), evaluators (`evaluators`), experiments, test suites, and trials (`experiments`), generations (`generations`), guards (`guards`), judge models and providers (`judge`), model rates (`model-rates`), evaluation rules (`rules`), and prompt templates (`templates`) | [`gcx agento11y`](../../cli-reference/#gcx-agento11y) |
| **Knowledge Graph** | Entities (`entities`), relationships (`relationships`), model rules (`model-rules`), Prometheus rules (`prom-rules`), relabel rules (`relabel-rules`), notifications (`notifications`), suppressions (`suppressions`), thresholds (`thresholds`), and telemetry quality checks (`quality`) | [`gcx kg`](../../cli-reference/#gcx-kg) |
| **Grafana Cloud platform** | Organization memberships (`orgs`) and Grafana Cloud stacks (`stacks`) | [`gcx cloud`](../../cli-reference/#gcx-cloud) |

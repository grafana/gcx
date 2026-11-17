---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/permissions/
title: Roles and permissions
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 7
---

# Roles and permissions

What you can do with `gcx` depends on how you sign in and which Grafana products or services you use. Use this page to find out what permissions you need for authentication, what each product requires, and how to fix authorization errors.

- [User and service account authentication](#user-and-service-account-authentication): How OAuth and service account permissions work, and the role required for OAuth sign-in.
- [Permissions by product and service](#permissions-by-product-and-service): Grafana roles and actions needed for dashboards, Grafana Alerting, Grafana IRM, Synthetic Monitoring, Grafana Cloud k6, SLO, and more.
- [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes): Scopes for the separate Grafana Cloud credential used by `gcx cloud` and direct Grafana Cloud API calls.
- [Fix an authorization error](#fix-an-authorization-error): How to inspect your effective actions and troubleshoot exit code `3` errors.

{{< admonition type="note" >}}
These permissions apply to the current release of `gcx` and can change as Grafana products and APIs evolve. For the latest requirements, refer to the documentation linked in each product section.
{{< /admonition >}}

## User and service account authentication

For commands that interact with your Grafana instance or Grafana Cloud stack, `gcx` uses the permissions of the Grafana identity you signed in with:

- **OAuth sign-in:** `gcx` acts as your Grafana user. Your effective permissions are your own Grafana user permissions. OAuth sign-in doesn't add to them, so you can't do anything with `gcx` that you can't already do in the Grafana UI.
- **Service account sign-in:** When you sign in with a service account token, `gcx` uses the permissions assigned to that service account. Refer to [Service account token](../authentication/#service-account-token) for setup details.
- **Basic authentication and mutual TLS (mTLS):** For self-managed Grafana, `gcx` uses the permissions of the authenticated Grafana user or identity.

For setup details for each sign-in method, refer to [Authenticate gcx](../authentication/).

In all cases, your Grafana user or service account needs the product-specific roles and actions listed in [Permissions by product and service](#permissions-by-product-and-service).

### Required role for OAuth sign-in

To authorize a `gcx` CLI connection with OAuth, your Grafana user needs the `grafana-assistant-app.tokens.gcx:access` permission. The gcx User role, registered by the Grafana Assistant application, grants this permission and is assigned automatically to users with the Viewer basic role or higher.

If your default role is `None`, or you're using a custom role that doesn't grant the default `plugins.app:access` action, you also need to explicitly grant access to `grafana-assistant-app`:

```json
{
  "action": "plugins.app:access",
  "scope": "plugins:id:grafana-assistant-app"
}
```

The `grafana-assistant-app.tokens.gcx:access` permission only lets you create `gcx` tokens for your own user. It doesn't grant access to other users' tokens, and it doesn't extend your existing Grafana permissions.

If `gcx login` fails with a `Permission Required` error naming the gcx User role, ask your Grafana administrator to assign you the gcx User role or a custom role that includes the `grafana-assistant-app.tokens.gcx:access` permission. If the role doesn't exist on your instance, update the Grafana Assistant application to a version that includes it.

## Permissions by product and service

Grant your Grafana user (for OAuth) or service account (for token authentication) a basic role that matches what you need to do, plus the product permissions listed in the following sections for each service you use.

### Dashboards, folders, and data sources

Dashboards, folders, and data sources use Grafana basic roles, or custom role-based access control (RBAC) roles for scoped access.

| Access or commands | Required role or permission |
| --- | --- |
| Query metrics, logs, traces, and profiles, and read dashboards and folders | Viewer basic role |
| Push and edit dashboards and folders | Editor basic role |
| Manage data source configuration | Admin basic role |

On Grafana Cloud and Grafana Enterprise, you can use RBAC custom roles to scope query access more tightly, for example `datasources:read` plus `datasources:query` on specific data sources.

To learn more about roles and permissions for dashboards, folders, and data sources, refer to [Manage dashboard permissions](https://grafana.com/docs/grafana/latest/administration/user-management/manage-dashboard-permissions/) and [Data source management](https://grafana.com/docs/grafana/latest/administration/data-source-management/).

### Agent Observability

`gcx agento11y` commands use the Agent Observability plugin (`grafana-agento11y-app`) on your stack through your Grafana sign-in.

| Access or commands | Required role or permission |
| --- | --- |
| View agents, model cards, generations, evaluation results, and experiments (`gcx agento11y agents`, `model-cards`, `generations`, `experiments`) | `grafana-agento11y-app.data:read` (Agent Observability Data Reader role or higher) |
| View conversations (`gcx agento11y conversations list`, `get`, `search`, `list-annotations`) | `grafana-agento11y-app.conversations:read` (Agent Observability Reader role or higher) |
| Annotate conversations (`gcx agento11y conversations annotate`) | `grafana-agento11y-app.feedback:write` (Agent Observability Feedback Writer role or higher) |
| Manage evaluators, evaluation rules, templates, experiments, test suites, collections, and saved conversations | `grafana-agento11y-app.eval:write` (Agent Observability Admin role) |
| Create, update, or delete guards (`gcx agento11y guards`) | `grafana-agento11y-app.guards:write` (Agent Observability Admin role) |
| Create or delete model rates (`gcx agento11y model-rates create`, `delete`) | `grafana-agento11y-app.pricing:write` (Agent Observability Admin role) |

All Agent Observability roles also require `plugins.app:access` scoped to `plugins:id:grafana-agento11y-app` (included in the built-in roles).

To learn more about roles and permissions for Agent Observability, refer to [Security and access controls](https://grafana.com/docs/grafana-cloud/observe-and-act/agent-observability/privacy-and-access/security/).

### Alerting

`gcx alert` commands use the Grafana Alerting APIs and require these permissions:

| Access or commands | Required role or permission |
| --- | --- |
| View Grafana-managed alert rules and rule groups (`gcx alert rules`, `gcx alert groups`) | `alert.rules:read`, plus `folders:read` on the rule's folder and `datasources:query` on the data sources the rule queries |
| View alert state history (`gcx alert state-history`) | `alert.rules:read` and `datasources:query` on the state history data source |
| View data source-managed rules (`gcx alert ruler`) | `alert.rules.external:read` on the data source |
| Create, update, or delete data source-managed rules (`gcx alert ruler groups upsert`, `gcx alert ruler groups delete`, `gcx alert ruler namespaces delete`) | `alert.rules.external:write` on the data source |
| View contact points, mute timings, templates, and notification policies | `alert.provisioning:read` or `alert.notifications.provisioning:read` (or the per-resource `alert.notifications.*:read` actions) |
| Create, update, or delete contact points, mute timings, templates, and notification policies | `alert.provisioning:write` or `alert.notifications.provisioning:write` |

To learn more about roles and permissions for Grafana Alerting, refer to [Configure RBAC](https://grafana.com/docs/grafana/latest/alerting/set-up/configure-rbac/).

### Application Observability

`gcx appo11y` commands query your stack's telemetry data sources and use the Application Observability plugin (`grafana-app-observability-app`) through your Grafana sign-in.

| Access or commands | Required role or permission |
| --- | --- |
| Inspect services, operations, labels, and service maps (`gcx appo11y services`, `operations`) | `plugins:grafana-app-observability-app:viewer` (Viewer or Editor basic role), plus `datasources:query` on your metrics, logs, and traces data sources |
| Read or update metrics generator overrides and plugin settings (`gcx appo11y overrides`, `settings`) | `plugins:grafana-app-observability-app:admin` (Admin basic role), plus the Plugins > Plugin Writer role to persist settings changes |

To learn more about roles and permissions for Application Observability, refer to [Application Observability role-based access control](https://grafana.com/docs/grafana-cloud/observe-and-act/monitor-applications/application-observability/manual/rbac/).

### Database Observability

`gcx dbo11y` commands query your Prometheus data source and check the Database Observability activation status on your stack through your Grafana sign-in.

| Access or commands | Required role or permission |
| --- | --- |
| List and inspect monitored database instances (`gcx dbo11y instances list`, `get`) | `plugins:grafana-dbo11y-app:reader` (Viewer basic role or higher), plus `datasources:query` on your Prometheus data source |
| Administer Database Observability plugin settings | `plugins:grafana-dbo11y-app:admin` (Admin basic role) |

To learn more about roles and permissions for Database Observability, refer to [Grafana Cloud app plugin role definitions](https://grafana.com/docs/grafana-cloud/platform/security-and-account-management/security-and-access/authentication-and-permissions/access-control/plugin-role-definitions/).

### Fleet Management

`gcx fleet` commands reach Fleet Management through the Collector app on your stack, so they use your Grafana sign-in and don't need a Grafana Cloud access policy scope. You need these Collector app permissions:

| Access or commands | Required role or permission |
| --- | --- |
| Access the Collector app's named read routes | `grafana-collector-app:read` (`plugins:grafana-collector-app:collector-app-reader`, `plugins:grafana-collector-app:collector-app-pipelines-viewer`, or Viewer basic role) |
| Access the Collector app's wildcard routes (required by some read-only commands) | `grafana-collector-app:admin` (`plugins:grafana-collector-app:collector-app-admin` or Admin basic role) |
| Look up stack details through the Collector app | Viewer basic role |

To learn more about roles and permissions for Fleet Management, refer to [Role-based access control for Fleet Management](https://grafana.com/docs/grafana-cloud/send-data/fleet-management/set-up/role-based-access-control/).

### Frontend Observability

`gcx frontend apps` commands use the Frontend Observability plugin (`grafana-kowalski-app`), except `apply-sourcemap`, which uploads directly to the Faro API using a Grafana Cloud credential.

| Access or commands | Required role or permission |
| --- | --- |
| List or inspect apps and source maps (`gcx frontend apps list`, `gcx frontend apps get`, `gcx frontend apps list-sourcemaps`) | `plugins:grafana-kowalski-app:frontend-observability-viewer` (or Viewer basic role) |
| Create or update apps (`gcx frontend apps create`, `gcx frontend apps update`) | `grafana-kowalski-app.apps:write` (`plugins:grafana-kowalski-app:frontend-observability-editor` or Editor basic role) |
| Delete apps or source map bundles (`gcx frontend apps delete`, `gcx frontend apps delete-sourcemap`) | `grafana-kowalski-app.apps:delete` (`plugins:grafana-kowalski-app:frontend-observability-admin` or Admin basic role) |
| Upload source maps (`gcx frontend apps apply-sourcemap`) | Grafana Cloud credential (plus Grafana sign-in if resolving app names by display name) |

An account with only `grafana-kowalski-app.apps:write` can create and update apps, but can't delete apps or source map bundles.

To learn more about roles and permissions for Frontend Observability, refer to [Frontend Observability role-based access control](https://grafana.com/docs/grafana-cloud/observe-and-act/monitor-applications/frontend-observability/settings-and-policies/rbac/).

### Grafana Assistant

`gcx assistant` commands use the Grafana Assistant plugin (`grafana-assistant-app`) on your stack through your Grafana sign-in. The Assistant Investigation User role is included in the Editor and Admin basic roles. The Viewer basic role doesn't include it.

| Access or commands | Required role or permission |
| --- | --- |
| Access the Grafana Assistant app | `plugins.app:access` scoped to `plugins:id:grafana-assistant-app` |
| Read investigations (`gcx assistant investigations list`) | `grafana-assistant-app.investigations:read` (included in the Assistant Investigation User role) |
| View investigations started automatically by alerts or Grafana Incident Response and Management (IRM) webhooks | `grafana-assistant-app.investigations.system:read` (Assistant System Investigation Viewer role) |
| View organization-wide Assistant usage and limits (`gcx assistant usage get`) | `grafana-assistant-app.usage:read` (included in the Assistant Admin role and Admin basic role) |

#### Troubleshoot Assistant permission errors

If a command such as `gcx assistant investigations list` fails, use the error message to identify the missing permission:

- **Plugin route error (for example, `plugin proxy route access denied`):** The error doesn't name the missing permission. You might be missing `plugins.app:access` scoped to `plugins:id:grafana-assistant-app`. Check your actions with `gcx api /api/access-control/user/actions`, and send the output to your Grafana administrator.
- **`insufficient permissions` or a named action (such as `grafana-assistant-app.investigations:read`):** Ask your administrator to assign you the Assistant Investigation User role.
- **Commands that change resources or call Model Context Protocol (MCP) servers:** `gcx` can't always tell you which permission is missing. If the error doesn't name one, ask your administrator to review your Assistant roles.

To learn more about roles and permissions for Grafana Assistant, refer to [Manage Assistant access with RBAC](https://grafana.com/docs/grafana-cloud/platform/grafana-assistant/privacy-and-security/rbac/).

### Grafana Cloud k6

`gcx k6` permissions depend on whether you sign in with OAuth or a service account token. When you sign in with OAuth, commands go through the k6 plugin on your stack. When you sign in with a service account token, `gcx` exchanges the token with the k6 API directly and also uses a Grafana Cloud credential to look up your stack.

| Access or commands | Required role or permission |
| --- | --- |
| Read-only access to k6 (OAuth sign-in) | `plugins:k6-app:reader` |
| Read and write access to k6, with limited scopes (OAuth sign-in) | `plugins:k6-app:editor` |
| Admin access to everything in k6 (OAuth sign-in) | `plugins:k6-app:admin` |
| Direct k6 API access (service account token) | Admin basic role, plus a Grafana Cloud credential with the `stacks:read` scope |

Refer to [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes) for configuring the `stacks:read` scope.

To learn more about roles and permissions for Grafana Cloud k6, refer to [Configure RBAC](https://grafana.com/docs/grafana-cloud/observe-and-act/testing/k6/projects-and-users/configure-rbac/).

### Grafana IRM

`gcx irm` commands go through the IRM plugin on your stack, so they use your Grafana sign-in and don't need a Grafana Cloud credential. You need an IRM plugin role.

These core roles cover most use cases:

| Access or commands | Required role or permission |
| --- | --- |
| Read-only access to everything in IRM | `plugins:grafana-irm-app:reader` |
| Read access to everything in IRM, plus editing alert groups, schedules, and your own settings | `plugins:grafana-irm-app:oncaller` |
| Everything the Admin role can do, except creating integrations, escalation chains, and outgoing webhooks, and updating ChatOps, other users', and general IRM settings | `plugins:grafana-irm-app:editor` |
| Read and write access to everything in IRM | `plugins:grafana-irm-app:admin` |
| Access IRM incidents | `plugins:grafana-irm-app:incident-access` |

To grant narrower access, use the granular IRM roles for the commands you run:

| Access or commands | Required role or permission |
| --- | --- |
| `gcx irm oncall alert-groups` | Read: `plugins:grafana-irm-app:alert-groups-reader`; Write: `plugins:grafana-irm-app:alert-groups-editor` |
| `gcx irm oncall integrations` | Read: `plugins:grafana-irm-app:integrations-reader`; Write: `plugins:grafana-irm-app:integrations-editor` |
| `gcx irm oncall integrations start-maintenance`, `stop-maintenance` | Read: `plugins:grafana-irm-app:maintenance-reader`; Write: `plugins:grafana-irm-app:maintenance-editor` |
| `gcx irm oncall escalation-chains` | Read: `plugins:grafana-irm-app:escalation-chains-reader`; Write: `plugins:grafana-irm-app:escalation-chains-editor` |
| `gcx irm oncall schedules`, `gcx irm oncall shifts` | Read: `plugins:grafana-irm-app:schedules-reader`; Write: `plugins:grafana-irm-app:schedules-editor` |
| `gcx irm oncall webhooks` | Read: `plugins:grafana-irm-app:outgoing-webhooks-reader`; Write: `plugins:grafana-irm-app:outgoing-webhooks-editor` |

OnCall personal notification rules don't work with a service account because the server rejects service account requests. Sign in as a Grafana user instead. Refer to [Manage OnCall notification rules](../guides/manage-oncall-notification-rules/) for the permissions those commands need.

To learn more about roles and permissions for Grafana IRM, refer to [Roles and permissions for Grafana IRM](https://grafana.com/docs/grafana-cloud/observe-and-act/respond-to-incidents/set-up/manage-access/roles-and-permissions/).

### Instrumentation

`gcx instrumentation` commands manage Grafana Instrumentation Hub through the Collector app (`grafana-collector-app`) on your stack, except `check`, `explain`, and `list-explanations`, which inspect local configuration and documentation.

| Access or commands | Required role or permission |
| --- | --- |
| Look up stack details and run named Collector read routes | `grafana-collector-app:read` (`plugins:grafana-collector-app:collector-app-reader`, `plugins:grafana-collector-app:collector-app-pipelines-viewer`, or Viewer basic role) |
| Inspect and configure clusters, cluster apps, services, and status (`gcx instrumentation status`, `clusters`, `services`, `setup`) | `grafana-collector-app:read` and `grafana-collector-app:admin` (`plugins:grafana-collector-app:collector-app-admin` or Admin basic role) |
| Run the Helm command generated by `gcx instrumentation setup` | Grafana Cloud access policy token with `set:alloy-data-write` and `metrics:read` |

Refer to [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes) for details on the Helm token scopes.

To learn more about roles and permissions for Instrumentation, refer to [Role-based access control for Fleet Management](https://grafana.com/docs/grafana-cloud/send-data/fleet-management/set-up/role-based-access-control/).

### Knowledge Graph

`gcx kg` commands use the Knowledge Graph plugin (`grafana-asserts-app`) on your stack through your Grafana sign-in.

| Access or commands | Required role or permission |
| --- | --- |
| Access Knowledge Graph | `plugins:grafana-asserts-app:knowledge-graph-access` |
| Query the graph, inspect entities and insights, and read rules (`gcx kg graph`, `entities list`, `insights`, `stats`, `diagnose`, `quality`, `meta`) | `plugins:grafana-asserts-app:knowledge-graph-reader` (Viewer basic role or higher) |
| Create, update, or delete rules, thresholds, suppressions, notifications, custom entities, and relationships | `plugins:grafana-asserts-app:knowledge-graph-writer` (Editor or Admin basic role) |

To learn more about roles and permissions for Knowledge Graph, refer to [Grafana Cloud app plugin role definitions](https://grafana.com/docs/grafana-cloud/platform/security-and-account-management/security-and-access/authentication-and-permissions/access-control/plugin-role-definitions/).

### Machine Learning

`gcx ml` commands use the Machine Learning plugin (`grafana-ml-app`) on your stack through your Grafana sign-in.

| Access or commands | Required role or permission |
| --- | --- |
| View forecast jobs, holidays, outlier detectors, and their alerts (`list`, `get`) | `plugins:grafana-ml-app:ml-viewer` (Viewer basic role or higher) |
| Create, update, or delete forecast jobs, holidays, outlier detectors, and their alerts | `plugins:grafana-ml-app:ml-editor` (Editor or Admin basic role) |

To learn more about roles and permissions for Machine Learning, refer to [Configure RBAC permissions](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/additional-configuration/rbac/).

### SLO

`gcx slo` commands use the SLO plugin on your stack. SLO permissions follow folder permissions:

| Access or commands | Required role or permission |
| --- | --- |
| View SLOs in folders where you have folder read permission | `plugins:grafana-slo-app:slo-reader` |
| Create, update, and delete SLOs in folders where you have folder edit permission | `plugins:grafana-slo-app:slo-writer` |
| Everything the SLO Writer role can do, plus changing organization preferences | `plugins:grafana-slo-app:slo-admin` |

To learn more about roles and permissions for SLO, refer to [Configure RBAC permissions](https://grafana.com/docs/grafana-cloud/observe-and-act/alert-and-measure-reliability/slo/set-up/rbac/).

### Synthetic Monitoring

`gcx synthetic-monitoring` (aliased as `gcx sm`) commands go through the Synthetic Monitoring data source on your stack, using your Grafana sign-in. You need a Synthetic Monitoring plugin role:

| Access or commands | Required role or permission |
| --- | --- |
| Read checks, probes, alerts, thresholds, and access tokens | `plugins:grafana-synthetic-monitoring-app:reader` |
| Add, update, and delete checks, probes, alerts, and thresholds | `plugins:grafana-synthetic-monitoring-app:editor` |
| Everything the Editor role can do, plus managing access tokens and enabling or disabling the plugin | `plugins:grafana-synthetic-monitoring-app:admin` |

For narrower access, use the granular roles, such as `plugins:grafana-synthetic-monitoring-app:checks-reader` and `plugins:grafana-synthetic-monitoring-app:checks-writer`, or the equivalent `probes`, `alerts`, and `thresholds` roles.

If `gcx` can't use the data source, it calls the Synthetic Monitoring API directly. That path needs a Grafana Cloud credential to create a Synthetic Monitoring token. Refer to [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes).

To learn more about roles and permissions for Synthetic Monitoring, refer to [Manage users and teams for Synthetic Monitoring](https://grafana.com/docs/grafana-cloud/observe-and-act/testing/synthetic-monitoring/user-and-team-management/).

## Choose Grafana Cloud access policy scopes

Grafana Cloud sign-in is separate from your Grafana sign-in. It gives `gcx` a Grafana Cloud credential for commands that call Grafana Cloud APIs: `gcx cloud` commands, Adaptive Telemetry (`gcx metrics adaptive`, `gcx logs adaptive`, `gcx logs archive`, and `gcx traces adaptive`), `gcx k6` with a service account token, `gcx frontend apps apply-sourcemap`, and Synthetic Monitoring (`gcx synthetic-monitoring`, aliased as `gcx sm`) when it calls the Synthetic Monitoring API directly. Refer to [Grafana Cloud product API](../authentication/#grafana-cloud-product-api) for how to add it.

You can sign in to Grafana Cloud in two ways:

- **Grafana Cloud access policy token:** Recommended for automation. The scopes you give the access policy decide which commands work.
- **Grafana Cloud OAuth:** The experimental browser flow, with `gcx cloud login` or the Grafana Cloud step of `gcx login`. By default, it requests `profile`, `stacks:read`, `stacks:write`, `stacks:delete`, `metrics:write`, `logs:write`, and `traces:write`. Not every Grafana Cloud product command accepts it yet. However, `gcx cloud orgs list` requires it: listing your organizations needs the `profile` scope, and access policy tokens can't enumerate user organization memberships.

If you use a Grafana Cloud access policy token, scope the access policy to the commands you use. Every policy needs `stacks:read`, which `gcx` checks when you sign in and when it validates the token. Add the scopes for the products you use:

| Access or commands | Required scope |
| --- | --- |
| Stack discovery, which resolves the stack slug for all Grafana Cloud commands, plus `gcx k6` reads | `stacks:read` (required) |
| Create or update stacks | `stacks:write` |
| Delete stacks (`gcx cloud stacks delete`) | `stacks:delete` |
| Adaptive Metrics (`gcx metrics adaptive`) | Operation-specific `adaptive-metrics-rules`, `adaptive-metrics-recommendations`, `adaptive-metrics-segments`, or `adaptive-metrics-exemptions` scope (`:read`, `:write`, or `:delete`) |
| Adaptive Logs and Logs Archive (`gcx logs adaptive`, `gcx logs archive`) | `adaptive-logs:admin` |
| Adaptive Traces (`gcx traces adaptive`) | `adaptive-traces:admin` |
| Synthetic Monitoring (`gcx synthetic-monitoring`) direct API token creation | `metrics:write`, `logs:write`, `traces:write` |
| Instrumentation Hub setup (`gcx instrumentation setup`) token for the printed Helm command | `set:alloy-data-write`, `metrics:read` |

Fleet Management, IRM, and SLO commands, and direct telemetry queries such as `gcx metrics query`, `gcx logs query`, `gcx traces query`, and `gcx profiles`, don't use the Grafana Cloud credential. They use your Grafana sign-in, either OAuth or a service account token.

If you're not sure which scopes you need, start with a narrow policy and add scopes as commands report missing-scope errors. You can change the policy's scopes without running `gcx login` again.

To learn more about Grafana Cloud access policy scopes and how to create a policy and token, refer to [Grafana Cloud Access Policies](https://grafana.com/docs/grafana-cloud/platform/security-and-account-management/security-and-access/authentication-and-permissions/access-policies/) and [Create access policies and tokens](https://grafana.com/docs/grafana-cloud/platform/security-and-account-management/security-and-access/authentication-and-permissions/access-policies/create-access-policies/).

## Fix an authorization error

When a command fails because you're missing a permission, it fails with `Authorization failed` and exit code `3`. Often, the error names the action you're missing.

To fix an authorization error:

1. Note the action that the error names, if any.
2. List the actions your user is allowed to perform:

   ```bash
   gcx api /api/access-control/user/actions
   ```

3. Compare the output with the action the error names.
4. Send the output and the error message to your Grafana administrator. They can grant the missing action, or assign you a role that includes it.

Refer to [Roles and permissions](https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/) for role assignment guidance.

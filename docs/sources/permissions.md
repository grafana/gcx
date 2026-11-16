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

## User and service account authentication

For commands that interact with your Grafana instance or Grafana Cloud stack, `gcx` uses the permissions of the Grafana identity you signed in with:

- **OAuth sign-in:** `gcx` acts as your Grafana user. Your effective permissions are your own Grafana user permissions. OAuth sign-in doesn't add to them, so you can't do anything with `gcx` that you can't already do in the Grafana UI.
- **Service account sign-in:** When you sign in with a service account token, `gcx` uses the permissions assigned to that service account. Refer to [Service account token](../authentication/#service-account-token) for setup details.

In both cases, your Grafana user or service account needs the product-specific roles and actions listed in [Permissions by product and service](#permissions-by-product-and-service).

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

### Fleet Management

`gcx fleet` commands reach Fleet Management through the Collector app on your stack, so they use your Grafana sign-in and don't need a Grafana Cloud access policy scope. You need these Collector app permissions:

| Access or commands | Required role or permission |
| --- | --- |
| Access the Collector app's named read routes | `grafana-collector-app:read` (`plugins:grafana-collector-app:collector-app-reader`, `plugins:grafana-collector-app:collector-app-pipelines-viewer`, or Viewer basic role) |
| Access the Collector app's wildcard routes (required by some read-only commands) | `grafana-collector-app:admin` (`plugins:grafana-collector-app:collector-app-admin` or Admin basic role) |
| Look up stack details through the Collector app | Viewer basic role |
| Check Kubernetes monitoring (`gcx instrumentation clusters list`) | `grafana-collector-app:read` and `grafana-collector-app:admin` |

### Grafana Assistant

The Assistant Investigation User role is included in the Editor and Admin basic roles. The Viewer basic role doesn't include it.

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

### Frontend Observability

`gcx frontend apps` commands use the Frontend Observability plugin (`grafana-kowalski-app`), except `apply-sourcemap`, which uploads directly to the Faro API using a Grafana Cloud credential.

| Access or commands | Required role or permission |
| --- | --- |
| List or inspect apps and source maps (`gcx frontend apps list`, `gcx frontend apps get`, `gcx frontend apps list-sourcemaps`) | `plugins:grafana-kowalski-app:frontend-observability-viewer` (or Viewer basic role) |
| Create or update apps (`gcx frontend apps create`, `gcx frontend apps update`) | `grafana-kowalski-app.apps:write` (`plugins:grafana-kowalski-app:frontend-observability-editor` or Editor basic role) |
| Delete apps or source map bundles (`gcx frontend apps delete`, `gcx frontend apps delete-sourcemap`) | `grafana-kowalski-app.apps:delete` (`plugins:grafana-kowalski-app:frontend-observability-admin` or Admin basic role) |
| Upload source maps (`gcx frontend apps apply-sourcemap`) | Grafana Cloud credential (plus Grafana sign-in if resolving app names by display name) |

An account with only `grafana-kowalski-app.apps:write` can create and update apps, but can't delete apps or source map bundles.

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

### Synthetic Monitoring

`gcx synthetic-monitoring` (aliased as `gcx sm`) commands go through the Synthetic Monitoring data source on your stack, using your Grafana sign-in. You need a Synthetic Monitoring plugin role:

| Access or commands | Required role or permission |
| --- | --- |
| Read checks, probes, alerts, thresholds, and access tokens | `plugins:grafana-synthetic-monitoring-app:reader` |
| Add, update, and delete checks, probes, alerts, and thresholds | `plugins:grafana-synthetic-monitoring-app:editor` |
| Everything the Editor role can do, plus managing access tokens and enabling or disabling the plugin | `plugins:grafana-synthetic-monitoring-app:admin` |

For narrower access, use the granular roles, such as `plugins:grafana-synthetic-monitoring-app:checks-reader` and `plugins:grafana-synthetic-monitoring-app:checks-writer`, or the equivalent `probes`, `alerts`, and `thresholds` roles.

If `gcx` can't use the data source, it calls the Synthetic Monitoring API directly. That path needs a Grafana Cloud credential to create a Synthetic Monitoring token. Refer to [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes).

### Grafana Cloud k6

`gcx k6` permissions depend on whether you sign in with OAuth or a service account token. When you sign in with OAuth, commands go through the k6 plugin on your stack. When you sign in with a service account token, `gcx` exchanges the token with the k6 API directly and also uses a Grafana Cloud credential to look up your stack.

| Access or commands | Required role or permission |
| --- | --- |
| Read-only access to k6 (OAuth sign-in) | `plugins:k6-app:reader` |
| Read and write access to k6, with limited scopes (OAuth sign-in) | `plugins:k6-app:editor` |
| Admin access to everything in k6 (OAuth sign-in) | `plugins:k6-app:admin` |
| Direct k6 API access (service account token) | Admin basic role, plus a Grafana Cloud credential with the `stacks:read` scope |

Refer to [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes) for configuring the `stacks:read` scope.

### SLO

`gcx slo` commands use the SLO plugin on your stack. SLO permissions follow folder permissions:

| Access or commands | Required role or permission |
| --- | --- |
| View SLOs in folders where you have folder read permission | `plugins:grafana-slo-app:slo-reader` |
| Create, update, and delete SLOs in folders where you have folder edit permission | `plugins:grafana-slo-app:slo-writer` |
| Everything the SLO Writer role can do, plus changing organization preferences | `plugins:grafana-slo-app:slo-admin` |

## Choose Grafana Cloud access policy scopes

Grafana Cloud sign-in is separate from your Grafana sign-in. It gives `gcx` a Grafana Cloud credential for commands that call Grafana Cloud APIs: `gcx cloud` commands, Adaptive Telemetry (`gcx metrics adaptive`, `gcx logs adaptive`, `gcx logs archive`, and `gcx traces adaptive`), `gcx k6` with a service account token, `gcx frontend apps apply-sourcemap`, and Synthetic Monitoring (`gcx synthetic-monitoring`, aliased as `gcx sm`) when it calls the Synthetic Monitoring API directly. Refer to [Grafana Cloud product API](../authentication/#grafana-cloud-product-api) for how to add it.

You can sign in to Grafana Cloud in two ways:

- **Grafana Cloud access policy token:** Recommended for automation. The scopes you give the access policy decide which commands work.
- **Grafana Cloud OAuth:** The experimental browser flow, with `gcx cloud login` or the Grafana Cloud step of `gcx login`. By default, it requests `profile`, `stacks:read`, `stacks:write`, `stacks:delete`, `metrics:write`, `logs:write`, and `traces:write`. Not every Grafana Cloud product command accepts it yet, though listing your organizations (`gcx cloud orgs list`) requires Grafana Cloud OAuth with the `profile` scope because access policy tokens can't enumerate user organization memberships.

If you use a Grafana Cloud [access policy](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/) token, scope the access policy to the commands you use:

- **`stacks:read` is required:** It resolves your stack and is checked when you sign in and when `gcx` validates the token.
- Add the scopes for the products you use:

| Access or commands | Required scope |
| --- | --- |
| Stack discovery, which resolves the stack slug for all Grafana Cloud commands, plus `gcx k6` reads | `stacks:read` (required) |
| Create or update stacks | `stacks:write` |
| Delete stacks (`gcx cloud stacks delete`) | `stacks:delete` |
| Adaptive Metrics (`gcx metrics adaptive`) | Operation-specific `adaptive-metrics-rules`, `adaptive-metrics-recommendations`, `adaptive-metrics-segments`, or `adaptive-metrics-exemptions` scope (`:read`, `:write`, or `:delete`) |
| Adaptive Logs and Logs Archive (`gcx logs adaptive`, `gcx logs archive`) | `adaptive-logs:admin` |
| Adaptive Traces (`gcx traces adaptive`) | `adaptive-traces:admin` |
| Synthetic Monitoring (`gcx sm`) direct API token creation | `metrics:write`, `logs:write`, `traces:write` |
| Instrumentation Hub setup (`gcx instrumentation setup`) token for the printed Helm command | `set:alloy-data-write`, `metrics:read` |

Fleet Management, IRM, and SLO commands, and direct telemetry queries such as `gcx metrics query`, `gcx logs query`, `gcx traces`, and `gcx profiles`, don't use the Grafana Cloud credential. They use your Grafana sign-in, either OAuth or a service account token.

If you're not sure which scopes you need, start with a narrow policy and add scopes as commands report missing-scope errors. You can change the policy's scopes without running `gcx login` again.

Refer to [Create access policies](https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies/) for how to create the policy and token.

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

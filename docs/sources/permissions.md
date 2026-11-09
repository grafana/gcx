---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/permissions/
title: Roles and Permissions
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 7
---

# Roles and permissions

What you can do with `gcx` depends on how you sign in. Use this page to find out which permissions you need for each sign-in method, and how to fix authorization errors.

- [OAuth sign-in](#grant-permissions-for-oauth-sign-in): `gcx` uses your own Grafana user permissions.
- [Service account sign-in](#grant-permissions-to-a-service-account): `gcx` uses the permissions of the service account.
- [Grafana Cloud sign-in](#choose-grafana-cloud-access-policy-scopes): a separate credential for Grafana Cloud commands, such as `gcx cloud`, scoped by an access policy.

## Grant permissions for OAuth sign-in

When you sign in with OAuth, `gcx` acts as your Grafana user. Your effective permissions are your own Grafana user permissions. OAuth sign-in doesn't add to them, so you can't do anything with `gcx` that you can't already do in the Grafana UI.

To use a product through `gcx`, your user needs the same permissions that a service account would need. Refer to [Grant permissions to a service account](#grant-permissions-to-a-service-account) for the permissions each product needs.

### Required role for OAuth sign-in

To authorize a `gcx` CLI connection with OAuth, your Grafana user needs the `grafana-assistant-app.tokens.gcx:access` permission. The gcx User role, registered by the Grafana Assistant application, grants this permission and is assigned automatically to users with the basic role Viewer or higher.

If your default role is `None`, or you're using a custom role that doesn't grant the default `plugins.app:access` action, you also need to explicitly grant access to `grafana-assistant-app`:

```json
{
  "action": "plugins.app:access",
  "scope": "plugins:id:grafana-assistant-app"
}
```

The `grafana-assistant-app.tokens.gcx:access` permission only lets you create `gcx` tokens for your own user. It doesn't grant access to other users' tokens and it doesn't extend your existing Grafana permissions.

If `gcx login` fails with a `Permission Required` error naming the gcx User role, ask your Grafana administrator to assign you the gcx User role, or a custom role that includes the `grafana-assistant-app.tokens.gcx:access` permission. If the role doesn't exist on your instance, update the Grafana Assistant application to a version that includes it.

## Grant permissions to a service account

When you sign in with a [service account token](../authentication#service-account-token), `gcx` uses the permissions of the service account. Grant the service account a role that matches what it needs to do, and add the product permissions listed in this section for the products you use.

### Dashboards, folders, and data sources

| To | The service account needs |
| --- | --- |
| Query metrics, logs, traces, and profiles, and read dashboards and folders | The Viewer basic role |
| Push and edit dashboards and folders | The Editor basic role |
| Manage data source configuration | The Admin basic role |

On Grafana Cloud and Grafana Enterprise, you can use RBAC custom roles to scope query access more tightly, for example `datasources:read` plus `datasources:query` on specific data sources.

### Alerting

`gcx alert` commands use the Grafana alerting APIs. The service account needs these permissions:

| Commands | Permission |
| --- | --- |
| View Grafana-managed alert rules, rule groups, and state history: `gcx alert rules`, `gcx alert groups`, `gcx alert state-history` | `alert.rules:read` |
| View data source-managed rules: `gcx alert ruler` | `alert.rules.external:read` on the data source |
| Create, update, or delete data source-managed rules: `gcx alert ruler upsert`, `gcx alert ruler delete` | `alert.rules.external:write` on the data source |
| View contact points, mute timings, templates, and notification policies | One of `alert.provisioning:read`, `alert.notifications.provisioning:read`, or `alert.notifications:read` |
| Create, update, or delete contact points, mute timings, templates, and notification policies | `alert.provisioning:write` or `alert.notifications.provisioning:write` |

### Fleet Management

`gcx fleet` commands reach Fleet Management through the Collector app on your stack, so they use your Grafana sign-in and don't need a Cloud Access Policy scope. The service account needs these Collector app permissions:

| Permission | Needed for |
| --- | --- |
| `grafana-collector-app:read` | The Collector app's named read routes. |
| `grafana-collector-app:admin`, or the Admin basic role | The Collector app's wildcard routes. Some read-only commands also use these routes, so `grafana-collector-app:read` alone might not be enough to read Fleet Management data. |

Looking up your stack's details through the Collector app only needs the Viewer role.

### Grafana Assistant

{{< admonition type="note" >}}
`gcx setup status` doesn't check Assistant access, so a passing status check doesn't mean Assistant commands work.
{{< /admonition >}}

The Assistant Investigation User role is included in the Editor and Admin basic roles. Viewers don't have it.

If an Assistant command such as `gcx assistant investigations list` fails, use the error message to find the missing permission:

| If the error mentions | You're missing | How to fix it |
| --- | --- | --- |
| A plugin route, for example `plugin proxy route access denied` | The error doesn't say which permission is missing. It might be access to the Assistant app: `plugins.app:access` scoped to `plugins:id:grafana-assistant-app`. | Check your actions with `gcx api /api/access-control/user/actions`, and send the output to your Grafana administrator. |
| `insufficient permissions`, or a named action such as `grafana-assistant-app.investigations:read` | Permission to read investigations: `grafana-assistant-app.investigations:read` | Ask your administrator to assign you the Assistant Investigation User role. |

#### View investigations created by alerts and webhooks

Investigations that start automatically, for example from an alert or an IRM webhook, need an additional permission: `grafana-assistant-app.investigations.system:read`. Without it, those investigations don't appear in your list.

#### Troubleshoot other Assistant commands

For commands that change things, such as creating investigations, or that call MCP servers, `gcx` can't always tell you which permission is missing. If the error doesn't name one, ask your administrator to review your Assistant roles.

### Frontend Observability

The `gcx frontend apps` commands that change apps need these Frontend Observability permissions:

| Commands | Action | Granted by |
| --- | --- | --- |
| `gcx frontend apps create`, `gcx frontend apps update` | `grafana-kowalski-app.apps:write` | The Frontend Observability Editor role |
| `gcx frontend apps delete`, `gcx frontend apps delete-sourcemap` | `grafana-kowalski-app.apps:delete` | The Frontend Observability Admin role, or the Admin basic role |

A user with only `grafana-kowalski-app.apps:write` can create and update apps, but can't delete apps or source map bundles.

### Grafana IRM

`gcx irm` commands go through the IRM plugin on your stack, so they use your Grafana sign-in and don't need a Grafana Cloud credential. The service account needs an IRM plugin role.

These core roles cover most use:

| Role | Access |
| --- | --- |
| `plugins:grafana-irm-app:reader` | Read-only access to everything in IRM. |
| `plugins:grafana-irm-app:oncaller` | Read access to everything in IRM, plus editing alert groups, schedules, and your own settings. |
| `plugins:grafana-irm-app:editor` | Like Admin, except creating integrations, escalation chains, and outgoing webhooks, and updating ChatOps, other users', and general IRM settings. |
| `plugins:grafana-irm-app:admin` | Read and write access to everything in IRM. |
| `plugins:grafana-irm-app:incident-access` | Access to IRM incidents. |

To grant narrower access, use the granular IRM roles for the commands you run:

| Commands | Read role | Write role |
| --- | --- | --- |
| `gcx irm oncall alert-groups` | `plugins:grafana-irm-app:alert-groups-reader` | `plugins:grafana-irm-app:alert-groups-editor` |
| `gcx irm oncall integrations` | `plugins:grafana-irm-app:integrations-reader` | `plugins:grafana-irm-app:integrations-editor` |
| `gcx irm oncall integrations start-maintenance`, `stop-maintenance` | `plugins:grafana-irm-app:maintenance-reader` | `plugins:grafana-irm-app:maintenance-editor` |
| `gcx irm oncall escalation-chains` | `plugins:grafana-irm-app:escalation-chains-reader` | `plugins:grafana-irm-app:escalation-chains-editor` |
| `gcx irm oncall schedules`, `gcx irm oncall shifts` | `plugins:grafana-irm-app:schedules-reader` | `plugins:grafana-irm-app:schedules-editor` |
| `gcx irm oncall webhooks` | `plugins:grafana-irm-app:outgoing-webhooks-reader` | `plugins:grafana-irm-app:outgoing-webhooks-editor` |

OnCall personal notification rules don't work with a service account: the server rejects service account requests. Sign in as a Grafana user instead. Refer to [Manage OnCall notification rules](../guides/manage-oncall-notification-rules) for the permissions those commands need.

### Synthetic Monitoring

`gcx sm` commands go through the Synthetic Monitoring data source on your stack, using your Grafana sign-in. The service account needs a Synthetic Monitoring plugin role:

| Role | Access |
| --- | --- |
| `plugins:grafana-synthetic-monitoring-app:reader` | Read checks, probes, alerts, thresholds, and access tokens. |
| `plugins:grafana-synthetic-monitoring-app:editor` | Add, update, and delete checks, probes, alerts, and thresholds. |
| `plugins:grafana-synthetic-monitoring-app:admin` | Everything Editor can do, plus managing access tokens and enabling or disabling the plugin. |

For narrower access, use the granular roles, such as `plugins:grafana-synthetic-monitoring-app:checks-reader` and `plugins:grafana-synthetic-monitoring-app:checks-writer`, or the equivalent `probes`, `alerts`, and `thresholds` roles.

If `gcx` can't use the data source, it calls the Synthetic Monitoring API directly. That path needs a Grafana Cloud credential to create a Synthetic Monitoring token. Refer to [Choose Grafana Cloud access policy scopes](#choose-grafana-cloud-access-policy-scopes).

### Grafana Cloud k6

When you sign in with OAuth, `gcx k6` commands go through the k6 plugin on your stack. Your user needs one of these k6 plugin roles:

| Role | Access |
| --- | --- |
| `plugins:k6-app:reader` | Read-only access to k6. |
| `plugins:k6-app:editor` | Read and write access to k6, with limited scopes. |
| `plugins:k6-app:admin` | Admin access to everything in k6. |

When you sign in with a service account token, `gcx` exchanges the token with the k6 API directly, and the token needs k6 access.

### SLO

`gcx slo` commands use the SLO plugin on your stack. SLO permissions follow folder permissions:

| Role | Access |
| --- | --- |
| `plugins:grafana-slo-app:slo-reader` | View SLOs in folders where you have folder read permission. |
| `plugins:grafana-slo-app:slo-writer` | Create, update, and delete SLOs in folders where you have folder edit permission. |
| `plugins:grafana-slo-app:slo-admin` | Everything SLO Writer can do, plus changing organization preferences. |

## Choose Grafana Cloud access policy scopes

Grafana Cloud sign-in is separate from your Grafana sign-in. It gives `gcx` a Grafana Cloud credential for commands that call Grafana Cloud APIs: `gcx cloud` commands, such as managing stacks, plus k6 (`gcx k6`) with a service account token, and Synthetic Monitoring (`gcx sm`) when it calls the Synthetic Monitoring API directly. Refer to [Grafana Cloud product API](../authentication#grafana-cloud-product-api) for how to add it.

You can sign in to Grafana Cloud in two ways:

- **Cloud Access Policy token:** recommended for automation. The scopes you give the access policy decide which commands work.
- **Grafana Cloud OAuth:** the experimental browser flow, with `gcx cloud login` or the Grafana Cloud step of `gcx login`. By default it requests `profile`, `stacks:read`, `stacks:write`, `stacks:delete`, `metrics:write`, `logs:write`, and `traces:write`. Not every Grafana Cloud product command accepts it yet.

If you use a [Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/), scope the access policy to the commands you use:

- **`stacks:read` is required**. It resolves your stack and is checked when you sign in and when `gcx` validates the token.
- Add the scopes for the products you use:

| Scope | Enables |
| --- | --- |
| `stacks:read` | Required. Stack discovery, which resolves the stack slug for all Grafana Cloud commands. Also covers `gcx k6` reads. |
| `metrics:write`, `logs:write`, `traces:write` | Synthetic Monitoring (`gcx sm`). `gcx` needs these scopes to create the Synthetic Monitoring token. |
| `stacks:write` | Creating or updating stacks. |
| `stacks:delete` | Deleting stacks (`gcx cloud stacks delete`). |
| `set:alloy-data-write`, `metrics:read` | Instrumentation Hub setup (`gcx instrumentation setup`). These scopes are for the token you add to the Helm command that `gcx instrumentation setup` prints. |

Fleet Management, IRM, and SLO commands, and signal queries such as `gcx metrics`, `gcx logs`, `gcx traces`, and `gcx profiles`, don't use the Grafana Cloud credential. They use your Grafana sign-in, either OAuth or a service account token.

If you're not sure which scopes you need, start with a narrow policy and add scopes as commands report missing-scope errors. You can change the policy's scopes without running `gcx login` again.

Refer to [Create access policies](https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies/) for how to create the policy and token.

## Fix an authorization error

When a command fails because you're missing a permission, it fails with `Authorization failed` and exit code 3. Often, the error names the action you're missing.

To fix an authorization error:

1. Note the action that the error names, if any.
1. List the actions your user is allowed to perform:

   ```bash
   gcx api /api/access-control/user/actions
   ```

1. Compare the output with the action the error names.
1. Send the output and the error message to your Grafana administrator. They can grant the missing action, or assign you a role that includes it.

Refer to [roles and permissions](https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/) for role assignment guidance.

## Permissions to check instrumentation status

`gcx setup status` and `gcx instrumentation clusters list` check Kubernetes monitoring through the Collector app. To run them, you need both `grafana-collector-app:read` and `grafana-collector-app:admin`.

`gcx setup status` reports `true` or `false` for each product it can check. `false` means `gcx` checked and the product isn't set up.

If `gcx` can't check a product, for example because of a permissions error, the state is **unknown**. Unknown can appear as `unknown` in a text table, or as `enabled: null` in JSON and YAML.

The `fleet-management` row also shows unknown when `gcx` can't read the state of the Collector app, because its status depends on that app.

### Use status checks in scripts and agents

When a check fails, the rest of the status document is still returned, with the same `type`, `schema_version`, and `products` fields. You can parse it as usual and treat `null` values as unknown.

To get the failure details in the output itself, run the command in agent mode or with `--json`. The output then includes an `error` object with these fields:

| Field | Description |
| --- | --- |
| `summary` | A short description of the failure. |
| `exitCode` | The exit code the command returns. |
| `details`, `suggestions`, `docsLink` | Optional. Present when `gcx` has more to say. |

These options remove the error object from the filtered output, so `gcx` also writes the error text to `stderr`:

- `-o yaml`, and `-o json` outside agent mode.
- `--jq`.
- `--json <fields>` when the field list doesn't include `error`.
- `--json list`.
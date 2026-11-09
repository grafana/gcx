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

Some `gcx` commands need additional Grafana permissions beyond the ones required to sign in. Use this page to find out which permissions a command needs and how to fix authorization errors.

For the permissions you need to sign in with OAuth, refer to [Required role for OAuth sign-in](../authentication#required-role-for-oauth-sign-in).

If you're signing in using a service account token and your user role needs permissions for querying, managing dashboards and folders, or managing data sources, refer to [Service account token](../authentication#service-account-token).

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

## Choose Cloud Access Policy scopes

Commands under `gcx sm`, `gcx k6`, and other Grafana Cloud product commands need a Grafana Cloud credential in addition to your Grafana sign-in. Refer to [Grafana Cloud product API](../authentication#grafana-cloud-product-api) for how to add it.

If you use a [Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/), scope the access policy to the products you manage:

- **`stacks:read` is required**. It resolves your stack and is checked when you sign in and when `gcx` validates the token.
- Add the scopes for the products you use:

| Scope | Enables |
| --- | --- |
| `stacks:read` | Required. Stack discovery, which resolves the stack slug for all Grafana Cloud commands. Also covers `gcx k6` reads. |
| `metrics:write`, `logs:write`, `traces:write` | Synthetic Monitoring (`gcx sm`). `gcx` needs these scopes to create the Synthetic Monitoring token. |
| `stacks:write` | Creating or updating stacks. |
| `stacks:delete` | Deleting stacks (`gcx cloud stacks delete`). |
| `set:alloy-data-write`, `metrics:read` | Instrumentation Hub setup (`gcx instrumentation setup`). These scopes are for the token you add to the Helm command that `gcx instrumentation setup` prints. |

`gcx fleet` and `gcx instrumentation` don't need a Cloud Access Policy scope to reach Fleet Management. They use the `grafana-collector-app` plugin on your stack, so your Grafana sign-in is enough.

The Cloud Access Policy token is only for Grafana Cloud product APIs: stack management, Synthetic Monitoring, and k6. IRM and SLO commands, and signal queries such as `gcx metrics`, `gcx logs`, `gcx traces`, and `gcx profiles`, use your Grafana sign-in, either OAuth or a service account token.

If you're not sure which scopes you need, start with a narrow policy and add scopes as commands report missing-scope errors. You can change the policy's scopes without running `gcx login` again.

Refer to [Create access policies](https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies/) for how to create the policy and token.

## Grant permissions to check instrumentation status

`gcx setup status` and `gcx instrumentation clusters list` check Kubernetes monitoring through the Collector app. To run them, your Grafana user needs both of these permissions:

- `grafana-collector-app:read`
- `grafana-collector-app:admin`

### Read the status when a check fails

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


## Grant Fleet Management permissions

`gcx fleet` commands reach Fleet Management through the Collector app on your stack, so they use your Grafana sign-in and don't need a Cloud Access Policy scope. Your Grafana user or service account needs these Collector app permissions:

| Permission | Needed for |
| --- | --- |
| `grafana-collector-app:read` | The Collector app's named read routes. |
| `grafana-collector-app:admin`, or the Admin basic role | The Collector app's wildcard routes. Some read-only commands also use these routes, so `grafana-collector-app:read` alone might not be enough to read Fleet Management data. |

Looking up your stack's details through the Collector app only needs the Viewer role.

## Grant Assistant investigation permissions

{{< admonition type="note" >}}
`gcx setup status` doesn't check Assistant access, so a passing status check doesn't mean Assistant commands work.
{{< /admonition >}}

The Assistant Investigation User role is included in the Editor and Admin basic roles. Viewers don't have it.

If an Assistant command such as `gcx assistant investigations list` fails, use the error message to find the missing permission:

| If the error mentions | You're missing | How to fix it |
| --- | --- | --- |
| A plugin route, for example `plugin proxy route access denied` | The error doesn't say which permission is missing. It might be access to the Assistant app: `plugins.app:access` scoped to `plugins:id:grafana-assistant-app`. | Check your actions with `gcx api /api/access-control/user/actions`, and send the output to your Grafana administrator. |
| `insufficient permissions`, or a named action such as `grafana-assistant-app.investigations:read` | Permission to read investigations: `grafana-assistant-app.investigations:read` | Ask your administrator to assign you the Assistant Investigation User role. |

### View investigations created by alerts and webhooks

Investigations that start automatically, for example from an alert or an IRM webhook, need an additional permission: `grafana-assistant-app.investigations.system:read`. Without it, those investigations don't appear in your list.

### Troubleshoot other Assistant commands

For commands that change things, such as creating investigations, or that call MCP servers, `gcx` can't always tell you which permission is missing. If the error doesn't name one, ask your administrator to review your Assistant roles.

## Grant Frontend Observability permissions

The `gcx frontend apps` commands that change apps need these Frontend Observability permissions:

| Commands | Action | Granted by |
| --- | --- | --- |
| `gcx frontend apps create`, `gcx frontend apps update` | `grafana-kowalski-app.apps:write` | The Frontend Observability Editor role |
| `gcx frontend apps delete`, `gcx frontend apps delete-sourcemap` | `grafana-kowalski-app.apps:delete` | The Frontend Observability Admin role, or the Admin basic role |

A user with only `grafana-kowalski-app.apps:write` can create and update apps, but can't delete apps or source map bundles.

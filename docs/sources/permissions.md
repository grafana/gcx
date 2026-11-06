---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/permissions/
title: Permissions
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 7
---

# `gcx` permissions

Some `gcx` commands need additinal Grafana permissions beyond the ones required to sign in. Use this page to find out which permissions a command needs and how to fix authorization errors.

For the permissions you need to sign in with OAuth, refer to [Required role for OAuth sign-in](../authentication#required-role-for-oauth-sign-in).

## Check which permissions you have

To list the actions your user is allowed to perform, run:

```bash
gcx api /api/access-control/user/actions
```

Send the output to your Grafana administrator along with any error message to modify them. Refer to [roles and permissions](https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/) for role assignment guidance.

## Permissions to check instrumentation status

`gcx setup status` and `gcx instrumentation clusters list` check Kubernetes monitoring through the Collector app. To run them, your Grafana user needs both of these permissions:

- `grafana-collector-app:read`
- `grafana-collector-app:admin`

If you're missing one, the command fails with `Authorization failed` and exit code 3.

To list the actions your user is allowed to perform, refer to [Check which permissions you have](#check-which-permissions-you-have).

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
- `--json list`

### Exit codes

`gcx setup status` is a read-only check, so it never exits with code 4.

| Exit code | Meaning |
| --- | --- |
| 3 | A probe failed with HTTP 401 or 403. Check your permissions. |
| 1 | Any other probe failed, for example HTTP 500. This applies even if the other products returned a result. |

## Permissions to use Assistant investigations

{{< admonition type="note" >}}
`gcx setup status` doesn't check Assistant access, so a passing status check doesn't mean Assistant commands will work.
{{< /admonition >}}

The **Assistant Investigation User** role is included in the Editor and Admin basic roles. Viewers don't have it.

If an Assistant command such as `gcx assistant investigations list` fails with `Authorization failed` and exit code 3, your Grafana user is missing a permission. The error message tells you which one:

| If the error mentions | You're missing | How to fix it |
| --- | --- | --- |
| A plugin route, for example `plugin proxy route access denied` | Access to the Assistant app: `plugins.app:access` scoped to `plugins:id:grafana-assistant-app` | Ask your Grafana administrator to grant this permission. |
| `insufficient permissions`, or a named action such as `grafana-assistant-app.investigations:read` | Permission to read investigations: `grafana-assistant-app.investigations:read` | Ask your administrator to assign you the **Assistant Investigation User** role. |

### Permissions for investigations created by alerts and webhooks

Investigations that start automatically, for example from an alert or an IRM webhook, need an additional permission: `grafana-assistant-app.investigations.system:read`. Without it, those investigations don't appear in your list.

### Other Assistant commands

For commands that change things, such as creating investigations, or that call MCP servers, `gcx` can't always tell you which permission is missing. If the error doesn't name one, ask your administrator to review your Assistant roles. 



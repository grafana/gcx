---
title: Manage OnCall notification rules
labels:
  products:
    - cloud
description: Manage personal OnCall notification rules with a Grafana user login.
keywords:
  - gcx
  - OnCall
  - notifications
weight: 8
---

# Manage OnCall personal notification rules

You can use `gcx` to manage OnCall personal notification rules

## Authentication and permissions

Use `gcx login` to sign in as a Grafana user. 

- The command uses the shared Grafana authentication transport and the IRM plugin proxy, and it doesn't need a separate OnCall API key. 
- Service account requests are rejected by the server.

The server requires `grafana-oncall-app.user-settings:read` for reads and `grafana-oncall-app.user-settings:write` for changes. Changes to another user's rules also require `grafana-oncall-app.user-settings:admin`.

## Manage OnCall notification rules 

To get a list of rules for all users and priority levels, run:

```bash
gcx irm oncall personal-notification-rules list
gcx irm oncall personal-notification-rules get <rule-id>
gcx resources list-examples personalnotificationrules
```

Manage rules with these commands:

- Create rules with `create -f rule.yaml`. 
- Update rules with `update <rule-id> -f rule.yaml`. 
- Delete rules with `delete <rule-id>`.

### Rule fields

**Do not use the public API's `type` or `duration` field names.** `step`, `notify_by`, and `wait_delay` use the internal IRM API values.

Rule fields such as `user` and `important` are set when the rule is created, and are ignored in updates.

Use a string of seconds for `wait_delay`, such as `"300.0"`, or `null`.

The server assigns its own rule ID at creation. Use the returned ID for later updates. For resource pushes, pull the created rule and keep that manifest before the next push. Its `metadata.name` contains the server ID. The server also controls `order`.

The API can also return `notify_device`. `gcx` preserves this server field in exports and updates. It's `null` when the rule has no selected device. 

### Routes

The **proxy route** is `notification_policies/`, and uses the same `UserNotificationPolicy` model as the public OnCall `/api/v1/personal_notification_rules/` route. 

The **public route** requires an OnCall API token in the raw `Authorization` header. A Grafana OAuth token is used through the plugin proxy instead.

The **internal route** defaults to the current user and `important=false` when query parameters are absent. 

`gcx` sends `?user=&important=` to disable these defaults. The backend then returns the full collection. The resource adapter uses the same client methods as the provider commands.

## Select a step and notification channel

Use these internal API values for `step`:

| Value | Step |
|-------|------|
| `0` | Wait for `wait_delay` seconds |
| `1` | Notify through `notify_by` |

A wait step returns `notify_by: null`. The default table shows a dash for that value and shows the wait duration in `WAIT-DELAY`.

### Notification channels

Notification channels depend on the stack and its integrations. Read the live catalog to get the available values and display names:

```bash
gcx api /api/plugins/grafana-irm-app/resources/notification_policies/notify_by_options/
```

Common `notify_by` values are:

| Value | Channel |
|-------|---------|
| `0` | Slack mentions |
| `1` | SMS |
| `2` | Phone call |
| `3` | Telegram |
| `5` | Mobile push |
| `6` | Mobile push important |
| `7` | Microsoft Teams |
| `8` | Email |
| `11` | Webhook |



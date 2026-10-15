---
title: Manage Assistant Watchers
description: Use gcx to create Assistant Watchers, update configuration, configure notifications, save reusable manifests, and delete Watchers in Grafana Cloud.
labels:
  products:
    - cloud
  stage: experimental
keywords:
  - gcx
  - assistant
  - watchers
weight: 100
---

# Manage Assistant Watchers

Use `gcx` to create Watchers and keep their configuration in version control. Watcher commands are experimental and require Grafana Cloud.

## Before you begin

- [Configure a gcx context](../../configuration/) for a Grafana Cloud stack with Assistant Watchers enabled.
- Make sure your identity can read and manage Watchers and access the data sources you want to monitor.

Commands use your selected context. Add `--context CONTEXT` to select another context.

## Create a Watcher

To create a Watcher, follow these steps:

1. Save the following manifest as `watcher.yaml`. Replace `DATASOURCE_UID` with an accessible data source UID.

   ```yaml
   apiVersion: assistant.ext.grafana.app/v1alpha1
   kind: Watcher
   metadata:
     name: checkout-health
   spec:
     title: Checkout health
     prompt: Watch request failures and sustained latency increases.
     datasourceUids: [DATASOURCE_UID]
     interval: 15m
     sensitivity: balanced
   ```

1. Create the Watcher:

   ```shell
   gcx assistant watchers create -f watcher.yaml -o json
   ```

   Creation saves an inactive, uncalibrated definition. Notification destinations and investigations are disabled unless you enable them in the manifest. Continue in Grafana to calibrate the Watcher and start monitoring.

## Inspect and update configuration

To change an existing Watcher, follow these steps:

1. Find its resource name or ID:

   ```shell
   gcx assistant watchers list
   ```

   Add `--archived` to list archived Watchers. If a name matches several Watchers, use an ID.

1. Save its configuration to a file:

   ```shell
   gcx assistant watchers get WATCHER -o yaml > watcher.yaml
   ```

1. Edit the file, then apply the complete manifest:

   ```shell
   gcx assistant watchers update WATCHER -f watcher.yaml
   ```

`update` and `resources push` replace the modeled configuration. Omitted optional settings receive defaults, including disabled notification destinations. Start from an exported manifest to retain your existing choices. These operations preserve calibrated checks and learned state, and do not request calibration, start monitoring, or pause it. If an edit is rejected during calibration, wait for it to finish or cancel it in Grafana.

If an apply reports a partial failure, changes may already be saved. Inspect the returned Watcher ID and failed stage, resolve the failure, and reapply with `update` or `resources push`.

Use `status` for calibration, checks, and runtime observations; `get` exports configuration only. A paused Watcher can retain an older health assessment.

Use Grafana for custom automatic-recalibration schedules and other settings gcx cannot represent. gcx refuses configuration writes that would drop those settings.

## Manage configuration files

To store and reapply Watcher configuration, follow these steps:

1. Export the manifests:

   ```shell
   gcx resources pull watchers -p ./watchers -o yaml
   ```

1. Edit the files and validate them without writing to Grafana:

   ```shell
   gcx resources push -p ./watchers --dry-run
   ```

1. Review your changes, then apply them:

   ```shell
   gcx resources push -p ./watchers
   ```

An unchanged apply sends no mutation unless you supply secret values. Push saves configuration only; calibrate new definitions in Grafana. Dry-run checks the manifest locally, so permissions, data source access, and other server checks still apply when you push.

## Configure Slack and Alerting notifications

To select a Slack recipient and an Alerting notification policy tree, follow these steps:

1. Add the following under `spec` in your complete Watcher manifest. Replace the example IDs and policy tree with your destination values:

   ```yaml
   notifications:
     slack:
       enabled: true
       targetType: dm
       userId: U_EXAMPLE
       teamId: T_EXAMPLE
       severity: critical
     alerting:
       enabled: true
       severity: critical
       routingTree: example-policy-tree
   ```

   For a Slack channel, use `targetType: channel` and `channelId` instead of `userId`. `teamId` is optional. Enabled channels require a nonblank channel ID; direct messages require a nonblank user ID even when disabled.

1. Apply the complete manifest with `gcx assistant watchers update WATCHER -f watcher.yaml`.
1. Verify the saved destinations with `gcx assistant watchers get WATCHER -o yaml`.

Notification severity defaults to `warning-and-critical`. Set `routingTree: ""` to use the default notification policy tree. Manage notification policy trees and contact points in Grafana Alerting.

## Supply webhook secrets

Use `fromEnv` or `fromFile` to supply a secret, for example `url: { fromEnv: WATCHER_WEBHOOK_URL }`. Exports use `{ preserve: true }` for saved secrets; omitting a secret on update also preserves it. Use `{ clear: true }` to remove one. Preserve markers cannot be used when creating a Watcher.

When supplying or clearing a webhook URL, also supply or clear every saved bearer token and signing secret. Keep resolved secrets out of manifests and version control.

## Delete a Watcher

To remove a Watcher, run the following command and confirm deletion:

```shell
gcx assistant watchers delete WATCHER
```

Use `--force` only when you want to skip confirmation.

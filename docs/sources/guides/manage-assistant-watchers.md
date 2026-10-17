---
title: Manage Assistant Watchers
description: Use gcx to create Assistant Watchers, follow calibration, review and update configuration, save reusable manifests, and delete Watchers in Grafana Cloud.
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

Use `gcx` to create Watchers and keep their configuration in version control. **Watcher commands are experimental and require Grafana Cloud**.

## Before you begin

- [Configure a gcx context](../../configuration/) for a Grafana Cloud stack with Assistant Watchers enabled.
- Make sure your identity can read and manage Watchers and access the data sources you want to monitor.

Commands use your selected context. Add `--context CONTEXT` to select another context.

## Create a Watcher and follow calibration

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

   The result includes the saved resource and a separate calibration request outcome. Creation requests background calibration without automatically starting monitoring. Notification destinations and investigations are disabled unless you enable them in the manifest.

1. Replace `WATCHER` with the returned resource name or ID and inspect progress:

   ```shell
   gcx assistant watchers status WATCHER -o json
   ```

   An `accepted` request means calibration was requested, not that checks are ready. Wait while it is queued or running. If calibration fails, is cancelled, or needs input, continue in Grafana. When it completes, review the enabled checks and lifecycle in Grafana before explicitly starting monitoring.

If the request outcome is `unavailable`, calibrate the saved Watcher in Grafana. An `error` or `not-requested` outcome returns exit code 4 and retains the Watcher identity. Inspect the reported failure and status; resolve any failed configuration stage with `update`, then continue in Grafana. Do not repeat `create`: a request error can leave calibration running.

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

## Supply webhook secrets

Use `fromEnv` or `fromFile` to supply a secret, for example `url: { fromEnv: WATCHER_WEBHOOK_URL }`. Exports use `{ preserve: true }` for saved secrets; omitting a secret on update also preserves it. Use `{ clear: true }` to remove one. You can't use preserve markers when creating a Watcher.

When supplying or clearing a webhook URL, also supply or clear every saved bearer token and signing secret. Keep resolved secrets out of manifests and version control.

## Delete a Watcher

To remove a Watcher, run the following command and confirm deletion:

```shell
gcx assistant watchers delete WATCHER
```

Use `--force` only when you want to skip confirmation.

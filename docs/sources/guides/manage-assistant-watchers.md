---
title: Manage Assistant Watchers
description: Use gcx to create Assistant Watchers, follow calibration, review and update configuration, save reusable manifests, control monitoring, inspect run history, restore definition versions, and delete Watchers in Grafana Cloud.
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

   An `accepted` request means calibration was requested, not that checks are ready. Wait while it is queued or running. If calibration fails, is cancelled, or needs input, continue in Grafana. When it completes, review the enabled checks in Grafana before [starting monitoring](#start-and-pause-monitoring).

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

## Start and pause monitoring

After calibration completes with at least one enabled check, start your Watcher:

```shell
gcx assistant watchers start WATCHER
```

Starting can immediately run a scan; event-only Watchers wait for their trigger. Both start and run can send configured notifications or launch investigations. If start fails, inspect `gcx assistant watchers status WATCHER` before retrying: monitoring might already be active.

To stop future scans, pause the Watcher:

```shell
gcx assistant watchers pause WATCHER
```

Finish or cancel calibration in Grafana before pausing. An in-flight scan can still finish and send notifications or launch investigations.

## Request a run and inspect history

Request a scan without changing the schedule, then inspect recent executions:

```shell
gcx assistant watchers run WATCHER -o json
gcx assistant watchers list-runs WATCHER --limit 20 -o json
```

The run receipt reports acceptance, skipped work, or failure. Acceptance doesn't mean the scan completed. Its `taskId` can't be matched to history IDs; don't assume the newest execution belongs to your request. Running might require create permission in addition to write permission.

History shows execution status separately from assessment. The default limit is 20, and the maximum is 100. `coverage.hasMore` indicates additional retained history; results aren't a snapshot of all past runs. For full run evidence, open the Watcher in Grafana.

## Archive and recover a Watcher

Pause before archiving to retain the Watcher's configuration and history:

```shell
gcx assistant watchers pause WATCHER
gcx assistant watchers archive WATCHER
```

Archive only accepts a Watcher whose status is `paused`; pause it first. If it is calibrating, finish or cancel calibration in Grafana before pausing. Keep other clients from starting it during this sequence: the pause check and archive aren't atomic. Archiving can resolve active Grafana Alerting findings.

Recover an archived Watcher without activating monitoring:

```shell
gcx assistant watchers unarchive WATCHER
```

Review its status and automatic-stop deadline before starting again. Recovery retains that deadline. Archive recovery doesn't restore a previous definition version.

## Compare definition versions

To inspect saved definitions before restoring one, follow these steps:

1. List retained versions:

   ```shell
   gcx assistant watchers list-versions WATCHER --limit 20 -o json
   ```

   Results contain one page. If `coverage.nextBeforeVersion` is present, pass it as `--before-version` to read the next page. Server retention can exclude older versions.

1. Read the definition you want to inspect:

   ```shell
   gcx assistant watchers versions get WATCHER VERSION -o yaml
   ```

1. Compare two definitions:

   ```shell
   gcx assistant watchers versions diff WATCHER FROM_VERSION TO_VERSION -o json
   ```

   The diff compares definitions only. Add `--json path,operation` to show changed paths and operations while retaining the compared versions and scope.

## Restore a definition version

Restore creates a new current definition version. It requires a non-archived Watcher in `draft`, `paused`, or `ready` state. It retains current notification actions and settings outside the definition; it doesn't copy historical learned state.

1. Inspect the Watcher's lifecycle and current version:

   ```shell
   gcx assistant watchers status WATCHER -o json
   ```

   For a running Watcher, run `gcx assistant watchers pause WATCHER` first. Finish or cancel calibration in Grafana before restoring.

1. Replace `CURRENT_VERSION` with the version reported by `status`, then restore the selected `VERSION` and confirm:

   ```shell
   gcx assistant watchers versions restore WATCHER VERSION --expected-current-version CURRENT_VERSION -o json
   ```

   A stale guard is rejected. Inspect status and compare again before choosing a new guard.

The restored Watcher stays inactive and can become a draft that needs calibration. You can restore another version from that draft without pausing it. Review it in Grafana before starting monitoring. If the write outcome is unknown, inspect `status` and `list-versions` before retrying. A confirmed save followed by failed verification returns exit code 4; inspect the saved version instead of repeating the restore.

## Delete a Watcher

To remove a Watcher, run the following command and confirm deletion:

```shell
gcx assistant watchers delete WATCHER
```

Use `--force` only when you want to skip confirmation.

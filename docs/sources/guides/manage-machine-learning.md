---
title: Manage Machine Learning resources
labels:
  products:
    - cloud
description: Manage forecasts, holidays, outlier detectors, and their alerts with gcx.
keywords:
  - gcx
  - machine learning
  - forecasting
weight: 8
---

# Manage Machine Learning resources

Use `gcx ml` to manage Grafana Cloud Machine Learning resources.
The commands use the active Grafana credential through the ML plugin proxy.
You do not need a separate provider token.
The Grafana stack must have Machine Learning enabled.

## Inspect resources

Forecasts are jobs in the ML API.
Use the server IDs from the list output with `get`, `update`, and `delete`.

```shell
gcx ml jobs list
gcx ml holidays list
gcx ml outlier-detectors list
gcx ml jobs get <job-id> -o yaml
```

Lists include all records from the collection endpoint.
Use `--json` or `--jq` to select output fields.

## Apply a resource

Use a resource example to create a manifest.
Set the datasource UID and the query for your target stack.

```shell
gcx resources list-examples jobs.ml.ext.grafana.app -o yaml > job.yaml
gcx ml jobs create -f job.yaml --dry-run
gcx ml jobs create -f job.yaml
gcx ml jobs update <job-id> -f job.yaml
```

`--dry-run` checks the input without a server mutation.
The result reports that server validation is not available.
Delete commands require confirmation or `--force`.

## Export and push manifests

Jobs, holidays, and outlier detectors use the standard resource pipeline.
The provider removes server IDs and connection fields from each spec.
Each write uses the target stack URL.
`metadata.name` contains the server ID from the source stack.
If that ID does not exist on the target, the pipeline matches `spec.name`.
It updates one matching resource or creates a resource when there is no match.
It rejects multiple matches.
After a cross-stack push, pull from the target to save its assigned IDs.

```shell
gcx resources pull jobs.ml.ext.grafana.app holidays.ml.ext.grafana.app outlierdetectors.ml.ext.grafana.app -p ./ml
gcx resources push -p ./ml --dry-run
gcx resources push -p ./ml
```

Datasource UIDs and job-to-holiday references remain in the spec.
Set these references for the target stack before a cross-stack push.

## Manage parent alerts

ML alerts belong to a job or an outlier detector.
They have no independent collection endpoint or resource adapter.
Use the parent ID with the alert commands.

```shell
gcx ml jobs list-alerts <job-id>
gcx ml jobs get-alert <job-id> <alert-id> -o yaml
gcx ml jobs create-alert <job-id> -f alert.yaml
gcx ml jobs update-alert <job-id> <alert-id> -f alert.yaml
gcx ml jobs delete-alert <job-id> <alert-id> --force
gcx ml outlier-detectors list-alerts <detector-id>
```

The same alert operations are available under `outlier-detectors`.
Alert files contain the ML alert object, including its `title`.
Use `get-alert` to export an alert object before you edit it.

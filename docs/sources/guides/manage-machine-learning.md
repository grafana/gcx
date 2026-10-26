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

## Prerequisites 

The following applies:

- Your Grafana stack must have [Machine Learning](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/) enabled.
- You don't need a separate provider token. The commands use the active Grafana credential through the Machine Learning plugin proxy.

## Inspect resources

Forecasts are jobs in the Machine Learning API.

Use the server IDs from the list output with `get`, `update`, and `delete`.

```shell
gcx ml jobs list
gcx ml holidays list
gcx ml outlier-detectors list
gcx ml jobs get <job-id> -o yaml
```

Use `--json` or `--jq` to select output fields.

### Configure the number of records in the output

Lists fetch all records from the collection endpoint. Agent output shows all records, while human output shows up to 50 records by default:

- Use `--limit 0` to show all records.
- Use `--limit 10` to show fewer records. A truncation notice appears on `stderr` when records are omitted.

## Apply a resource

Use a resource example to create a manifest. Set the datasource UID and the query for your target stack:

```shell
gcx resources list-examples jobs.ml.ext.grafana.app --jq '.["ml.ext.grafana.app"].v1alpha1[0].example' -o json > job.json
gcx ml jobs create -f job.json --dry-run
gcx ml jobs create -f job.json
gcx ml jobs update <job-id> -f job.json
```

> Creating or updating a forecast job can start model training.

`--dry-run` checks the input without a server mutation. The result reports that server validation is not available.

### Delete a resource

Delete commands require confirmation or `--force`.

## Export and push manifests

Jobs, holidays, and outlier detectors use the standard resource pipeline. The provider removes server IDs and connection fields from each spec. 

### Target stack

Each write uses the target stack URL. The server ID from the source stack is stored in `metadata.name`. If that ID doesn't exist on the target, the pipeline matches the normalized value of `spec.name`. 

Each write resolves the numeric datasource ID from the UID on the target stack.
Writes need read access to the selected datasource.

Matching: 

- Converts names to lowercase slugs.
- Updates one matching resource or creates a resource when there is no match.
- Multiple matches are rejected.

### Push cross-stack

Data source UIDs and job-to-holiday references remain in the spec. Set these references for the target stack before a cross-stack push.

After a cross-stack push, pull from the target to save its assigned IDs:

```shell
gcx resources pull jobs.ml.ext.grafana.app holidays.ml.ext.grafana.app outlierdetectors.ml.ext.grafana.app -p ./ml
gcx resources push -p ./ml --dry-run
gcx resources push -p ./ml
```

## Manage parent alerts

Machine Learning alerts belong to a job or an outlier detector, and have no independent collection endpoint or resource adapter.
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

### Alert objects

Alert files contain the Machine Learning alert object, including its `title`.

Use `get-alert` to export an alert object before you edit it.

### Outlier alerts

Outlier alerts require a nonzero `window`, such as `5m`. Don't set `threshold` or `anomalyCondition` for an outlier alert.

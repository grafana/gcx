---
aliases:
  - ../guides/manage-machine-learning/ # /docs/grafana/<GRAFANA_VERSION>/ai-tools/grafana-cli/gcx/guides/manage-machine-learning/
title: Manage Machine Learning resources as code
menuTitle: Manage Machine Learning as code
labels:
  products:
    - cloud
description: Pull and push forecasts, holidays, and outlier detectors as manifests with gcx, including across stacks.
keywords:
  - gcx
  - machine learning
  - forecasting
weight: 5
---

# Manage Machine Learning resources as code

Use `gcx resources pull` and `gcx resources push` to manage Grafana Cloud Machine Learning forecasts, holidays, and outlier detectors as manifests, and to copy them between stacks.

To list, inspect, create, update, and delete Machine Learning resources and their alerts with `gcx ml`, refer to [Use gcx to manage Machine Learning resources](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/additional-configuration/gcx/).

## Before you begin

- Your Grafana stack must have [Machine Learning](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/) enabled.
- You don't need a separate provider token. The commands use the active Grafana credential through the Machine Learning plugin proxy.

## Start from an example manifest

To create a manifest, start from a resource example. Set the data source UID and the query for your target stack:

```shell
gcx resources list-examples jobs.ml.ext.grafana.app --jq '.["ml.ext.grafana.app"].v1alpha1[0].example' -o json > job.json
gcx ml jobs create -f job.json --dry-run
```

`--dry-run` checks the input without a server mutation. The result reports that server validation is not available.

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
gcx resources push -p ./ml --dry-run
gcx resources push -p ./ml
gcx resources pull jobs.ml.ext.grafana.app holidays.ml.ext.grafana.app outlierdetectors.ml.ext.grafana.app -p ./ml
```

## Alerts aren't part of the pipeline

Machine Learning alerts belong to a job or an outlier detector, and have no independent collection endpoint or resource adapter. `gcx resources pull` and `push` don't include them. Use the `gcx ml jobs` and `gcx ml outlier-detectors` alert commands with the parent ID instead. For the full list of alert commands, refer to [Manage alerts](https://grafana.com/docs/grafana-cloud/ai-tools/machine-learning/additional-configuration/gcx/#manage-alerts) in the Grafana Cloud Machine Learning guide.

Alert files contain the Machine Learning alert object, including its `title`. Use `get-alert` to export an alert object before you edit it:

```shell
gcx ml jobs get-alert <job-id> <alert-id> -o yaml > alert.yaml
gcx ml jobs update-alert <job-id> <alert-id> -f alert.yaml
```

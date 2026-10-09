---
title: Explore and modify resources
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Use gcx to explore and modify resources.
keywords:
  - gcx
  - Grafana CLI
  - observability as code
  - telemetry
weight: 1
---

# Explore and modify resources with `gcx`

The Grafana CLI allows you to interact with Grafana resources directly from your terminal. You can browse, inspect, update, and delete resources without using the Grafana UI.

Use this approach if you're an advanced user and want to manage resources more efficiently, or to integrate Grafana operations into automated workflows.

## Find and delete dashboards using incorrect data sources

To identify and remove production dashboards relying on non-production data sources, use the command below to list dashboard UIDs along with the data source UIDs used in their panels:

```shell
gcx resources get dashboards --context prod | jq '.items | map({ uid: .metadata.name, datasources: .spec.panels | map(.datasource.uid)  })'
[
   {
      "uid": "important-production-dashboard",
      "datasources": [
         "mimir-prod"
      ]
   },
   {
      "uid": "test-dashboard-from-dev",
      "datasources": [
         "mimir-dev"
      ]
   },
   {
      "uid": "test-dashboard-from-stg",
      "datasources": [
         "mimir-stg"
      ]
   }
]
```

Next, identify the dashboards that are using unexpected data sources, and delete them:

```shell
gcx resources delete dashboards/test-dashboard-from-stg,test-dashboard-from-dev
✔ 2 resources deleted, 0 errors
```

## Edit remote resources

You can edit resources directly from the default editor, without having to pull them first:

```shell
gcx resources edit dashboard/edit-me-please
```

This command opens the default editor as configured by the `EDITOR` environment variable, or fall back to 'vi' for Linux or 'notepad' for Windows.

After the editor process terminates, the resource is updated in the Grafana instance targeted by the current context. Note that edition will be cancelled if no changes are written to the file or if the file after edition is empty.

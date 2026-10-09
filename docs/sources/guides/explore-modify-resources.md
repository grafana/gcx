---
title: Explore and modify resources
---

This section describes how to use the Grafana CLI to interact with Grafana resources directly from your terminal.

These commands allow you to browse, inspect, update, and delete resources without using the Grafana UI.

This approach is useful for advanced users who want to manage resources more efficiently or integrate Grafana operations into automated workflows.

## Find and delete dashboards using incorrect data sources

In this example, we want to identify and remove production dashboards relying on non-production data sources.

The command below lists dashboard UIDs along with the data source UIDs used in their panels:

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

We can then identify the dashboards that are using unexpected data sources, and delete them:

```shell
gcx resources delete dashboards/test-dashboard-from-stg,test-dashboard-from-dev
✔ 2 resources deleted, 0 errors
```

## Edit remote resources

Resources can be edited directly from the default editor, without having to pull them first:

```shell
gcx resources edit dashboard/edit-me-please
```

This command will open the default editor as configured by the `EDITOR` environment variable (or fall back to 'vi' for Linux or 'notepad' for Windows).

Once the editor process terminates, the resource will be updated in the Grafana instance targeted by the current context.

!!! note
    The edition will be cancelled if no changes are written to the file or if the file after edition is empty.

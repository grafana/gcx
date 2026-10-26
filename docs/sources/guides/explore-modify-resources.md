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
weight: 2
---

# Explore and modify resources with `gcx`

The Grafana CLI allows you to interact with Grafana resources directly from your terminal. You can browse, inspect, update, and delete resources without using the Grafana UI.

Use this approach if you're an advanced user and want to manage resources more efficiently, or to integrate Grafana operations into automated workflows.

## Search resources by relevance

Use the experimental `gcx resources search` command to find ranked matches from a phrase:

```shell
gcx resources search "kubernetes memory problems"
```

This command requires Grafana's global hybrid search endpoint to be enabled.
On supported Grafana Cloud stacks, it can combine text and semantic matching.
On-premises deployments with the endpoint use text matching only; semantic matching isn't available on-premises.
Your deployment must have default resource types, or you must select types with `--resource`.

Select resource types and exact folder UIDs when you need a narrower search:

```shell
gcx resources search "memory" \
  --resource dashboards.dashboard.grafana.app \
  --resource folders.folder.grafana.app \
  --folder team-platform \
  --limit 10
gcx resources search "memory" --folder general
```

Repeat `--resource` to search your selected types instead of the server defaults.
Repeat `--folder` to match any listed folder UID.
Folders match exactly, without descendants; `general` or `--folder ''` selects the root folder.
The default limit is 50, and the maximum is 200.

Use `-o wide` to see matching text, or `-o json` or `-o yaml` for the full response:

```shell
gcx resources search "memory" -o wide
gcx resources search "memory" -o json
```

Scores rank matches in the response; they aren't percentages.
Search returns top matches, without pagination or an exhaustive resource inventory.

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

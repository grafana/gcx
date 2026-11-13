---
title: Resource management workflows
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Use gcx to search, inspect, edit, delete, pull, and push resources.
keywords:
  - gcx
  - Grafana CLI
  - observability as code
  - telemetry
aliases:
  - ./explore-modify-resources/ # /docs/grafana/<GRAFANA_VERSION>/as-code/observability-as-code/grafana-cli/gcx/guides/explore-modify-resources/
weight: 2
---

# Resource management workflows with `gcx`

`gcx` allows you to interact with Grafana resources directly from your terminal. You can browse, inspect, update, and delete resources without using the Grafana UI, and move them between environments as files.

Use this approach if you're an advanced user and want to manage resources more efficiently, or to integrate Grafana operations into automated workflows.

## Available commands

The `gcx resources` command group includes the following commands:

| Command | Use it to | Refer to |
| --- | --- | --- |
| `gcx resources list-types` | List the resource types your Grafana instance serves | [Discover resource types](#discover-resource-types) |
| `gcx resources list-examples` | Show example manifests for provider-backed resource types | [Discover resource types](#discover-resource-types) |
| `gcx resources search` | Find resources by relevance to a phrase (experimental) | [Search resources by relevance](#search-resources-by-relevance) |
| `gcx resources get` | Fetch resources from Grafana and display them | [Find and delete dashboards using incorrect data sources](#find-and-delete-dashboards-using-incorrect-data-sources) |
| `gcx resources delete` | Delete resources by selector or from local files | [Find and delete dashboards using incorrect data sources](#find-and-delete-dashboards-using-incorrect-data-sources) |
| `gcx resources edit` | Edit a remote resource in your default editor | [Edit remote resources](#edit-remote-resources) |
| `gcx resources validate` | Check local files against a Grafana instance without writing them | [Validate and preview changes](#validate-and-preview-changes) |
| `gcx resources pull` | Save resources from Grafana to local files | [Migrate resources between environments](#migrate-resources-between-environments), [Back up and restore resources](#back-up-and-restore-resources) |
| `gcx resources push` | Create or update resources in Grafana from local files | [Validate and preview changes](#validate-and-preview-changes), [Migrate resources between environments](#migrate-resources-between-environments) |

For every flag of each command, refer to the [CLI reference](../../cli-reference/#gcx-resources).

## Discover resource types

To list the resource types that your Grafana instance serves, including provider-backed types such as SLOs and Synthetic Monitoring checks, run:

```shell
gcx resources list-types
```

This command requires a connection to Grafana. It also fetches each type's schema, so add `--no-schema` for faster results. Pass a selector to filter the list, and use `-o wide`, `-o json`, or `-o yaml` for more detail:

```shell
gcx resources list-types incidents -o json
```

To write a manifest from scratch, start from an example. Without arguments, `list-examples` lists every resource type that has an example. Pass a selector to show the examples for that type:

```shell
gcx resources list-examples
gcx resources list-examples slo -o yaml
```

## Search resources by relevance

Use the experimental `gcx resources search` command to find ranked matches from a phrase:

```shell
gcx resources search "kubernetes memory problems"
```
Search returns top matches, without pagination or an exhaustive resource inventory. Scores rank matches in the response. 

### Prerequisites

You need to enable the Grafana global hybrid search endpoint to use `gcx resources search`.

- On supported Grafana Cloud stacks, `gcx resources search` can combine text and semantic matching.
- On self-managed deployments with the endpoint, only use text matching. Semantic matching isn't available on-premises.

Your deployment must have default resource types, or you must select types with `--resource`.

### Search for specific resources

Select resource types and exact folder UIDs for a narrower search:

```shell
gcx resources search "memory" \
  --resource dashboards.dashboard.grafana.app \
  --resource folders.folder.grafana.app \
  --folder team-platform \
  --limit 10
gcx resources search "memory" --folder general
```

Repeat `--resource` to search your selected types instead of the server defaults.

Repeat `--folder` to match any listed folder UID. Folders match exactly, without descendants; `general` or `--folder ''` selects the root folder. The default limit is 50, and the maximum is 200.

Use `-o wide` to see matching text, or `-o json` or `-o yaml` for the full response:

```shell
gcx resources search "memory" -o wide
gcx resources search "memory" -o json
```

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

You can edit resources directly from the default editor, without having to pull them first. To do so, run:

```shell
gcx resources edit dashboard/edit-me-please
```

This command opens the default editor set in the `EDITOR` environment variable, or falls back to 'vi' for Linux and 'notepad' for Windows.

After the editor process terminates, the resource is updated in the Grafana instance targeted by the current context. Edition is cancelled if no changes are written to the file, or if the file after edition is empty.

## Validate and preview changes

Before you push local files, check them against the target Grafana instance. `gcx resources validate` reads resources from disk and asks Grafana to validate each one, without creating or updating anything:

```shell
gcx resources validate --path ./resources
gcx resources validate dashboards folders -o json
```

To preview the outcome of a push, add `--dry-run`:

```shell
gcx resources push --path ./resources --dry-run
```

Both commands use server-side dry-run. Resources that aren't known to support server-side dry-run aren't sent, and the summary reports them as skipped instead of verified. If you know a resource honors server-side dry-run, add it with `--assume-server-dry-run`, for example `--assume-server-dry-run alertrules.rules.alerting.grafana.app`.

## Migrate resources between environments

Grafana resources can be migrated from one environment to another, for example from a development to production environment. 

You need to [configure one context per environment](../../configuration#define-contexts-to-use-different-instances). In this example scenario, use `dev` for the development environment and `prod` for production.

1. Make changes to dashboards and other resources using the Grafana UI in your **development instance**.
1. Pull those resources from the development environment to your local machine:
   ```shell
   gcx resources pull --context dev # Add `-o yaml` export resources as YAML 
   ```
1. Push the resources to production:
   ```shell
   gcx resources push --context prod
   ```

{{< admonition type="note" >}}
Resources are pulled and pushed from the `./resources` directory by default.
Configure this path with the `--path`/`-p` flags.
{{< /admonition >}}

### Keep references valid across environments

`gcx` moves the resource envelope between environments: it removes server-managed fields on pull and sets the target namespace on push. It does not rewrite what a resource refers to. A dashboard that names a datasource UID that does not exist in the target environment pushes successfully and then fails to load its panels.

To keep references valid:

- Give each datasource the same UID in every environment.
- Select datasources in dashboards through a datasource template variable instead of a fixed UID.

## Name pulled files by title

By default, `gcx resources pull` names each file after the resource's `metadata.name`, which is usually a generated UID such as `adkg9x2`. These names are hard to recognize when you browse a directory or review a Git diff.

To name files after the resource title instead, add `--filename-by=title`:

```shell
gcx resources pull dashboards --filename-by=title --path ./dashboard-backup
```

For example, a dashboard titled `Ventas Overview` is saved as `ventas-overview.json` instead of `adkg9x2.json`. Only the filename changes: the file still contains the original `metadata.name`, so `push` and `validate` treat it exactly like a file pulled by name.

### How titles become filenames

`gcx` converts the title to lowercase letters, digits, and hyphens, and limits it to 100 characters. If a resource has no usable title, `gcx` uses its `metadata.name` instead.

If two resources produce the same filename, for example two dashboards titled `Overview`, `gcx` appends each resource's `metadata.name` to keep them apart, such as `overview-adkg9x2.json` and `overview-b71fq0m.json`. `gcx` never overwrites a file that belongs to a different resource. For the complete rules, refer to the [file naming rules](https://github.com/grafana/gcx/blob/main/docs/design/naming.md#92-file-naming).

### Remove stale files before you push

`gcx resources pull` writes files but never deletes them. If a resource is renamed, or you switch between `--filename-by=name` and `--filename-by=title`, the directory can end up with two files for the same resource: the old one and the new one.

When you pull with `--filename-by=title`, `gcx` warns on `stderr` about files that hold the same resource under a different name. A pull in name mode does not check for this. Before you push, remove the outdated files, for example with `git rm`, or pull into an empty directory.

## Back up and restore resources

Follow this workflow to back up all Grafana resources from one instance and later restore them. This can be useful to replicate a configuration or perform disaster recovery.

1. Ensure the current context points to the Grafana instance to backup/restore:
   ```shell
   gcx config use-context YOUR_CONTEXT  # for example "prod"
   ```
1. Pull all resources from your target environment:
   ```shell
   gcx resources pull --path ./backup-prod # Add `-o yaml` to export resources as YAML
   ```
1. Save the exported resources to version control or cloud storage.
1. Push the resources to restore them:
   ```shell
   gcx resources push --path ./backup-prod
   ```

## Incident descriptions

Incident descriptions remain visible in `gcx resources get` and
`gcx irm incidents list` results.

`gcx resources pull` omits descriptions from saved manifests. These backups
cannot restore incident descriptions.

Old backup files can contain `spec.description`. Remove that field before you
push the file to create an incident or to change an incident with a different
description. You can update other fields when `spec.description` is absent or equal to the current value.

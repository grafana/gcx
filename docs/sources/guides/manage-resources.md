---
title: Manage resources
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Use gcx to manage resources.
keywords:
  - gcx
  - Grafana CLI
  - observability as code
  - telemetry
weight: 3
---

# Manage resources with `gcx`

## Migrate resources between environments

Grafana resources can be migrated from one environment to another, for example from a development to production environment. 

You need to [configure one context per environment](../../configuration#define-contexts). In this example scenario, use `dev` for the development environment and `prod` for production.

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

By default, `gcx resources pull` names each file using the resource's
`metadata.name` (usually its UID). Use `--filename-by=title` for readable filenames:

```shell
gcx resources pull dashboards --filename-by=title --path ./dashboard-backup
```

For example, a dashboard titled `Ventas Overview` becomes `ventas-overview.json`
inside its usual resource-type directory. Resources without a usable title fall
back to `metadata.name`. The resource contents retain their original identity,
so push, validate, and serve use these files normally.

Title mode resolves filename collisions with both the current pull and existing
destination files, preserving files that belong to other resources. See the
[file naming rules](https://github.com/grafana/gcx/blob/main/docs/design/naming.md#92-file-naming)
for the full algorithm.

Pull does not remove old files. Title-mode pull warns on stderr when it finds
another file for the same resource at a different path, such as after a rename or
switching naming modes. Reading resources also warns when skipping duplicate
identities. Pull into a clean directory or remove stale files (for example, with
`git rm`) before pushing.

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

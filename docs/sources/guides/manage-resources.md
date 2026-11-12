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

Title mode checks filename conflicts against the current pull and existing
destination files. It preserves files that belong to other resources. See the
[file naming rules](https://github.com/grafana/gcx/blob/main/docs/design/naming.md#92-file-naming)
for the full algorithm.

`gcx resources pull` does not remove old files. Title-mode pull warns on stderr
when it finds another file for the same resource at a different path, such as
after a rename or a change of naming mode. Reading resources also warns when skipping duplicate
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

## Cluster-scoped resources

Some resource types are cluster-scoped. These types do not belong to a namespace.
For example, the `kgatlas.ext.grafana.app` group serves the cluster-scoped type `SystemKgSchema`.
To see the scope of each type, run:

```shell
gcx resources list-types -o wide
```

The `SCOPE` column shows `Namespaced` or `Cluster`.
The JSON and YAML outputs show the same value in the `scope` field.
When `gcx` fetches schemas, which it does by default, the JSON and YAML outputs leave out each type that has no OpenAPI schema.
To list every type, add `--no-schema`.

You can read cluster-scoped types with `gcx resources get` only:

- `gcx resources get` reads a cluster-scoped type when you name it, for example `gcx resources get systemkgschemas`. Without a resource selector, it includes cluster-scoped types.
- `gcx resources pull` does not include cluster-scoped types, because they are not part of a backup of your stack. Without a resource selector, it skips them. If a selector names a cluster-scoped type, the command fails with exit code 2.
- `gcx resources push`, `gcx resources edit` and `gcx resources delete` refuse cluster-scoped types and send no change to Grafana.

The app that serves a type controls whether `gcx` can see it. It sets this in its app manifest.

The namespace of the current context does not apply to cluster-scoped types.

## Incident descriptions

Incident descriptions remain visible in `gcx resources get` and
`gcx irm incidents list` results.

`gcx resources pull` omits descriptions from saved manifests. These backups
cannot restore incident descriptions.

Old backup files can contain `spec.description`. Remove that field before you
push the file to create an incident or to change an incident with a different
description. You can update other fields when `spec.description` is absent or equal to the current value.

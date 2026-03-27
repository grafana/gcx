---
title: Manage resources
---

## Migrate resources between environments

Grafana resources can be migrated from one environment to another, for example: from a development to production environment.

As such, you will need to [configure several contexts](../configuration.md#defining-contexts): one per environment.
In this example scenario, we will use `dev` for the development environment and `prod` for production.

1. Make changes to dashboards and other resources using the Grafana UI in your **development instance**.
1. Pull those resources from the development environment to your local machine:

   ```shell
   gcx resources pull --context dev # Add `-o yaml` export resources as YAML 
   ```

1. Push the resources to production:

   ```shell
   gcx resources push --context prod
   ```

!!! note
    Resources are pulled and pushed from the `./resources` directory by default.
    This path can be configured with the `--path`/`-p` flags.

## Backup and restore resources

This workflow helps you back up all Grafana resources from one instance and later restore them. This can be useful to replicate a configuration or perform disaster recovery.

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

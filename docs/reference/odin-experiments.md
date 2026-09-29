---
title: Odin experiments
---

# Odin experiments

Use `gcx experiments list` to find names, then `gcx experiments get <name>` to
read the complete stored resource. The get command defaults to YAML; use
`-o json` or `--jq` to select fields for an agent.

## Create an Odin experiment

`gcx experiments create` accepts one complete `Experiment` resource. It is
non-interactive, so a person or an agent can author the same YAML or JSON file.
It uses the selected Grafana context and Odin's app plugin endpoint.

## Agent workflow

1. Run `gcx experiments create --example -o yaml` to get a complete single-query example.
2. Choose the target datasource with `gcx datasources list`; replace the example's datasource UID, type, and datasource-specific `query` object. Check that the query returns the named entity, variant, and metric fields. The example query object is for PostgreSQL; other datasource plugins use different fields.
3. Set the experiment name, title, description, existing Grafana feature toggle, variant values, metrics, and desired lifecycle status. Start with `spec.status: draft` unless the experiment is ready to wait for data. Review the manifest before the write.
4. Create with `gcx experiments create -f experiment.yaml -o json`, or stream JSON/YAML through `gcx experiments create -f -`. The result is the full resource returned by Odin, including its assigned namespace and metadata.

The plugin validates the resource's structure and semantic fields. Creation does
not prove that the stored datasource query returns the intended data. Query
preview depends on the datasource and should be checked separately before
creation. This command has no server-side dry-run; `--example` only prints a
sample and does not validate or create anything.

## Manifest rules

- `apiVersion` must be `odin.ext.grafana.com/v1alpha1`, and `kind` must be `Experiment`.
- `metadata.name` identifies the new experiment. Odin chooses the namespace. Omit server-managed `metadata.uid`, `metadata.resourceVersion`, and top-level `status`.
- `spec.grafanaFeatureToggle.name` references a toggle that already exists. Do not use the deprecated `spec.featureFlags` model for new experiments.
- `spec.analyticsConfig.unifiedQuery.query` is the datasource's native query object. `variantConfig` maps query values to control and treatment. `metrics[]` describes the measurements and the direction that counts as improvement.
- A create fails if the name already exists; it never updates an experiment.

See the [CLI reference](cli/gcx_experiments_create.md) for flags and examples.

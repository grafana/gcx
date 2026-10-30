# `target-valid-promql`

Checks that each Prometheus target on each panel uses a valid PromQL query.

Queries that reference a datasource variable use its configured plugin to select
the query language: the variable's `query` field in v1 dashboards and
`DatasourceVariable.spec.pluginId` in v2 dashboards. This takes precedence over
the target's saved datasource type (v1) or query group (v2). References in `$name`,
`${name}`, and legacy `[[name]]` forms are supported. Direct datasource references
and unresolved variables keep their saved type or group.
Non-Prometheus queries are skipped. Violations include the panel identifier and
query reference ID in their details.

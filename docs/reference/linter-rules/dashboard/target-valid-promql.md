# `target-valid-promql`

Checks that each Prometheus target on each panel uses a valid PromQL query.

For v2 dashboards, a query whose datasource references a `DatasourceVariable`
uses that variable's `pluginId` to select the query language. This takes precedence
over saved query group metadata. References in `$name`, `${name}`, and legacy
`[[name]]` forms are supported. Other queries use their saved group.
Non-Prometheus queries are skipped. Violations include the panel identifier and
query reference ID in their details.

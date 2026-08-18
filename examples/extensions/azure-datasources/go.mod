// A gcx extension is an ordinary program. This one deliberately depends on
// nothing but the standard library: it reaches Azure through the `az` CLI and
// Grafana through the `gcx` binary that dispatched it.
module github.com/grafana/gcx-ext-azure-datasources

go 1.24

package datasources

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// DatasourceProvider defines the interface for a datasource type plugin.
// Each implementation registers under `datasources $KIND` with its own
// set of subcommands - query is required, extra commands are optional.
//
// The ConfigLoader is created and flag-bound by the mounting code in
// cmd/gcx/datasources/, so providers don't manage flag binding themselves.
type DatasourceProvider interface {
	// Kind returns the datasource kind (e.g., "prometheus", "loki", "pyroscope", "tempo").
	Kind() string

	// ShortDesc returns a one-line description of the datasource provider.
	ShortDesc() string

	// QueryCmd returns the query subcommand. Every datasource must support querying.
	QueryCmd(loader *providers.ConfigLoader) *cobra.Command

	// ExtraCommands returns additional subcommands (labels, metrics, etc.).
	// Returns nil when the provider has no commands beyond query.
	ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command
}

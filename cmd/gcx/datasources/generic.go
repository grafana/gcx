package datasources

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/datasources/query"
	"github.com/spf13/cobra"
)

func queryCmd(configOpts *cmdconfig.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query any datasource (auto-detects type)",
		Long:  "Query any datasource type. The datasource type is auto-detected via the Grafana API.",
	}

	cmd.AddCommand(query.GenericCmd(configOpts))

	return cmd
}

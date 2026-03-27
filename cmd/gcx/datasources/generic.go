package datasources

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/datasources/query"
	"github.com/spf13/cobra"
)

func genericCmd(configOpts *cmdconfig.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generic",
		Short: "Generic datasource operations (auto-detects type)",
		Long:  "Operations for any datasource type. The datasource type is auto-detected via the Grafana API.",
	}

	cmd.AddCommand(query.GenericCmd(configOpts))

	return cmd
}

package resources

import (
	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Manipulate Grafana resources",
		Long:  "Manipulate Grafana resources.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(deleteCmd(configOpts))
	cmd.AddCommand(editCmd(configOpts))
	cmd.AddCommand(examplesCmd(configOpts))
	cmd.AddCommand(getCmd(configOpts))
	cmd.AddCommand(listCmd(configOpts))
	cmd.AddCommand(pullCmd(configOpts))
	cmd.AddCommand(pushCmd(configOpts))
	cmd.AddCommand(validateCmd(configOpts))

	return cmd
}

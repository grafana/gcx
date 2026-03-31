package resources

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Manipulate Grafana resources",
		Long:  "Manipulate Grafana resources.",
		// Inject --context into the Go context for all subcommands so provider
		// adapter factories (SLO, Synth, etc.) can honour it when loading
		// their own credentials.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Cobra v1.x does not chain PersistentPreRun hooks, so we must
			// explicitly call the root hook to preserve terminal detection,
			// agent mode enforcement, logging setup, and flag handling.
			if root := cmd.Root(); root != nil && root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
			ctx := config.ContextWithName(cmd.Context(), configOpts.Context)
			cmd.SetContext(ctx)
		},
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(deleteCmd(configOpts))
	cmd.AddCommand(editCmd(configOpts))
	cmd.AddCommand(examplesCmd(configOpts))
	cmd.AddCommand(getCmd(configOpts))
	cmd.AddCommand(schemasCmd(configOpts))
	cmd.AddCommand(pullCmd(configOpts))
	cmd.AddCommand(pushCmd(configOpts))
	cmd.AddCommand(validateCmd(configOpts))

	return cmd
}

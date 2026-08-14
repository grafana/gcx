package irm

import (
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// IRM keeps its own copy of the Grafana users and teams, and refreshes it on
// a schedule. Until that refresh lands, an IRM object that references a new
// team or user fails with "Object does not exist". A script therefore cannot
// sequence "create the team" then "create the schedule of that team" without
// a way to force the refresh.

type syncPluginOpts struct {
	IO cmdio.Options
}

func (o *syncPluginOpts) setup(flags *pflag.FlagSet) {
	// The result is a SingleMutation document through the codec system: the
	// text codec prints the human one-liner, and agent mode or an explicit
	// -o json/yaml gets the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{
		render: func(w io.Writer, _ cmdio.SingleMutation) {
			cmdio.Success(w, "Requested a sync of the IRM plugin")
		},
	})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newSyncPluginCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &syncPluginOpts{}
	cmd := &cobra.Command{
		Use:   "sync-plugin",
		Short: "Refresh the IRM copy of the Grafana users and teams.",
		Long: `Refresh the IRM copy of the Grafana users and teams.

IRM mirrors the Grafana users and teams, and refreshes that copy on a
schedule. Until the refresh lands, an IRM object that references a new team or
a new user fails with "Object does not exist".

Run this command after you create a Grafana team or user, before you create
the IRM objects that reference it.

The backend accepts the request and refreshes in the background, so a
successful call does not prove that the copy is already current.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}

			if _, err := client.SyncPlugin(ctx); err != nil {
				return err
			}

			// Changed stays unset: the backend reports that it accepted the
			// request, not whether the copy changed.
			result := cmdio.NewSingleMutation("sync-requested", cmdio.MutationTarget{
				Kind: "Plugin",
				Name: "grafana-irm-app",
			})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

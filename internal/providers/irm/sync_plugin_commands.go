package irm

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// IRM keeps its own copy of the Grafana users and teams, and refreshes it on
// a schedule. Until that refresh lands, an IRM object that references a new
// team or user fails with "Object does not exist". A script therefore cannot
// sequence "create the team" then "create the schedule of that team" without
// a way to force the refresh.

// syncPluginResult is the result document of `gcx irm sync-plugin`: the shared
// single-mutation document plus the message of the backend. The backend can
// distinguish "sync request processed" from "sync already running", so the
// message reaches the caller. The field is absent when the backend sends none.
type syncPluginResult struct {
	cmdio.SingleMutation

	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// syncPluginTextCodec renders the human one-liner for syncPluginResult. The
// shared singleMutationTextCodec accepts a bare cmdio.SingleMutation only.
type syncPluginTextCodec struct{}

func (c *syncPluginTextCodec) Format() format.Format { return "text" }

func (c *syncPluginTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *syncPluginTextCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(syncPluginResult)
	if !ok {
		return errors.New("invalid data type for text codec: expected syncPluginResult")
	}
	if result.Message != "" {
		cmdio.Success(w, "Requested a sync of the IRM plugin: %s", result.Message)
		return nil
	}
	cmdio.Success(w, "Requested a sync of the IRM plugin")
	return nil
}

type syncPluginOpts struct {
	IO cmdio.Options
}

func (o *syncPluginOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &syncPluginTextCodec{})
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

			synced, err := client.SyncPlugin(ctx)
			if err != nil {
				return err
			}

			// Changed stays unset: the backend reports that it accepted the
			// request, not whether the copy changed.
			result := syncPluginResult{
				SingleMutation: cmdio.NewSingleMutation("sync-requested", cmdio.MutationTarget{
					Kind: "Plugin",
					Name: "grafana-irm-app",
				}),
			}
			if synced != nil {
				result.Message = synced.Message
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

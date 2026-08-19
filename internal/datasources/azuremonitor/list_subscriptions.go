package azuremonitor

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	azclient "github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listSubscriptionsOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *listSubscriptionsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listSubscriptionsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
}

func (opts *listSubscriptionsOpts) Validate() error {
	return opts.IO.Validate()
}

// ListSubscriptionsCmd returns the `list-subscriptions` subcommand for Azure Monitor.
func ListSubscriptionsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listSubscriptionsOpts{}

	cmd := &cobra.Command{
		Use:   "list-subscriptions",
		Short: "List Azure subscriptions visible to the datasource",
		Long:  "List the Azure subscriptions the Azure Monitor datasource's credentials can access.",
		Example: `
  gcx datasources azuremonitor list-subscriptions -d UID
  gcx datasources azuremonitor list-subscriptions -d UID -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "azuremonitor")
			if err != nil {
				return err
			}

			client, err := azclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			subs, err := client.ListSubscriptions(ctx, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to list subscriptions: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), subs)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor list-subscriptions -d UID",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listSubscriptionsTableCodec struct{}

func (c *listSubscriptionsTableCodec) Format() format.Format { return "table" }

func (c *listSubscriptionsTableCodec) Encode(w io.Writer, data any) error {
	subs, ok := data.([]azclient.Subscription)
	if !ok {
		return fmt.Errorf("listSubscriptionsTableCodec: unexpected type %T", data)
	}
	return azclient.FormatSubscriptions(w, subs)
}

func (c *listSubscriptionsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listSubscriptionsTableCodec does not support decoding")
}

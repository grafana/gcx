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

type listResourceGroupsOpts struct {
	IO           cmdio.Options
	Datasource   string
	Subscription string
}

func (opts *listResourceGroupsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listResourceGroupsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringVar(&opts.Subscription, "subscription", "", "Azure subscription ID (required)")
}

func (opts *listResourceGroupsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Subscription == "" {
		return errors.New("--subscription is required")
	}
	return nil
}

// ListResourceGroupsCmd returns the `list-resource-groups` subcommand for Azure Monitor.
func ListResourceGroupsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listResourceGroupsOpts{}

	cmd := &cobra.Command{
		Use:   "list-resource-groups",
		Short: "List resource groups in an Azure subscription",
		Long:  "List the resource groups in an Azure subscription via an Azure Monitor datasource.",
		Example: `
  gcx datasources azuremonitor list-resource-groups -d UID --subscription SUB_ID
  gcx datasources azuremonitor list-resource-groups -d UID --subscription SUB_ID -o json`,
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

			groups, err := client.ListResourceGroups(ctx, datasourceUID, opts.Subscription)
			if err != nil {
				return fmt.Errorf("failed to list resource groups: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), groups)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor list-resource-groups -d UID --subscription SUB_ID",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listResourceGroupsTableCodec struct{}

func (c *listResourceGroupsTableCodec) Format() format.Format { return "table" }

func (c *listResourceGroupsTableCodec) Encode(w io.Writer, data any) error {
	groups, ok := data.([]azclient.ResourceGroup)
	if !ok {
		return fmt.Errorf("listResourceGroupsTableCodec: unexpected type %T", data)
	}
	return azclient.FormatResourceGroups(w, groups)
}

func (c *listResourceGroupsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listResourceGroupsTableCodec does not support decoding")
}

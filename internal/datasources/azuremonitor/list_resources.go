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

type listResourcesOpts struct {
	IO            cmdio.Options
	Datasource    string
	Subscription  string
	ResourceGroup string
}

func (opts *listResourcesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listResourcesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringVar(&opts.Subscription, "subscription", "", "Azure subscription ID (required)")
	flags.StringVar(&opts.ResourceGroup, "resource-group", "", "Azure resource group name (optional; lists the whole subscription when omitted)")
}

func (opts *listResourcesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Subscription == "" {
		return errors.New("--subscription is required")
	}
	return nil
}

// ListResourcesCmd returns the `list-resources` subcommand for Azure Monitor.
func ListResourcesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listResourcesOpts{}

	cmd := &cobra.Command{
		Use:   "list-resources",
		Short: "List Azure resources in a subscription or resource group",
		Long:  "List the Azure resources in a subscription, optionally scoped to a resource group, via an Azure Monitor datasource.",
		Example: `
  gcx datasources azuremonitor list-resources -d UID --subscription SUB_ID --resource-group my-rg
  gcx datasources azuremonitor list-resources -d UID --subscription SUB_ID -o json`,
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

			resources, err := client.ListResources(ctx, datasourceUID, opts.Subscription, opts.ResourceGroup)
			if err != nil {
				return fmt.Errorf("failed to list resources: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resources)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor list-resources -d UID --subscription SUB_ID --resource-group RG",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listResourcesTableCodec struct{}

func (c *listResourcesTableCodec) Format() format.Format { return "table" }

func (c *listResourcesTableCodec) Encode(w io.Writer, data any) error {
	resources, ok := data.([]azclient.Resource)
	if !ok {
		return fmt.Errorf("listResourcesTableCodec: unexpected type %T", data)
	}
	return azclient.FormatResources(w, resources)
}

func (c *listResourcesTableCodec) Decode(io.Reader, any) error {
	return errors.New("listResourcesTableCodec does not support decoding")
}

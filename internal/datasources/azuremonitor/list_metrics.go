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

type listMetricsOpts struct {
	IO            cmdio.Options
	Datasource    string
	Subscription  string
	ResourceGroup string
	Resource      string
	Namespace     string
}

func (opts *listMetricsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listMetricsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringVar(&opts.Subscription, "subscription", "", "Azure subscription ID (required)")
	flags.StringVar(&opts.ResourceGroup, "resource-group", "", "Azure resource group name (required)")
	flags.StringVar(&opts.Resource, "resource", "", "Azure resource name (required)")
	flags.StringVar(&opts.Namespace, "namespace", "", "Metric namespace, e.g. Microsoft.Storage/storageAccounts (required; matches the resource type from list-resources)")
}

func (opts *listMetricsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Subscription == "" {
		return errors.New("--subscription is required")
	}
	if opts.ResourceGroup == "" {
		return errors.New("--resource-group is required")
	}
	if opts.Resource == "" {
		return errors.New("--resource is required")
	}
	if opts.Namespace == "" {
		return errors.New("--namespace is required")
	}
	return nil
}

// ListMetricsCmd returns the `list-metrics` subcommand for Azure Monitor.
func ListMetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listMetricsOpts{}

	cmd := &cobra.Command{
		Use:   "list-metrics",
		Short: "List available Azure Monitor metrics for a resource",
		Long: `List the Azure Monitor metric definitions available for a resource, including
each metric's primary aggregation, unit, and dimensions.`,
		Example: `
  gcx datasources azuremonitor list-metrics -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Storage/storageAccounts --resource mystorage

  gcx datasources azuremonitor list-metrics -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Compute/virtualMachines --resource my-vm -o json`,
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

			defs, err := client.ListMetricDefinitions(ctx, datasourceUID, opts.Subscription, opts.ResourceGroup, opts.Namespace, opts.Resource)
			if err != nil {
				return fmt.Errorf("failed to list metrics: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), defs)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor list-metrics -d UID --subscription SUB_ID --resource-group RG --namespace Microsoft.Storage/storageAccounts --resource NAME",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listMetricsTableCodec struct{}

func (c *listMetricsTableCodec) Format() format.Format { return "table" }

func (c *listMetricsTableCodec) Encode(w io.Writer, data any) error {
	defs, ok := data.([]azclient.MetricDefinition)
	if !ok {
		return fmt.Errorf("listMetricsTableCodec: unexpected type %T", data)
	}
	return azclient.FormatMetricDefinitions(w, defs)
}

func (c *listMetricsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listMetricsTableCodec does not support decoding")
}

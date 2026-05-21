package cloudwatch

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listMetricsOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
	Namespace  string
	AccountID  string
}

func (opts *listMetricsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listMetricsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudwatch is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region (required)")
	flags.StringVar(&opts.Namespace, "namespace", "", "CloudWatch namespace (required)")
	flags.StringVar(&opts.AccountID, "account-id", "", "AWS account ID for cross-account monitoring (or 'all')")
}

func (opts *listMetricsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Region == "" {
		return errors.New("--region is required")
	}
	if opts.Namespace == "" {
		return errors.New("--namespace is required")
	}
	return nil
}

// ListMetricsCmd returns the `list-metrics` subcommand for CloudWatch.
func ListMetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listMetricsOpts{}

	cmd := &cobra.Command{
		Use:   "list-metrics",
		Short: "List available CloudWatch metrics in a namespace",
		Long:  "List the CloudWatch metrics available within a namespace and region from a CloudWatch datasource.",
		Example: `
  gcx datasources cloudwatch list-metrics -d UID --region us-east-1 --namespace AWS/EC2
  gcx datasources cloudwatch list-metrics -d UID --region us-east-1 --namespace AWS/Lambda -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "cloudwatch")
			if err != nil {
				return err
			}

			client, err := cwclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			metrics, err := client.ListMetrics(ctx, datasourceUID, opts.Region, opts.Namespace, opts.AccountID)
			if err != nil {
				return fmt.Errorf("failed to list metrics: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), metrics)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources cloudwatch list-metrics -d UID --region us-east-1 --namespace AWS/EC2 -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listMetricsTableCodec struct{}

func (c *listMetricsTableCodec) Format() format.Format { return "table" }

func (c *listMetricsTableCodec) Encode(w io.Writer, data any) error {
	metrics, ok := data.([]cwclient.Metric)
	if !ok {
		return fmt.Errorf("listMetricsTableCodec: unexpected type %T", data)
	}
	return cwclient.FormatMetrics(w, metrics)
}

func (c *listMetricsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listMetricsTableCodec does not support decoding")
}

package cloudwatch

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listDimensionsOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
	Namespace  string
	Metric     string
	AccountID  string
}

func (opts *listDimensionsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listDimensionsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudwatch is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region (required)")
	flags.StringVar(&opts.Namespace, "namespace", "", "CloudWatch namespace (required)")
	flags.StringVar(&opts.Metric, "metric", "", "CloudWatch metric name (required)")
	flags.StringVar(&opts.AccountID, "account-id", "", "AWS account ID for cross-account monitoring (or 'all')")
}

func (opts *listDimensionsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Region == "" {
		return errors.New("--region is required")
	}
	if opts.Namespace == "" {
		return errors.New("--namespace is required")
	}
	if opts.Metric == "" {
		return errors.New("--metric is required")
	}
	return nil
}

// ListDimensionsCmd returns the `list-dimensions` subcommand for CloudWatch.
func ListDimensionsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listDimensionsOpts{}

	cmd := &cobra.Command{
		Use:   "list-dimensions",
		Short: "List available dimension keys for a CloudWatch metric",
		Long:  "List the dimension keys available for a CloudWatch metric within a namespace and region.",
		Example: `
  gcx datasources cloudwatch list-dimensions -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization
  gcx datasources cloudwatch list-dimensions -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization -o json`,
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

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "cloudwatch")
			if err != nil {
				return err
			}

			client, err := cwclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			keys, err := client.ListDimensionKeys(ctx, datasourceUID, opts.Region, opts.Namespace, opts.Metric, opts.AccountID)
			if err != nil {
				return fmt.Errorf("failed to list dimension keys: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), keys)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources cloudwatch list-dimensions -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listDimensionsTableCodec struct{}

func (c *listDimensionsTableCodec) Format() format.Format { return "table" }

func (c *listDimensionsTableCodec) Encode(w io.Writer, data any) error {
	keys, ok := data.([]string)
	if !ok {
		return fmt.Errorf("listDimensionsTableCodec: unexpected type %T", data)
	}
	return cwclient.FormatDimensions(w, keys)
}

func (c *listDimensionsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listDimensionsTableCodec does not support decoding")
}

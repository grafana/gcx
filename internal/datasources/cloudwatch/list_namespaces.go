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

type listNamespacesOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
	AccountID  string
}

func (opts *listNamespacesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listNamespacesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudwatch is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region (required)")
	flags.StringVar(&opts.AccountID, "account-id", "", "AWS account ID for cross-account monitoring (or 'all')")
}

func (opts *listNamespacesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Region == "" {
		return errors.New("--region is required")
	}
	return nil
}

// ListNamespacesCmd returns the `list-namespaces` subcommand for CloudWatch.
func ListNamespacesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listNamespacesOpts{}

	cmd := &cobra.Command{
		Use:   "list-namespaces",
		Short: "List available CloudWatch namespaces",
		Long:  "List the CloudWatch namespaces visible in the given region from a CloudWatch datasource.",
		Example: `
  gcx datasources cloudwatch list-namespaces -d UID --region us-east-1
  gcx datasources cloudwatch list-namespaces -d UID --region us-east-1 -o json`,
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

			namespaces, err := client.ListNamespaces(ctx, datasourceUID, opts.Region, opts.AccountID)
			if err != nil {
				return fmt.Errorf("failed to list namespaces: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), namespaces)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources cloudwatch list-namespaces -d UID --region us-east-1 -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listNamespacesTableCodec struct{}

func (c *listNamespacesTableCodec) Format() format.Format { return "table" }

func (c *listNamespacesTableCodec) Encode(w io.Writer, data any) error {
	ns, ok := data.([]string)
	if !ok {
		return fmt.Errorf("listNamespacesTableCodec: unexpected type %T", data)
	}
	return cwclient.FormatNamespaces(w, ns)
}

func (c *listNamespacesTableCodec) Decode(io.Reader, any) error {
	return errors.New("listNamespacesTableCodec does not support decoding")
}

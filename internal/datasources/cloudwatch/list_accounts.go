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

type listAccountsOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
}

func (opts *listAccountsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listAccountsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudwatch is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region (required)")
}

func (opts *listAccountsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Region == "" {
		return errors.New("--region is required")
	}
	return nil
}

// ListAccountsCmd returns the `list-accounts` subcommand for CloudWatch.
func ListAccountsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listAccountsOpts{}

	cmd := &cobra.Command{
		Use:   "list-accounts",
		Short: "List AWS accounts accessible via cross-account monitoring",
		Long: `List the AWS accounts accessible via this CloudWatch datasource.
Only returns data for cross-account monitoring datasources; other datasources
may return a 404 which is surfaced as a clear error.`,
		Example: `
  gcx datasources cloudwatch list-accounts -d UID --region us-east-1
  gcx datasources cloudwatch list-accounts -d UID --region us-east-1 -o json`,
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

			accounts, err := client.ListAccounts(ctx, datasourceUID, opts.Region)
			if err != nil {
				return fmt.Errorf("failed to list accounts: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), accounts)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources cloudwatch list-accounts -d UID --region us-east-1 -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listAccountsTableCodec struct{}

func (c *listAccountsTableCodec) Format() format.Format { return "table" }

func (c *listAccountsTableCodec) Encode(w io.Writer, data any) error {
	accounts, ok := data.([]cwclient.Account)
	if !ok {
		return fmt.Errorf("listAccountsTableCodec: unexpected type %T", data)
	}
	return cwclient.FormatAccounts(w, accounts)
}

func (c *listAccountsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listAccountsTableCodec does not support decoding")
}

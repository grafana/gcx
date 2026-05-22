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

type listRegionsOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *listRegionsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listRegionsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudwatch is configured)")
}

func (opts *listRegionsOpts) Validate() error {
	return opts.IO.Validate()
}

// ListRegionsCmd returns the `list-regions` subcommand for CloudWatch.
func ListRegionsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listRegionsOpts{}

	cmd := &cobra.Command{
		Use:   "list-regions",
		Short: "List available AWS regions for the CloudWatch datasource",
		Long:  "List the AWS regions exposed by the configured CloudWatch datasource.",
		Example: `
  gcx datasources cloudwatch list-regions -d UID
  gcx datasources cloudwatch list-regions -d UID -o json`,
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

			regions, err := client.ListRegions(ctx, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to list regions: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), regions)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources cloudwatch list-regions -d UID -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listRegionsTableCodec struct{}

func (c *listRegionsTableCodec) Format() format.Format { return "table" }

func (c *listRegionsTableCodec) Encode(w io.Writer, data any) error {
	regions, ok := data.([]string)
	if !ok {
		return fmt.Errorf("listRegionsTableCodec: unexpected type %T", data)
	}
	return cwclient.FormatRegions(w, regions)
}

func (c *listRegionsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listRegionsTableCodec does not support decoding")
}

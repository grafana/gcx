// Package synth mounts Synthetic Monitoring as a dedicated datasource under
// `gcx datasources synthetic-monitoring ...` (aliases: sm, synth). Per-resource
// subcommands (probes, checks) are registered as ExtraCommands on the
// synthDSProvider.
//
// HTTP transport lives in internal/query/synth — this package owns only the
// cobra command surface and resource resolution.
package synth

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type probesOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *probesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &probesTableCodec{})
	opts.IO.RegisterCustomCodec("wide", &probesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.synthetic-monitoring is configured)")
}

func (opts *probesOpts) Validate() error {
	return opts.IO.Validate()
}

// ProbesCmd returns the `probes` subcommand for a Synthetic Monitoring datasource parent.
func ProbesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &probesOpts{}

	cmd := &cobra.Command{
		Use:   "probes",
		Short: "List Synthetic Monitoring probes",
		Long:  "List all probes accessible through the configured Synthetic Monitoring datasource.",
		Example: `
  # List probes (use datasource UID, not name)
  gcx datasources synthetic-monitoring probes -d UID

  # Output as JSON
  gcx datasources synthetic-monitoring probes -d UID -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProbes(cmd, loader, opts)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// runProbes is the shared execution path for both `probes` and `query probes`.
func runProbes(cmd *cobra.Command, loader *providers.ConfigLoader, opts *probesOpts) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	ctx := cmd.Context()

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return err
	}

	var cfgCtx *internalconfig.Context
	if fullCfg, err := loader.LoadFullConfig(ctx); err == nil {
		cfgCtx = fullCfg.GetCurrentContext()
	} else {
		logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
	}

	datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "synthetic-monitoring")
	if err != nil {
		return err
	}

	client, err := synth.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	result, err := client.ListProbes(ctx, datasourceUID)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	return opts.IO.Encode(cmd.OutOrStdout(), result)
}

type probesTableCodec struct{}

func (c *probesTableCodec) Format() format.Format { return "table" }

func (c *probesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.([]probes.Probe)
	if !ok {
		return errors.New("invalid data type for probes table codec")
	}
	return synth.FormatProbesTable(w, resp)
}

func (c *probesTableCodec) Decode(io.Reader, any) error {
	return errors.New("probes table codec does not support decoding")
}

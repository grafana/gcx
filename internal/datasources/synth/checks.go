package synth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type checksOpts struct {
	IO           cmdio.Options
	Datasource   string
	WithAlerts   bool
	Search       string
	Enabled      *bool
	MinFrequency time.Duration
	MaxFrequency time.Duration
}

func (opts *checksOpts) setup(flags *pflag.FlagSet) {
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.synthetic-monitoring is configured)")
	flags.BoolVar(&opts.WithAlerts, "with-alerts", false, "Include each check's alert rules in the response (server-side composition via ?includeAlerts=true). Cannot be combined with --search/--enabled/--min-frequency/--max-frequency.")
	flags.StringVar(&opts.Search, "search", "", "Case-insensitive substring match against the check's job and target")
	// Enabled is tri-state: nil = no filter, &true = only enabled, &false = only disabled.
	// pflag has no native *bool, so we wire it manually after construction.
	flags.Var(&optionalBool{ptr: &opts.Enabled}, "enabled", "Restrict to enabled (--enabled=true) or disabled (--enabled=false) checks; omit for no filter")
	flags.Lookup("enabled").NoOptDefVal = "true" // bare --enabled means --enabled=true
	flags.DurationVar(&opts.MinFrequency, "min-frequency", 0, "Lower bound on check frequency, inclusive (e.g. 30s, 5m). Sent to the API as min_frequency in milliseconds.")
	flags.DurationVar(&opts.MaxFrequency, "max-frequency", 0, "Upper bound on check frequency, inclusive (e.g. 30s, 5m). Sent to the API as max_frequency in milliseconds.")
}

func (opts *checksOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.WithAlerts && (opts.Search != "" || opts.Enabled != nil || opts.MinFrequency > 0 || opts.MaxFrequency > 0) {
		return errors.New("--with-alerts cannot be combined with --search/--enabled/--min-frequency/--max-frequency (synthetic-monitoring API limitation)")
	}
	if opts.MinFrequency > 0 && opts.MaxFrequency > 0 && opts.MinFrequency > opts.MaxFrequency {
		return fmt.Errorf("--min-frequency (%s) must be <= --max-frequency (%s)", opts.MinFrequency, opts.MaxFrequency)
	}
	return nil
}

// optionalBool is a pflag.Value that toggles a *bool — used to model
// tri-state flags where "unset" is meaningful.
type optionalBool struct {
	ptr **bool
}

func (o *optionalBool) String() string {
	if o.ptr == nil || *o.ptr == nil {
		return ""
	}
	if **o.ptr {
		return "true"
	}
	return "false"
}

func (o *optionalBool) Set(s string) error {
	switch s {
	case "true", "TRUE", "True", "1":
		v := true
		*o.ptr = &v
	case "false", "FALSE", "False", "0":
		v := false
		*o.ptr = &v
	default:
		return fmt.Errorf("invalid bool %q: expected true or false", s)
	}
	return nil
}

func (o *optionalBool) Type() string { return "bool" }

// ChecksCmd returns the `checks` subcommand for a Synthetic Monitoring datasource parent.
func ChecksCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &checksOpts{}

	cmd := &cobra.Command{
		Use:   "checks",
		Short: "List Synthetic Monitoring checks",
		Long:  "List all checks accessible through the configured Synthetic Monitoring datasource.",
		Example: `
  # List checks (use datasource UID, not name)
  gcx datasources synthetic-monitoring checks -d UID

  # List checks with their alert rules embedded (one server-side call)
  gcx datasources synthetic-monitoring checks -d UID --with-alerts

  # Filter by job/target substring
  gcx datasources synthetic-monitoring checks -d UID --search api-prod

  # Only disabled checks
  gcx datasources synthetic-monitoring checks -d UID --enabled=false

  # Aggressive checks (faster than 1 minute)
  gcx datasources synthetic-monitoring checks -d UID --max-frequency 1m

  # Combine filters
  gcx datasources synthetic-monitoring checks -d UID --search staging --enabled --max-frequency 30s

  # Output as JSON
  gcx datasources synthetic-monitoring checks -d UID -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			result, err := client.ListChecksFiltered(ctx, datasourceUID, synth.ListChecksOptions{
				Search:       opts.Search,
				Enabled:      opts.Enabled,
				MinFrequency: opts.MinFrequency,
				MaxFrequency: opts.MaxFrequency,
				WithAlerts:   opts.WithAlerts,
			})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

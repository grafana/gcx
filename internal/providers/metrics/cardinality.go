package metrics

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CardinalityCommands returns the `cardinality` subcommand group exposing the
// Mimir cardinality analysis endpoints (label names and label values).
func CardinalityCommands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cardinality",
		Short: "Analyze series cardinality",
		Long: `Analyze series cardinality by label names and values via the Mimir cardinality analysis API.

These endpoints are available on Grafana Mimir (OSS) and Grafana Cloud; on
self-hosted Mimir they require -querier.cardinality-analysis-enabled.`,
	}

	cmd.AddCommand(newCardinalityLabelNamesCmd(loader), newCardinalityLabelValuesCmd(loader))

	return cmd
}

type cardinalityOpts struct {
	IO          cmdio.Options
	Datasource  string
	Selector    string
	CountMethod string
	Limit       int
	Labels      []string // label-values only
}

func (opts *cardinalityOpts) setup(flags *pflag.FlagSet, withLabels bool) {
	if withLabels {
		opts.IO.RegisterCustomCodec("table", &cardinalityLabelValuesTableCodec{})
	} else {
		opts.IO.RegisterCustomCodec("table", &cardinalityLabelNamesTableCodec{})
	}
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	// For label_values the limit is applied per input label name; for
	// label_names it caps the single list of returned label names.
	limitUsage := "Maximum number of items to return (0-500)"
	if withLabels {
		limitUsage = "Maximum number of items to return per label (0-500)"
	}

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringVar(&opts.Selector, "selector", "", "PromQL series selector scoping the analysis")
	flags.StringVar(&opts.CountMethod, "count-method", "inmemory", `Series counting method: "inmemory" or "active"`)
	flags.IntVar(&opts.Limit, "limit", 20, limitUsage)

	if withLabels {
		flags.StringArrayVarP(&opts.Labels, "label", "l", nil, "Label name to analyze; repeatable (required)")
	}
}

func (opts *cardinalityOpts) Validate(withLabels bool) error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Limit < 0 || opts.Limit > 500 {
		return errors.New("--limit must be between 0 and 500")
	}
	switch opts.CountMethod {
	case "", "inmemory", "active":
	default:
		return errors.New(`--count-method must be "inmemory" or "active"`)
	}
	if withLabels && len(opts.Labels) == 0 {
		return errors.New("at least one --label is required")
	}
	return nil
}

func (opts *cardinalityOpts) clientOptions() prometheus.CardinalityOptions {
	return prometheus.CardinalityOptions{
		Selector:    opts.Selector,
		CountMethod: opts.CountMethod,
		Limit:       opts.Limit,
	}
}

// resolveClient runs the shared datasource-resolution and client-construction
// flow used by both cardinality subcommands.
func (opts *cardinalityOpts) resolveClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*prometheus.Client, string, error) {
	ctx := cmd.Context()

	cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
	if err != nil {
		return nil, "", err
	}

	datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
	if err != nil {
		return nil, "", err
	}

	client, err := prometheus.NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create client: %w", err)
	}

	return client, datasourceUID, nil
}

func newCardinalityLabelNamesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &cardinalityOpts{}

	cmd := &cobra.Command{
		Use:   "label-names",
		Short: "Show the number of distinct values per label name",
		Long:  "Show, for each label name, how many distinct values it has, via the Mimir cardinality analysis API.",
		Example: `
  # Top label names by distinct value count (configured default datasource)
  gcx metrics cardinality label-names

  # Scope to a metric family and use active-series counting
  gcx metrics cardinality label-names -d UID --selector '{__name__=~"grafanacloud_.*"}' --count-method active

  # Output as JSON
  gcx metrics cardinality label-names -d UID -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(false); err != nil {
				return err
			}

			client, datasourceUID, err := opts.resolveClient(cmd, loader)
			if err != nil {
				return err
			}

			resp, err := client.CardinalityLabelNames(cmd.Context(), datasourceUID, opts.clientOptions())
			if err != nil {
				return fmt.Errorf("failed to get label names cardinality: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx metrics cardinality label-names -d UID -o json",
	}

	opts.setup(cmd.Flags(), false)

	return cmd
}

func newCardinalityLabelValuesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &cardinalityOpts{}

	cmd := &cobra.Command{
		Use:   "label-values",
		Short: "Show distinct values and per-value series counts for labels",
		Long:  "Show, for each requested label name, its distinct values and the number of series per value, via the Mimir cardinality analysis API.",
		Example: `
  # Per-value series counts for one label
  gcx metrics cardinality label-values -d UID --label job

  # Multiple labels, capped and as JSON
  gcx metrics cardinality label-values -d UID --label job --label instance --limit 50 -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(true); err != nil {
				return err
			}

			client, datasourceUID, err := opts.resolveClient(cmd, loader)
			if err != nil {
				return err
			}

			resp, err := client.CardinalityLabelValues(cmd.Context(), datasourceUID, opts.Labels, opts.clientOptions())
			if err != nil {
				return fmt.Errorf("failed to get label values cardinality: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx metrics cardinality label-values -d UID --label job -o json",
	}

	opts.setup(cmd.Flags(), true)

	return cmd
}

type cardinalityLabelNamesTableCodec struct{}

func (c *cardinalityLabelNamesTableCodec) Format() format.Format {
	return "table"
}

func (c *cardinalityLabelNamesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.CardinalityLabelNamesResponse)
	if !ok {
		return errors.New("invalid data type for cardinality label names table codec")
	}

	return prometheus.FormatCardinalityLabelNamesTable(w, resp)
}

func (c *cardinalityLabelNamesTableCodec) Decode(io.Reader, any) error {
	return errors.New("cardinality label names table codec does not support decoding")
}

type cardinalityLabelValuesTableCodec struct{}

func (c *cardinalityLabelValuesTableCodec) Format() format.Format {
	return "table"
}

func (c *cardinalityLabelValuesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.CardinalityLabelValuesResponse)
	if !ok {
		return errors.New("invalid data type for cardinality label values table codec")
	}

	return prometheus.FormatCardinalityLabelValuesTable(w, resp)
}

func (c *cardinalityLabelValuesTableCodec) Decode(io.Reader, any) error {
	return errors.New("cardinality label values table codec does not support decoding")
}

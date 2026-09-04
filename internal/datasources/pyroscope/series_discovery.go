package pyroscope

import (
	"errors"
	"io"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	pyroscope "github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type profileSeriesOpts struct {
	TimeRange  dsquery.TimeRangeOpts
	IO         cmdio.Options
	Datasource string
	Matchers   []string
	LabelNames []string
}

func (opts *profileSeriesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &profileSeriesTableCodec{})
	opts.IO.RegisterCustomCodec("wide", &profileSeriesWideCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringArrayVar(&opts.Matchers, "match", nil, "Profile label selector (repeatable; selectors are combined as a union)")
	flags.StringSliceVar(&opts.LabelNames, "label-name", nil, "Label name to return (repeatable; limit labels to reduce response size and speed up discovery)")
	opts.TimeRange.SetupTimeFlags(flags)
}

func (opts *profileSeriesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.TimeRange.ValidateTimeRange()
}

// SeriesCmd lists unique profile label sets without requiring a profile type.
func SeriesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &profileSeriesOpts{}

	cmd := &cobra.Command{
		Use:   "series [SELECTOR]",
		Short: "List unique profile label sets",
		Long: `List unique profile label sets from a Pyroscope datasource.

The command uses Pyroscope's Series endpoint and does not require a profile
type. SELECTOR is optional; use --match for repeatable selectors. Multiple
selectors are combined as a union. By default, the response includes every
label.

Use --label-name to request only the labels needed for discovery. This reduces
the response size and can significantly speed up queries with high-cardinality
labels. For example, use --label-name service_name --label-name namespace to
discover services and namespaces without fetching pod, instance, or custom
labels.`,
		Args: cobra.RangeArgs(0, 1),
		Example: `
	# List service and workload combinations from the last hour
	gcx datasources pyroscope series -d UID --since 1h

	# Faster discovery: request only the labels needed
	gcx datasources pyroscope series -d UID '{service_name="checkout"}' \
		--label-name service_name --label-name namespace --label-name pod --since 7d

	# Use multiple selectors and JSON output
	gcx datasources pyroscope series -d UID \
		--match '{namespace="payments"}' --match '{namespace="checkout"}' \
		--since 24h -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			selectors := append([]string{}, opts.Matchers...)
			if len(args) == 1 {
				selectors = append(selectors, args[0])
			}

			ctx := cmd.Context()
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}
			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
				return err
			}

			start, end, err := opts.TimeRange.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}
			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return err
			}
			resp, err := client.Series(ctx, datasourceUID, pyroscope.SeriesRequest{
				Matchers:   selectors,
				LabelNames: opts.LabelNames,
				Start:      start,
				End:        end,
			})
			if err != nil {
				return err
			}
			if len(resp.LabelsSet) == 0 {
				emitEmptyWindowHint(cmd.ErrOrStderr(), "profile series", start, end, opts.TimeRange.IsRange())
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   `gcx datasources pyroscope series -d UID --match '{service_name="frontend"}' --label-name namespace --label-name pod --since 1h -o json`,
	}
	opts.setup(cmd.Flags())
	return cmd
}

type profileSeriesTableCodec struct{}

func (c *profileSeriesTableCodec) Format() format.Format { return "table" }

func (c *profileSeriesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*pyroscope.SeriesResponse)
	if !ok {
		return errors.New("invalid data type for profile series table codec")
	}
	return pyroscope.FormatProfileSeriesTable(w, resp)
}

func (c *profileSeriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("profile series table codec does not support decoding")
}

type profileSeriesWideCodec struct{}

func (c *profileSeriesWideCodec) Format() format.Format { return "wide" }

func (c *profileSeriesWideCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*pyroscope.SeriesResponse)
	if !ok {
		return errors.New("invalid data type for profile series wide codec")
	}
	return pyroscope.FormatProfileSeriesWide(w, resp)
}

func (c *profileSeriesWideCodec) Decode(io.Reader, any) error {
	return errors.New("profile series wide codec does not support decoding")
}

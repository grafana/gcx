package elasticsearch

import (
	"errors"
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type metricsOpts struct {
	dsquery.SharedOpts

	Datasource string
	Agg        string
	Field      string
	GroupBy    string
	GroupSize  int
	TimeField  string
}

func (opts *metricsOpts) setup(flags *pflag.FlagSet) {
	opts.Setup(flags, true)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.elasticsearch is configured)")
	flags.StringVar(&opts.Agg, "agg", "count", "Metric aggregation: count, avg, sum, min, max, or cardinality")
	flags.StringVar(&opts.Field, "field", "", "Field to aggregate (required unless --agg count)")
	flags.StringVar(&opts.GroupBy, "group-by", "", "Split series by this field's terms (use .keyword for text fields)")
	flags.IntVar(&opts.GroupSize, "group-size", 10, "Max number of series when using --group-by")
	flags.StringVar(&opts.TimeField, "time-field", elasticsearch.DefaultTimeField, "Time field for the date histogram")
}

func (opts *metricsOpts) Validate() error {
	if err := opts.SharedOpts.Validate(); err != nil {
		return err
	}
	return elasticsearch.ValidateAgg(opts.Agg, opts.Field)
}

// MetricsCmd returns the `metrics` subcommand for an Elasticsearch datasource parent.
func MetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &metricsOpts{}

	cmd := &cobra.Command{
		Use:   "metrics [EXPR]",
		Short: "Aggregate documents over time from an Elasticsearch datasource",
		Long: `Run a metric aggregation bucketed by a time histogram, optionally split
into series by a terms field.

EXPR is a Lucene query string scoping the documents; omit it to aggregate all.
Returns (time, value, series) rows. Use --step to control bucket size.
Datasource is resolved from -d flag or datasources.elasticsearch in your context.`,
		Example: `
  # Document count over time
  gcx datasources elasticsearch metrics --since 6h

  # Error count per app
  gcx datasources elasticsearch metrics 'level:error' --group-by app.keyword --since 6h

  # Average value of a numeric field
  gcx datasources elasticsearch metrics --agg avg --field duration_ms --since 1h -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr := opts.Expr
			if len(args) == 1 {
				if expr != "" {
					return errors.New("provide the expression as a positional argument or via --expr, not both")
				}
				expr = args[0]
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "elasticsearch")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := opts.ParseTimes(now)
			if err != nil {
				return err
			}
			if start.IsZero() && end.IsZero() {
				end = now
				start = now.Add(-1 * time.Hour)
			}

			client, err := elasticsearch.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := elasticsearch.AggsRequest{
				Query:     expr,
				Agg:       opts.Agg,
				Field:     opts.Field,
				GroupBy:   opts.GroupBy,
				GroupSize: opts.GroupSize,
				TimeField: opts.TimeField,
				Start:     start,
				End:       end,
			}
			if step > 0 {
				req.StepMs = step.Milliseconds()
			}

			resp, err := client.Aggregations(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources elasticsearch metrics 'level:error' --group-by app.keyword --since 6h`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

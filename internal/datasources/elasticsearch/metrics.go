package elasticsearch

import (
	"fmt"

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

			resolved, err := prepareQuery(cmd, args, loader, &opts.SharedOpts, opts.Datasource)
			if err != nil {
				return err
			}

			req := elasticsearch.AggsRequest{
				Query:     resolved.Expr,
				Agg:       opts.Agg,
				Field:     opts.Field,
				GroupBy:   opts.GroupBy,
				GroupSize: opts.GroupSize,
				TimeField: opts.TimeField,
				Start:     resolved.Start,
				End:       resolved.End,
				StepMs:    resolved.StepMs,
			}

			resp, err := resolved.Client.Aggregations(cmd.Context(), resolved.DatasourceUID, req)
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

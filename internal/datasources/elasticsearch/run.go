package elasticsearch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
)

// searchFn is the client call a document-search command executes
// (Client.Search or Client.Logs).
type searchFn func(ctx context.Context, c *elasticsearch.Client, dsUID string, req elasticsearch.SearchRequest) (*querysql.QueryResponse, error)

// searchOpts are the flags shared by the query and logs commands.
type searchOpts struct {
	dsquery.SharedOpts

	Datasource string
	Size       int
	TimeField  string
}

func (opts *searchOpts) Validate() error {
	if err := opts.SharedOpts.Validate(); err != nil {
		return err
	}
	if opts.Size <= 0 || opts.Size > maxSize {
		opts.Size = defaultSize
	}
	return nil
}

// searchCmdSpec parameterizes the two document-search commands, which differ
// only in wording, the size flag's name, and the client call they make.
type searchCmdSpec struct {
	use, short, long, example string
	sizeFlag, sizeUsage       string
	tokenCost, llmHint        string
	search                    searchFn
}

// newSearchCmd builds a document-search command from a spec.
func newSearchCmd(loader *providers.ConfigLoader, spec searchCmdSpec) *cobra.Command {
	opts := &searchOpts{}

	cmd := &cobra.Command{
		Use:     spec.use,
		Short:   spec.short,
		Long:    spec.long,
		Example: spec.example,
		Args:    cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return runSearch(cmd, args, loader, &opts.SharedOpts, opts.Datasource, opts.Size, opts.TimeField, spec.search)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: spec.tokenCost,
		agent.AnnotationLLMHint:   spec.llmHint,
	}

	opts.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.elasticsearch is configured)")
	cmd.Flags().IntVar(&opts.Size, spec.sizeFlag, defaultSize, spec.sizeUsage)
	cmd.Flags().StringVar(&opts.TimeField, "time-field", elasticsearch.DefaultTimeField, "Time field used for range filtering")

	return cmd
}

// runSearch is the shared execution path for the query and logs commands:
// resolve the optional Lucene expression, the datasource, and the time range,
// then run the given search and encode its result.
func runSearch(cmd *cobra.Command, args []string, loader *providers.ConfigLoader, opts *dsquery.SharedOpts, datasource string, size int, timeField string, search searchFn) error {
	// EXPR is optional: an empty Lucene query matches all documents.
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

	datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "elasticsearch")
	if err != nil {
		return err
	}

	now := time.Now()
	start, end, _, err := opts.ParseTimes(now)
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

	resp, err := search(ctx, client, datasourceUID, elasticsearch.SearchRequest{
		Query:     expr,
		Size:      size,
		TimeField: timeField,
		Start:     start,
		End:       end,
	})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	return opts.IO.Encode(cmd.OutOrStdout(), resp)
}

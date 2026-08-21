package elasticsearch

import (
	"errors"
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/spf13/cobra"
)

// resolvedQuery is the per-invocation state every Elasticsearch query command
// needs before it calls the client: the Lucene expression, the resolved
// datasource, the effective time range, and a ready client.
type resolvedQuery struct {
	Expr           string
	CfgCtx         *config.Context
	Cfg            config.NamespacedRESTConfig
	DatasourceUID  string
	DatasourceType string
	Start          time.Time
	End            time.Time
	StepMs         int64
	Client         *elasticsearch.Client
}

// ExploreBase returns the datasource and time-range state an Explore link
// builder needs. Expr stays empty: the Lucene expression travels inside the
// plugin query model, not in the shared ExploreQuery field.
func (r *resolvedQuery) ExploreBase(opts *dsquery.SharedOpts) dsquery.ExploreQuery {
	return dsquery.ExploreQuery{
		DatasourceUID:  r.DatasourceUID,
		DatasourceType: r.DatasourceType,
		From:           opts.From,
		To:             opts.To,
		OrgID:          dsquery.OrgID(r.CfgCtx),
	}
}

// prepareQuery resolves the optional Lucene expression, the datasource, the
// default time range, and the query client. The query and metrics commands
// both need this same block before they call the client.
func prepareQuery(cmd *cobra.Command, args []string, loader *providers.ConfigLoader, opts *dsquery.SharedOpts, datasource string) (*resolvedQuery, error) {
	// EXPR is optional: an empty Lucene query matches all documents.
	expr := opts.Expr
	if len(args) == 1 {
		if expr != "" {
			return nil, errors.New("provide the expression as a positional argument or via --expr, not both")
		}
		expr = args[0]
	}

	ctx := cmd.Context()

	cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
	if err != nil {
		return nil, err
	}

	datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "elasticsearch")
	if err != nil {
		return nil, err
	}

	now := time.Now()
	start, end, step, err := opts.ParseTimes(now)
	if err != nil {
		return nil, err
	}
	if start.IsZero() && end.IsZero() {
		end = now
		start = now.Add(-1 * time.Hour)
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	resolved := &resolvedQuery{
		Expr:           expr,
		CfgCtx:         cfgCtx,
		Cfg:            cfg,
		DatasourceUID:  datasourceUID,
		DatasourceType: dsType,
		Start:          start,
		End:            end,
		Client:         client,
	}
	if step > 0 {
		resolved.StepMs = step.Milliseconds()
	}
	return resolved, nil
}

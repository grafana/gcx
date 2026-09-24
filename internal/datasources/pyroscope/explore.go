package pyroscope

import (
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
)

// The query fields match Grafana 12's Pyroscope datasource dataquery schema:
// https://github.com/grafana/grafana/blob/v12.0.0/public/app/plugins/datasource/grafana-pyroscope-datasource/dataquery.gen.ts

// QueryExploreURL builds a standard Grafana Explore URL for a profile query.
func QueryExploreURL(host, datasourceUID string, orgID int64, req pyroscope.QueryRequest) string {
	q := map[string]any{
		"queryType":     "profile",
		"profileTypeId": req.ProfileTypeID,
		"labelSelector": req.LabelSelector,
		"groupBy":       []string{},
	}
	if req.MaxNodes > 0 {
		q["maxNodes"] = req.MaxNodes
	}
	if len(req.SpanIDs) > 0 {
		q["spanSelector"] = req.SpanIDs
	}
	return buildExploreURL(host, datasourceUID, orgID, req.Start, req.End, q)
}

// MetricsExploreURL builds a standard Grafana Explore URL for profile time series.
func MetricsExploreURL(host, datasourceUID string, orgID int64, req pyroscope.SelectSeriesRequest) string {
	groupBy := req.GroupBy
	if groupBy == nil {
		groupBy = []string{}
	}
	q := map[string]any{
		"queryType":     "metrics",
		"profileTypeId": req.ProfileTypeID,
		"labelSelector": req.LabelSelector,
		"groupBy":       groupBy,
		"limit":         req.Limit,
	}
	return buildExploreURL(host, datasourceUID, orgID, req.Start, req.End, q)
}

func buildExploreURL(host, datasourceUID string, orgID int64, start, end time.Time, q map[string]any) string {
	if datasourceUID == "" {
		return ""
	}
	q["refId"] = "A"
	q["datasource"] = dsquery.ExploreDatasource("grafana-pyroscope-datasource", datasourceUID)
	from, to := strconv.FormatInt(start.UnixMilli(), 10), strconv.FormatInt(end.UnixMilli(), 10)
	return dsquery.BuildExploreURL(host, orgID, dsquery.SinglePane(datasourceUID, []any{q}, from, to, nil), nil)
}

// Keep diagnostics and browser side effects after successful output, including
// artifact receipts and DOT output which bypass the usual response codecs.
func encodeAndHandleExplore(cmd *cobra.Command, encode func() error, opts dsquery.ExploreLinkOpts, url string, omitted []string) error {
	unavailable, failedOpen := dsquery.ExploreMessages("query")
	return dsquery.EncodeAndHandleExplore(cmd, func() error {
		if err := encode(); err != nil {
			return err
		}
		if opts.Enabled() && url != "" && len(omitted) > 0 {
			cmdio.Warning(cmd.ErrOrStderr(), "Grafana Explore link does not preserve %s", strings.Join(omitted, ", "))
		}
		return nil
	}, opts, dsquery.ExploreLink{URL: url, UnavailableMsg: unavailable, FailedOpenMsg: failedOpen})
}

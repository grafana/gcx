package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/resources/adapter"
)

type metricsSort struct {
	wire      string
	direction string
}

//nolint:gochecknoglobals // Static lookup table.
var metricsSorts = map[string]metricsSort{
	"projected-change":  {wire: "projected_series_count_change", direction: "asc"},
	"raw-series":        {wire: "raw_series_count", direction: "desc"},
	"current-reduction": {wire: "current_series_count_reduction", direction: "desc"},
	"metric":            {wire: "metric", direction: "asc"},
}

//nolint:gochecknoglobals // Static lookup table.
var metricsListActions = map[string]bool{
	"add":    true,
	"update": true,
	"remove": true,
	"keep":   true,
	"none":   true,
}

type metricsListOptions struct {
	Actions []string
	Sort    string
	Limit   int
}

type metricListItemV2 struct {
	Metric                      string     `json:"metric"`
	RecommendedAction           string     `json:"recommended_action"`
	RawSeriesCount              *int64     `json:"raw_series_count"`
	CurrentSeriesCountReduction *int64     `json:"current_series_count_reduction"`
	ProjectedSeriesCountChange  *int64     `json:"projected_series_count_change"`
	StatsUpdatedAt              *time.Time `json:"stats_updated_at"`
}

// ListMetrics reads a segment's analyzed metrics, following page tokens up to
// limit. A non-positive limit drains every page.
func (b *rulesBackend) ListMetrics(ctx context.Context, opts metricsListOptions) ([]AdaptiveMetric, error) {
	sortSpec, ok := metricsSorts[opts.Sort]
	if !ok {
		return nil, fmt.Errorf("invalid sort %q", opts.Sort)
	}

	metrics := []AdaptiveMetric{}
	token := ""
	for {
		pageSize := int64(rulesMaxPageSize)
		if opts.Limit > 0 && int64(opts.Limit)-int64(len(metrics)) < pageSize {
			pageSize = int64(opts.Limit) - int64(len(metrics))
		}
		q := url.Values{
			"page_size":      {strconv.FormatInt(pageSize, 10)},
			"sort":           {sortSpec.wire},
			"sort_direction": {sortSpec.direction},
		}
		for _, action := range opts.Actions {
			q.Add("action", action)
		}
		if token != "" {
			q.Set("page_token", token)
		}

		status, body, err := b.send(ctx, b.v2Call(http.MethodGet, "/metrics?"+q.Encode(), nil, rulesReadAction))
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, ruleError("list metrics", status, body)
		}
		var page struct {
			Metrics       []metricListItemV2 `json:"metrics"`
			NextPageToken string             `json:"next_page_token"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("adaptive-metrics: list metrics: decode: %w", err)
		}
		for _, m := range page.Metrics {
			metrics = append(metrics, AdaptiveMetric{
				Metric:                      m.Metric,
				RecommendedAction:           m.RecommendedAction,
				RawSeriesCount:              m.RawSeriesCount,
				CurrentSeriesCountReduction: m.CurrentSeriesCountReduction,
				ProjectedSeriesCountChange:  m.ProjectedSeriesCountChange,
				StatsUpdatedAt:              rfc3339Ptr(m.StatsUpdatedAt),
			})
		}

		if page.NextPageToken == "" || (opts.Limit > 0 && len(metrics) >= opts.Limit) {
			return metrics, nil
		}
		if page.NextPageToken == token {
			return nil, errors.New("adaptive-metrics: list metrics: server repeated a page token")
		}
		token = page.NextPageToken
	}
}

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

type metricDetailV2 struct {
	Metric      string           `json:"metric"`
	Verdict     *metricVerdictV2 `json:"verdict"`
	Current     *ruleV2          `json:"current"`
	Recommended *ruleV2          `json:"recommended"`
	Stats       *metricStatsV2   `json:"stats"`
}

type metricVerdictV2 struct {
	RecommendedAction string `json:"recommended_action"`
}

type metricStatsV2 struct {
	RawSeriesCount         int `json:"raw_series_count"`
	CurrentSeriesCount     int `json:"current_series_count"`
	RecommendedSeriesCount int `json:"recommended_series_count"`
	UsagesInRules          int `json:"usages_in_rules"`
	UsagesInQueries        int `json:"usages_in_queries"`
	UsagesInDashboards     int `json:"usages_in_dashboards"`
}

func (b *rulesBackend) getMetricDetail(ctx context.Context, op, metric string) (*metricDetailV2, error) {
	status, body, err := b.send(ctx, b.v2Call(http.MethodGet, "/metrics/"+url.PathEscape(metric), nil, rulesReadAction))
	if err != nil {
		return nil, err
	}
	if isMetricNotFound(status, body) {
		return nil, fmt.Errorf("metric %s: %w", metric, adapter.ErrNotFound)
	}
	if status != http.StatusOK {
		return nil, ruleError(op, status, body)
	}
	var detail metricDetailV2
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: %s: decode: %w", op, err)
	}
	if detail.Metric == "" {
		detail.Metric = metric
	}
	return &detail, nil
}

// GetMetricDetail reads one analyzed metric's verdict, rules and usage stats.
// A metric the catalog does not list is not found, even when a rule exists for
// it, because the command exposes analyzed metrics rather than rules.
func (b *rulesBackend) GetMetricDetail(ctx context.Context, metric string) (*AdaptiveMetricDetail, error) {
	detail, err := b.getMetricDetail(ctx, "get metric", metric)
	if err != nil {
		return nil, err
	}
	if detail.Verdict == nil {
		return nil, fmt.Errorf("metric %s: %w", metric, adapter.ErrNotFound)
	}

	out := &AdaptiveMetricDetail{Metric: detail.Metric}
	if detail.Verdict != nil {
		out.RecommendedAction = detail.Verdict.RecommendedAction
	}
	if detail.Current != nil {
		rule := fromRuleV2(*detail.Current)
		out.CurrentRule = &rule
	}
	if detail.Recommended != nil {
		rule := fromRuleV2(*detail.Recommended)
		out.RecommendedRule = &rule
	}
	if detail.Stats != nil {
		out.Stats = &AdaptiveMetricStats{
			RawSeriesCount:         detail.Stats.RawSeriesCount,
			CurrentSeriesCount:     detail.Stats.CurrentSeriesCount,
			RecommendedSeriesCount: detail.Stats.RecommendedSeriesCount,
			UsagesInRules:          detail.Stats.UsagesInRules,
			UsagesInQueries:        detail.Stats.UsagesInQueries,
			UsagesInDashboards:     detail.Stats.UsagesInDashboards,
		}
	}
	return out, nil
}

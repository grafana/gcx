package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// applyRecommendationsMaxMetrics is the most metrics one v2 apply call takes.
const applyRecommendationsMaxMetrics = 100

const (
	actionAdd    = "add"
	actionUpdate = "update"
	actionRemove = "remove"

	applyStatusApplied = "applied"
)

// metricRecommendation is what the v2 per-metric detail says about one
// metric's recommendation, in the CLI's rule shape.
type metricRecommendation struct {
	Metric            string
	Action            string
	Current           *MetricRule
	Recommended       *MetricRule
	CurrentSeries     int
	RecommendedSeries int
}

// changesRules reports whether applying the recommendation writes anything.
func (r metricRecommendation) changesRules() bool {
	switch r.Action {
	case actionAdd, actionUpdate, actionRemove:
		return true
	default:
		return false
	}
}

func noRecommendationError(metric string) error {
	return fmt.Errorf("no recommendation found for %s. Use 'recommendations list' to see available recommendations", metric)
}

// Recommendation reads one metric's recommendation. A metric the catalog does
// not list has no recommendation, even when a rule exists for it; the v2
// apply uses the same definition.
func (b *rulesBackend) Recommendation(ctx context.Context, metric string) (metricRecommendation, error) {
	status, body, err := b.send(ctx, b.v2Call(http.MethodGet, "/metrics/"+url.PathEscape(metric), nil, rulesReadAction))
	if err != nil {
		return metricRecommendation{}, err
	}
	if isMetricNotFound(status, body) {
		return metricRecommendation{}, noRecommendationError(metric)
	}
	if status != http.StatusOK {
		return metricRecommendation{}, ruleError("get recommendation", status, body)
	}
	var detail struct {
		Verdict *struct {
			RecommendedAction string `json:"recommended_action"`
		} `json:"verdict"`
		Current     *ruleV2 `json:"current"`
		Recommended *ruleV2 `json:"recommended"`
		Stats       *struct {
			CurrentSeriesCount     int `json:"current_series_count"`
			RecommendedSeriesCount int `json:"recommended_series_count"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return metricRecommendation{}, fmt.Errorf("adaptive-metrics: get recommendation: decode: %w", err)
	}
	if detail.Verdict == nil {
		return metricRecommendation{}, noRecommendationError(metric)
	}
	rec := metricRecommendation{Metric: metric, Action: detail.Verdict.RecommendedAction}
	if detail.Current != nil {
		r := fromRuleV2(*detail.Current)
		rec.Current = &r
	}
	if detail.Recommended != nil {
		r := fromRuleV2(*detail.Recommended)
		rec.Recommended = &r
	}
	if detail.Stats != nil {
		rec.CurrentSeries = detail.Stats.CurrentSeriesCount
		rec.RecommendedSeries = detail.Stats.RecommendedSeriesCount
	}
	return rec, nil
}

func isMetricNotFound(status int, body []byte) bool {
	if status != http.StatusNotFound {
		return false
	}
	var er struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &er) == nil && strings.HasPrefix(er.Error, "metric not found")
}

// RecommendationCounts returns the segment's recommendations counted by
// action.
func (b *rulesBackend) RecommendationCounts(ctx context.Context) (map[string]int, error) {
	status, body, err := b.send(ctx, b.v2Call(http.MethodGet, "/metrics:summary", nil, rulesReadAction))
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, ruleError("summarize recommendations", status, body)
	}
	var summary struct {
		CountsByAction map[string]int `json:"counts_by_action"`
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: summarize recommendations: decode: %w", err)
	}
	return summary.CountsByAction, nil
}

type applyResult struct {
	Metric string `json:"metric"`
	Status string `json:"status"`
}

type applySummary struct {
	Upserted  int `json:"upserted"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
}

type applyWarning struct {
	Code    string `json:"code"`
	Skipped int    `json:"skipped"`
}

type applyResponse struct {
	Results  []applyResult  `json:"results"`
	Summary  *applySummary  `json:"summary"`
	Warnings []applyWarning `json:"warnings"`
}

// ApplyRecommendations applies the listed metrics' recommendations in one
// atomic write. Every metric must have a recommendation.
func (b *rulesBackend) ApplyRecommendations(ctx context.Context, metrics []string) ([]applyResult, error) {
	resp, err := b.apply(ctx, map[string]any{"metrics": metrics})
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// ApplyAllRecommendations applies every recommendation of the segment. The
// server commits in chunks, so on failure the returned response still counts
// what was committed.
func (b *rulesBackend) ApplyAllRecommendations(ctx context.Context) (applyResponse, error) {
	return b.apply(ctx, map[string]any{"strategy": "all"})
}

func (b *rulesBackend) apply(ctx context.Context, req map[string]any) (applyResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return applyResponse{}, fmt.Errorf("adaptive-metrics: apply recommendations: marshal: %w", err)
	}
	status, body, err := b.send(ctx, b.v2Call(http.MethodPost, "/rules:applyRecommendations", data, rulesWriteAction))
	if err != nil {
		return applyResponse{}, err
	}
	var resp applyResponse
	decodeErr := json.Unmarshal(body, &resp)
	if status != http.StatusOK {
		return resp, ruleError("apply recommendations", status, body)
	}
	if decodeErr != nil {
		return applyResponse{}, fmt.Errorf("adaptive-metrics: apply recommendations: decode: %w", decodeErr)
	}
	return resp, nil
}

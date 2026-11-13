package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
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
	detail, err := b.getMetricDetail(ctx, "get recommendation", metric)
	if err != nil {
		if errors.Is(err, adapter.ErrNotFound) {
			return metricRecommendation{}, noRecommendationError(metric)
		}
		return metricRecommendation{}, err
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
	var er providers.ErrorResponse
	return json.Unmarshal(body, &er) == nil && strings.HasPrefix(er.ErrorMessage(), "metric not found")
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
// atomic write. Every metric must have a recommendation. This endpoint returns
// per-metric results; summary and warning fields are used only by segment-wide apply.
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

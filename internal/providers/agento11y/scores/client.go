package scores

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
)

const (
	generationScoresPathFmt = "/query/generations/%s/scores"
	ruleScoresPathFmt       = "/eval/rules/%s/scores"

	// ruleScoreDefaultLimit is higher than the generation default (50) because
	// rule scores are the failure-theme-analysis surface (the llm_hint suggests
	// --limit 100); a single generation's score set is small, so 50 suffices.
	ruleScoreDefaultLimit = 100
	ruleScoreMaxValues    = 20

	// scoreMaxPageLimit is the largest per-request page size sent to the API.
	scoreMaxPageLimit = 500
	// scoreHardCap bounds the total rows fetched when no smaller user --limit
	// applies. Both rule and generation score tables can grow large, so an
	// unbounded --limit 0 could load an unbounded set into memory; the cap keeps
	// that bounded and is disclosed to the caller via ListMeta.Cap.
	scoreHardCap = 1000
)

// Client is an HTTP client for Agent Observability generation score endpoints.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a new scores client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// ListByGeneration returns scores for a generation, paginated. A generation has
// no filter flags, so an explicit --limit is the caller's route to more rows and
// is honored verbatim; only --limit 0 ("all") is bounded by scoreHardCap as an
// out-of-memory backstop. Page size is bounded by scoreMaxPageLimit. The bool is
// true when the fetch was truncated and more rows exist beyond it.
func (c *Client) ListByGeneration(ctx context.Context, generationID string, limit int) ([]Score, bool, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(scorePageLimit(limit)))
	fetch := limit
	if fetch <= 0 {
		fetch = scoreHardCap
	}
	return agento11yhttp.ListAllWithHasMore[Score](ctx, c.base, fmt.Sprintf(generationScoresPathFmt, url.PathEscape(generationID)), query, fetch)
}

// ListScoresOptions holds filters for ListByRule.
type ListScoresOptions struct {
	Limit       int
	EvaluatorID string
	Passed      *bool
	From        string
	To          string
	AgentNames  []string
	Models      []string
	Providers   []string
	ScoreValues []string
	MinValue    *float64
	MaxValue    *float64
	SortBy      string
	SortDir     string
}

// ListByRule returns online evaluation score rows for a rule, paginated. The
// bool is true when more rows exist beyond the returned page. opts is assumed
// valid: the sole caller (the CLI command) validates it before calling.
func (c *Client) ListByRule(ctx context.Context, ruleID string, opts ListScoresOptions) ([]Score, bool, error) {
	query := buildRuleScoreQuery(opts)
	items, hasMore, err := agento11yhttp.ListAllWithHasMore[Score](ctx, c.base, fmt.Sprintf(ruleScoresPathFmt, url.PathEscape(ruleID)), query, ruleScoreFetchCap(opts.Limit))
	return items, hasMore, err
}

// ruleScoreFetchCap is the total-rows bound passed to ListAll as maxItems for the
// rule command. A user --limit within (0, scoreHardCap] is honored verbatim; 0
// ("no limit") or a limit above the cap is bounded by scoreHardCap — a rule's
// score table grows with traffic and filters, not a larger --limit, are the route
// beyond the cap. Validation rejects negatives upstream.
func ruleScoreFetchCap(limit int) int {
	if limit <= 0 || limit > scoreHardCap {
		return scoreHardCap
	}
	return limit
}

// ScoreHardCap reports the safety cap for use as PagedListMeta's safetyCap and as
// a test assertion hook.
func ScoreHardCap() int { return scoreHardCap }

// scorePageLimit is the per-request limit query param sent to the API.
// The server caps page size at scoreMaxPageLimit; use that when fetching all pages.
func scorePageLimit(limit int) int {
	if limit <= 0 || limit > scoreMaxPageLimit {
		return scoreMaxPageLimit
	}
	return limit
}

// buildRuleScoreQuery renders validated options into query params. It does not
// validate; the CLI command validates before ListByRule is reached, so sort
// fields are non-empty and filter slices carry no empty entries (guaranteed by
// listRuleOpts.Validate).
func buildRuleScoreQuery(opts ListScoresOptions) url.Values {
	params := url.Values{}
	params.Set("sort_by", opts.SortBy)
	params.Set("sort_dir", opts.SortDir)
	params.Set("limit", strconv.Itoa(scorePageLimit(opts.Limit)))

	if opts.EvaluatorID != "" {
		params.Set("evaluator_id", opts.EvaluatorID)
	}
	if opts.Passed != nil {
		params.Set("passed", strconv.FormatBool(*opts.Passed))
	}
	if opts.From != "" {
		params.Set("from", opts.From)
	}
	if opts.To != "" {
		params.Set("to", opts.To)
	}
	if opts.MinValue != nil {
		params.Set("min_value", strconv.FormatFloat(*opts.MinValue, 'f', -1, 64))
	}
	if opts.MaxValue != nil {
		params.Set("max_value", strconv.FormatFloat(*opts.MaxValue, 'f', -1, 64))
	}
	for _, v := range opts.AgentNames {
		params.Add("agent_name", v)
	}
	for _, v := range opts.Models {
		params.Add("model", v)
	}
	for _, v := range opts.Providers {
		params.Add("provider", v)
	}
	for _, v := range opts.ScoreValues {
		params.Add("score_value", v)
	}
	return params
}

package assistant

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"golang.org/x/sync/errgroup"
)

// MonthlyUsage is a server-reported counter and its applicable monthly limit.
// A zero limit means unlimited. EffectiveLimit, when present, is the enforced
// investigation cap; Limit remains the customer's configured cap.
type MonthlyUsage struct {
	Total          int64  `json:"total"`
	Limit          int64  `json:"limit"`
	Month          string `json:"month"`
	Scope          string `json:"scope,omitempty"`
	EffectiveLimit *int64 `json:"effectiveLimit,omitempty"`
}

// Usage contains the monthly limits displayed by Grafana Assistant.
type Usage struct {
	Admin          *AdminUsage  `json:"admin,omitempty"`
	Prompts        MonthlyUsage `json:"prompts"`
	Tokens         MonthlyUsage `json:"tokens"`
	Investigations MonthlyUsage `json:"investigations"`
}

// FetchUsage reads all three counters, regardless of the requested output format.
// A failed or malformed response fails the snapshot instead of implying zero use.
func FetchUsage(ctx context.Context, client *assistanthttp.Client) (*Usage, error) {
	result := &Usage{}
	group, ctx := errgroup.WithContext(ctx)
	for _, counter := range []struct {
		path string
		dst  *MonthlyUsage
	}{
		{"prompt", &result.Prompts},
		{"tokens", &result.Tokens},
		{"investigation", &result.Investigations},
	} {
		group.Go(func() error {
			// Pointers distinguish missing required values from an actual zero.
			data, err := assistanthttp.DoEnvelopeRequest[*struct {
				MonthlyUsage

				Total *int64 `json:"total"`
				Limit *int64 `json:"limit"`
			}](client, ctx, http.MethodGet, "/api/v1/usage/limits/"+counter.path, nil, "get "+counter.path+" usage")
			if err != nil {
				return err
			}
			if *data == nil || (*data).Total == nil || (*data).Limit == nil {
				return fmt.Errorf("%s usage response is missing required data, total, or limit", counter.path)
			}
			value := (*data).MonthlyUsage
			value.Total, value.Limit = *(*data).Total, *(*data).Limit
			if value.Total < 0 || value.Limit < 0 || (value.EffectiveLimit != nil && *value.EffectiveLimit < 0) {
				return fmt.Errorf("%s usage response contains a negative count or limit", counter.path)
			}
			if _, err := time.Parse("2006-01", value.Month); err != nil {
				return fmt.Errorf("%s usage response contains an invalid month: %w", counter.path, err)
			}
			if counter.path == "tokens" && value.Scope == "" {
				return errors.New("tokens usage response is missing its scope")
			}
			*counter.dst = value
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

package assistant

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	// Seats is nil unless the caller may read the seat limit (usage:write).
	Seats *SeatUsage `json:"seats,omitempty"`
	// MyLimitRequests is nil when the stack does not serve limit requests.
	MyLimitRequests *MyLimitRequests `json:"myLimitRequests,omitempty"`
	// LimitRequestGroups is nil unless the caller may list pending requests.
	LimitRequestGroups []LimitRequestGroup `json:"limitRequestGroups,omitempty"`
}

// FetchLimitContext adds the caller's pending limit requests and, for admins,
// seats and pending request groups to usage. Stacks without limit requests,
// and callers without permission, leave those fields nil instead of failing.
func FetchLimitContext(ctx context.Context, client *assistanthttp.Client, usage *Usage) error {
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		mine, err := fetchMyPendingLimitRequests(ctx, client)
		usage.MyLimitRequests = mine
		return err
	})
	if usage.Admin != nil {
		group.Go(func() error {
			seats, err := fetchSeatUsage(ctx, client)
			usage.Seats = seats
			return err
		})
		group.Go(func() error {
			groups, err := fetchPendingLimitRequestGroups(ctx, client, usage.Admin.Teams)
			usage.LimitRequestGroups = groups
			return err
		})
	}
	return group.Wait()
}

// LimitsReached returns the requestable limits blocking the caller: a token
// limit the counters show as used up, and any limit the caller already asked
// about. A seat block is only known from a pending request, because people
// without a seat cannot read the seat count.
func (u Usage) LimitsReached() []string {
	reached := make([]string, 0)
	if limit := u.Tokens.enforcedLimit(); limit > 0 && u.Tokens.Total >= limit {
		key := LimitUserTokens
		if u.Tokens.Scope == "stack" {
			key = LimitStackTokens
		}
		reached = append(reached, key)
	}
	if u.MyLimitRequests != nil {
		for _, request := range u.MyLimitRequests.Requests {
			if !slices.Contains(reached, request.LimitKey) {
				reached = append(reached, request.LimitKey)
			}
		}
	}
	slices.SortStableFunc(reached, func(a, b string) int {
		return cmp.Compare(slices.Index(RequestableLimits, a), slices.Index(RequestableLimits, b))
	})
	return reached
}

// InvestigationsReached reports whether the stack's investigation limit is used up.
func (u Usage) InvestigationsReached() bool {
	limit := u.Investigations.enforcedLimit()
	return limit > 0 && u.Investigations.Total >= limit
}

func (m MonthlyUsage) enforcedLimit() int64 {
	if m.EffectiveLimit != nil {
		return *m.EffectiveLimit
	}
	return m.Limit
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

package assistant

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

// UsageTimeRange is an API-reported UTC reporting period.
type UsageTimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// UsageMember contains current-month usage and the resolved per-user cap.
// Service accounts share a pool; their EffectiveLimitMtok is not a personal cap.
type UsageMember struct {
	ID                              string           `json:"id"`
	UserType                        string           `json:"userType"`
	Email                           string           `json:"email"`
	GrafanaUserID                   string           `json:"grafanaUserId,omitempty"`
	TeamUIDs                        []string         `json:"teamUids"`
	UsedSelectedRangeTokens         int64            `json:"usedSelectedRangeTokens"`
	UsedThisMonthTokens             int64            `json:"usedThisMonthTokens"`
	UsedSelectedRangeTokensBySource map[string]int64 `json:"usedSelectedRangeTokensBySource,omitempty"`
	InvestigationTokens             int64            `json:"investigationTokens"`
	InvestigationsCount             int64            `json:"investigationsCount"`
	EffectiveLimitMtok              int64            `json:"effectiveLimitMtok,omitempty"`
	LimitSource                     string           `json:"limitSource"`
	LimitSourceTeamUIDs             []string         `json:"limitSourceTeamUids,omitempty"`
}

// AdminUsage is the complete, unpaginated members snapshot from the Usage API.
// Limits ending in Mtok are millions of tokens; usage counters are raw tokens.
type AdminUsage struct {
	ThisMonth                UsageTimeRange   `json:"thisMonth"`
	StackUsedThisMonthTokens int64            `json:"stackUsedThisMonthTokens"`
	Members                  []UsageMember    `json:"members"`
	StackTotalLimitMtok      int64            `json:"stackTotalLimitMtok,omitempty"`
	DefaultLimits            map[string]int64 `json:"defaultLimits"`
	TeamDataPartial          bool             `json:"teamDataPartial,omitempty"`
}

// FetchAdminUsage reads the permission-gated snapshot used by the Usage page.
func FetchAdminUsage(ctx context.Context, client *assistanthttp.Client) (*AdminUsage, error) {
	data, err := assistanthttp.DoEnvelopeRequest[*struct {
		AdminUsage

		StackUsedThisMonthTokens *int64         `json:"stackUsedThisMonthTokens"`
		Members                  *[]UsageMember `json:"members"`
	}](client, ctx, http.MethodGet, "/api/v1/usage/tokens/teams", nil, "get admin usage")
	if err != nil {
		if _, ok := errors.AsType[*assistanthttp.ForbiddenError](err); ok {
			return nil, fmt.Errorf("admin usage requires grafana-assistant-app.usage:read permission: %w", err)
		}
		return nil, err
	}
	if *data == nil || (*data).StackUsedThisMonthTokens == nil || (*data).Members == nil || (*data).ThisMonth.Start.IsZero() || (*data).ThisMonth.End.IsZero() {
		return nil, errors.New("admin usage response is missing the stack counter, members, or monthly period")
	}
	result := (*data).AdminUsage
	result.StackUsedThisMonthTokens = *(*data).StackUsedThisMonthTokens
	result.Members = *(*data).Members
	if !result.ThisMonth.End.After(result.ThisMonth.Start) {
		return nil, errors.New("admin usage response contains an invalid monthly period")
	}
	limits := []int64{result.StackTotalLimitMtok, result.DefaultLimits["stackTotalMtok"]}
	if result.StackUsedThisMonthTokens < 0 {
		return nil, errors.New("admin usage response contains negative stack usage")
	}
	for i := range result.Members {
		member := &result.Members[i]
		if member.UsedThisMonthTokens < 0 {
			return nil, errors.New("admin usage response contains negative member usage")
		}
		limits = append(limits, member.EffectiveLimitMtok)
		if member.TeamUIDs == nil {
			member.TeamUIDs = []string{}
		}
	}
	for _, limit := range limits {
		if limit < 0 || limit > math.MaxInt64/tokensPerMillion {
			return nil, errors.New("admin usage response contains an invalid token limit")
		}
	}
	return &result, nil
}

const tokensPerMillion int64 = 1_000_000

// UsersNeedingAttention returns users at or above 80% of their personal cap,
// most utilized first. It never mutates the snapshot or treats a shared
// service-account pool as an individual cap. An empty result is a non-nil slice.
func (a *AdminUsage) UsersNeedingAttention() []UsageMember {
	members := make([]UsageMember, 0)
	for _, member := range a.Members {
		if member.UserType == "user" && member.EffectiveLimitMtok > 0 && memberUtilization(member) >= .8 {
			members = append(members, member)
		}
	}
	slices.SortFunc(members, func(a, b UsageMember) int {
		if order := cmp.Compare(memberUtilization(b), memberUtilization(a)); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return members
}

func memberUtilization(member UsageMember) float64 {
	return float64(member.UsedThisMonthTokens) / (float64(member.EffectiveLimitMtok) * float64(tokensPerMillion))
}

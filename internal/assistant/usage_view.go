package assistant

import (
	"cmp"
	"slices"
	"strings"
)

// UsageMetric contains exact counts and the applicable limit for one month.
// A zero limit means no cap is reported by the endpoint.
type UsageMetric struct {
	Used  int64  `json:"used"`
	Limit int64  `json:"limit"`
	Month string `json:"month"`
}

// PersonalUsageView contains only metrics scoped to the current user.
type PersonalUsageView struct {
	Metrics map[string]UsageMetric `json:"metrics"`
}

// OrganizationUsageView contains shared metrics and users needing attention.
type OrganizationUsageView struct {
	Metrics               map[string]UsageMetric `json:"metrics"`
	UsersNeedingAttention []UsageMember          `json:"usersNeedingAttention"`
	TeamDataPartial       bool                   `json:"teamDataPartial,omitempty"`
}

// UsageView mirrors the personal and organization sections of the terminal view.
type UsageView struct {
	YourUsage PersonalUsageView      `json:"yourUsage"`
	OrgUsage  *OrganizationUsageView `json:"orgUsage,omitempty"`
}

// JSONView groups metrics by ownership. Shared counters appear in orgUsage for
// every caller; only the users-needing-attention list requires administrator
// access, and it is null when that data is unavailable.
func (usage Usage) JSONView() UsageView {
	view := UsageView{YourUsage: PersonalUsageView{Metrics: make(map[string]UsageMetric)}}
	org := &OrganizationUsageView{Metrics: make(map[string]UsageMetric)}
	for _, row := range usageSummaryRows(usage) {
		key := strings.ToLower(row.name)
		metric := UsageMetric{Used: row.total, Limit: row.limit, Month: row.month}
		if row.scope == "user" {
			view.YourUsage.Metrics[key] = metric
		} else {
			org.Metrics[key] = metric
		}
	}
	if usage.Admin != nil {
		org.UsersNeedingAttention = usage.Admin.UsersNeedingAttention()
		org.TeamDataPartial = usage.Admin.TeamDataPartial
	}
	if usage.Admin != nil || len(org.Metrics) > 0 {
		view.OrgUsage = org
	}
	return view
}

// usageSummaryRow keeps presentation ordering separate from the API result.
type usageSummaryRow struct {
	name, scope, month string
	total, limit       int64
}

func usageSummaryRows(usage Usage) []usageSummaryRow {
	rows := make([]usageSummaryRow, 0, 4)
	for _, counter := range []struct {
		name, fallbackScope string
		usage               MonthlyUsage
	}{
		{"Prompts", "user", usage.Prompts},
		{"Tokens", "", usage.Tokens},
		{"Investigations", "stack", usage.Investigations},
	} {
		limit := counter.usage.Limit
		if counter.usage.EffectiveLimit != nil {
			limit = *counter.usage.EffectiveLimit
		}
		scope := counter.usage.Scope
		if scope == "" {
			scope = counter.fallbackScope
		}
		rows = append(rows, usageSummaryRow{counter.name, scope, counter.usage.Month, counter.usage.Total, limit})
	}
	if admin := usage.Admin; admin != nil {
		// Keep the caller endpoint's enforced cap when it reports this same bucket.
		sameStack := usage.Tokens.Scope == "stack" && usage.Tokens.Month == admin.ThisMonth.Start.UTC().Format("2006-01")
		if !sameStack {
			limit := admin.StackTotalLimitMtok
			if limit == 0 {
				limit = admin.DefaultLimits["stackTotalMtok"]
			}
			rows = append(rows, usageSummaryRow{"Tokens", "stack", admin.ThisMonth.Start.UTC().Format("2006-01"), admin.StackUsedThisMonthTokens, limit * tokensPerMillion})
		}
	}
	// Rank actual reported caps by utilization, including shared stack caps.
	// Stable ties retain the metric order; uncapped counters follow all caps.
	slices.SortStableFunc(rows, func(a, b usageSummaryRow) int {
		if a.limit == 0 && b.limit == 0 {
			return 0
		}
		if a.limit == 0 {
			return 1
		}
		if b.limit == 0 {
			return -1
		}
		return cmp.Compare(float64(b.total)/float64(b.limit), float64(a.total)/float64(a.limit))
	})
	return rows
}

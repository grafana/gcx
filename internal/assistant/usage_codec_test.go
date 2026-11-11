package assistant_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageBars(t *testing.T) {
	for _, tc := range []struct {
		name        string
		used, limit int64
		want        string
	}{
		{"zero", 0, 100, "[------------]   0.0%"},
		{"half", 50, 100, "[######------]  50.0%"},
		{"full", 100, 100, "[############] 100.0%"},
		{"over", 150, 100, "[############] 150.0%"},
		{"unlimited", 42, 0, "No cap reported"},
		{"large count", math.MaxInt64, math.MaxInt64, "[############] 100.0%"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, (&assistant.UsageTextCodec{}).Encode(&out, assistant.Usage{Prompts: assistant.MonthlyUsage{Total: tc.used, Limit: tc.limit}}))
			assert.Contains(t, out.String(), tc.want)
		})
	}
}

func TestUsageTextEffectiveLimit(t *testing.T) {
	zero, capLimit := int64(0), int64(10)
	for _, tc := range []struct {
		name      string
		effective *int64
		want      string
	}{
		{"older server fallback", nil, "5 / 100"},
		{"enforced cap", &capLimit, "5 / 10"},
		{"explicit unlimited", &zero, "No cap reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, (&assistant.UsageTextCodec{}).Encode(&out, assistant.Usage{Investigations: assistant.MonthlyUsage{Total: 5, Limit: 100, EffectiveLimit: tc.effective, Scope: "user"}}))
			assert.Contains(t, out.String(), tc.want)
			if tc.effective != nil && *tc.effective == 0 {
				assert.Regexp(t, `Investigations\s+5\s+No cap reported`, out.String())
			}
		})
	}
}

func TestUsageAdminServiceAccountsAndEmpty(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"empty", `{"members":[]}`, "No users at or above 80%"},
		{"shared pool", `{"members":[{"id":"robot","userType":"service_account","usedThisMonthTokens":500,"effectiveLimitMtok":1}]}`, "No users at or above 80%"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var admin assistant.AdminUsage
			require.NoError(t, json.Unmarshal([]byte(tc.body), &admin))
			var out bytes.Buffer
			require.NoError(t, (&assistant.UsageTextCodec{}).Encode(&out, assistant.Usage{Admin: &admin}))
			assert.Contains(t, out.String(), tc.want)
			if tc.name == "shared pool" {
				assert.NotContains(t, out.String(), "robot")
				assert.NotContains(t, out.String(), "0.1%")
			}
		})
	}
}

type usageFailingWriter struct{}

func (usageFailingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestUsageCodecErrors(t *testing.T) {
	codec := &assistant.UsageTextCodec{}
	require.Error(t, codec.Encode(&bytes.Buffer{}, "wrong type"))
	require.ErrorContains(t, codec.Encode(usageFailingWriter{}, assistant.Usage{}), "write failed")
	require.Error(t, codec.Decode(&bytes.Buffer{}, nil))
}

func TestUsageSummaryPrioritizesReportedLimits(t *testing.T) {
	month := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	zero, effective := int64(0), int64(100)
	for _, tc := range []struct {
		name  string
		usage assistant.Usage
		want  []string
	}{
		{
			name: "reached cap first then closest to limit",
			usage: assistant.Usage{
				Prompts:        assistant.MonthlyUsage{Total: 10, Limit: 100},
				Tokens:         assistant.MonthlyUsage{Total: 120, Limit: 100, Scope: "user"},
				Investigations: assistant.MonthlyUsage{Total: 80, Limit: 1000, EffectiveLimit: &effective},
			},
			want: []string{"YOUR USAGE:Tokens", "YOUR USAGE:Prompts"},
		},
		{
			name: "shared caps before unreported prompt cap",
			usage: assistant.Usage{
				Prompts:        assistant.MonthlyUsage{Total: 10000},
				Tokens:         assistant.MonthlyUsage{Total: 1, Limit: 100, Scope: "user"},
				Investigations: assistant.MonthlyUsage{Total: 25, Limit: 100},
				Admin:          &assistant.AdminUsage{StackUsedThisMonthTokens: 100000, StackTotalLimitMtok: 1},
			},
			want: []string{"YOUR USAGE:Tokens", "YOUR USAGE:Prompts", "ORGANIZATION:Investigations", "ORGANIZATION:Tokens"},
		},
		{
			name: "zero usage still has a reachable cap",
			usage: assistant.Usage{
				Tokens:         assistant.MonthlyUsage{Limit: 100, Scope: "user"},
				Investigations: assistant.MonthlyUsage{Limit: 10},
			},
			want: []string{"YOUR USAGE:Tokens", "YOUR USAGE:Prompts"},
		},
		{
			name: "explicit effective zero is not a configured cap",
			usage: assistant.Usage{
				Prompts:        assistant.MonthlyUsage{Total: 10, Limit: 100},
				Tokens:         assistant.MonthlyUsage{Total: 20, Limit: 100, Scope: "user"},
				Investigations: assistant.MonthlyUsage{Total: 999, Limit: 10, EffectiveLimit: &zero},
			},
			want: []string{"YOUR USAGE:Tokens", "YOUR USAGE:Prompts"},
		},
		{
			name: "explicit investigation scope is respected",
			usage: assistant.Usage{
				Prompts:        assistant.MonthlyUsage{Limit: 100},
				Tokens:         assistant.MonthlyUsage{Limit: 100, Scope: "service_account"},
				Investigations: assistant.MonthlyUsage{Total: 9, Limit: 10, Scope: "user"},
			},
			want: []string{"YOUR USAGE:Investigations", "YOUR USAGE:Prompts"},
		},
		{
			name: "same stack token bucket is displayed once",
			usage: assistant.Usage{
				Prompts:        assistant.MonthlyUsage{Limit: 100},
				Tokens:         assistant.MonthlyUsage{Total: 120, Limit: 100, Scope: "stack", Month: "2026-11"},
				Investigations: assistant.MonthlyUsage{Limit: 10},
				Admin:          &assistant.AdminUsage{ThisMonth: assistant.UsageTimeRange{Start: month}, StackTotalLimitMtok: 1},
			},
			want: []string{"YOUR USAGE:Prompts", "ORGANIZATION:Tokens", "ORGANIZATION:Investigations"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.usage)
			require.NoError(t, err)
			var out bytes.Buffer
			require.NoError(t, (&assistant.UsageTextCodec{}).Encode(&out, tc.usage))
			var rows []string
			section := ""
			for line := range strings.SplitSeq(out.String(), "\n") {
				if line == "YOUR USAGE" || line == "ORGANIZATION" {
					section = line
				}
				fields := strings.Fields(line)
				if len(fields) > 1 && (fields[0] == "Prompts" || fields[0] == "Tokens" || fields[0] == "Investigations") {
					rows = append(rows, section+":"+fields[0])
				}
			}
			assert.Equal(t, tc.want, rows)
			assert.NotContains(t, out.String(), "SCOPE")
			after, err := json.Marshal(tc.usage)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after), "human ordering must not alter JSON data")
			if tc.name == "same stack token bucket is displayed once" {
				assert.Contains(t, out.String(), "120 / 100")
			}
		})
	}
}

func TestUsageAdminAttentionList(t *testing.T) {
	admin := &assistant.AdminUsage{Members: []assistant.UsageMember{
		{ID: "healthy", UserType: "user", UsedThisMonthTokens: 79000000, EffectiveLimitMtok: 100},
		{ID: "near", UserType: "user", UsedThisMonthTokens: 800000, EffectiveLimitMtok: 1},
		{ID: "reached", UserType: "user", UsedThisMonthTokens: 2000000, EffectiveLimitMtok: 2},
		{ID: "over", UserType: "user", UsedThisMonthTokens: 1500000, EffectiveLimitMtok: 1},
		{ID: "uncapped", UserType: "user", UsedThisMonthTokens: 1000000000},
		{ID: "robot", UserType: "service_account", UsedThisMonthTokens: 1000000000, EffectiveLimitMtok: 1},
	}}
	for _, top := range []int{0, 2} {
		var out bytes.Buffer
		require.NoError(t, (&assistant.UsageTextCodec{Top: &top}).Encode(&out, assistant.Usage{Admin: admin}))
		text := out.String()
		assert.NotContains(t, text, "healthy")
		assert.NotContains(t, text, "uncapped")
		assert.NotContains(t, text, "robot")
		assert.Less(t, strings.Index(text, "over"), strings.Index(text, "reached"))
		assert.Contains(t, text, "Limit reached")
		if top == 0 {
			assert.Contains(t, text, "showing 3 of 3")
			assert.Contains(t, text, "800K / 1M")
			assert.Contains(t, text, "Near limit")
		} else {
			assert.Contains(t, text, "showing 2 of 3")
			assert.NotContains(t, text, "800K / 1M")
			assert.Contains(t, text, "gcx assistant usage get --top 0")
		}
	}
	assert.Equal(t, "healthy", admin.Members[0].ID, "text sorting must not alter the underlying snapshot")
}

func TestUsageSharedLimitNotice(t *testing.T) {
	zero := int64(0)
	for _, tc := range []struct {
		name       string
		total      int64
		effective  *int64
		wantNotice bool
	}{
		{name: "healthy", total: 25},
		{name: "near", total: 90},
		{name: "reached", total: 100, wantNotice: true},
		{name: "over", total: 120, wantNotice: true},
		{name: "effective unlimited", total: 120, effective: &zero},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, (&assistant.UsageTextCodec{}).Encode(&out, assistant.Usage{
				Tokens:         assistant.MonthlyUsage{Total: tc.total, Limit: 100, Month: "2026-12", Scope: "stack", EffectiveLimit: tc.effective},
				Investigations: assistant.MonthlyUsage{Total: tc.total, Limit: 100, Month: "2026-12", EffectiveLimit: tc.effective},
			}))
			assert.NotContains(t, out.String(), "ORGANIZATION")
			assert.NotContains(t, out.String(), "SCOPE")
			assert.NotContains(t, out.String(), "Investigations")
			assert.NotContains(t, out.String(), "Tokens")
			if tc.wantNotice {
				// Without limit requests on the stack, there is nothing to run.
				assert.Contains(t, out.String(), "✗ This stack has used all its investigations this month.\n  The limit is 100 investigations. It resets on Jan 1 (UTC).\n  Contact your Assistant Admins to raise it.")
				assert.Contains(t, out.String(), "✗ This stack has used all its Assistant tokens this month.\n  The stack's limit is 100 tokens. It resets on Jan 1 (UTC).\n  Contact your Assistant Admins to raise it.")
			} else {
				assert.NotContains(t, out.String(), "✗")
			}
			assert.NotContains(t, out.String(), "limit-requests create")
		})
	}
}

func TestUsageServiceAccountPoolBelongsToOrganization(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, (&assistant.UsageTextCodec{}).Encode(&out, assistant.Usage{
		Tokens: assistant.MonthlyUsage{Total: 800, Limit: 1000, Scope: "service_account"},
		Admin:  &assistant.AdminUsage{},
	}))
	personal, organization, ok := strings.Cut(out.String(), "ORGANIZATION")
	require.True(t, ok)
	assert.NotContains(t, personal, "800")
	assert.Contains(t, organization, "Service-account tokens")
	assert.Contains(t, organization, "800 / 1,000")
}

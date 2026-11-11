package assistant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	assistantcmd "github.com/grafana/gcx/internal/providers/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adminUsageFixture = `{"data":{"generatedAt":"2026-11-10T12:00:00Z","thisMonth":{"start":"2026-11-01T00:00:00Z","end":"2026-12-01T00:00:00Z"},"stackUsedThisMonthTokens":5000000,"stackTotalLimitMtok":10,"teamDataPartial":true,"members":[{"id":"a","email":"alice@example.com","userType":"user","usedThisMonthTokens":1000000,"effectiveLimitMtok":2},{"id":"b","email":"bob@example.com","userType":"user","usedThisMonthTokens":3000000,"effectiveLimitMtok":2},{"id":"sa","userType":"service_account","usedThisMonthTokens":500000,"effectiveLimitMtok":10}],"teams":[]}}`

func TestUsageCommand(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		agentMode  bool
		wantJSON   bool
		wantAdmin  bool
		adminBody  string
		wantOutput string
		wantCount  int
	}{
		{name: "human default"},
		{name: "JSON", args: []string{"-o", "json"}, wantJSON: true, wantAdmin: true, wantCount: 1},
		{name: "agent default", agentMode: true, wantJSON: true, wantAdmin: true, wantCount: 1},
		{name: "agent explicit text", agentMode: true, args: []string{"-o", "text"}},
		{name: "admin detected automatically", args: []string{"--top", "1"}, wantAdmin: true},
		{name: "JSON includes all at-risk users despite top", args: []string{"--top", "1", "-o", "json"}, wantJSON: true, wantAdmin: true, wantCount: 2,
			adminBody: strings.Replace(adminUsageFixture, `"members":[`, `"members":[{"id":"edge","userType":"user","usedThisMonthTokens":1600000,"effectiveLimitMtok":2},{"id":"below","userType":"user","usedThisMonthTokens":1599999,"effectiveLimitMtok":2},{"id":"uncapped","userType":"user","usedThisMonthTokens":9000000},`, 1)},
		{name: "JSON without admin permission", args: []string{"-o", "json"}, wantJSON: true},
		{name: "agent without admin permission", agentMode: true, wantJSON: true},
		{name: "empty attention array", args: []string{"-o", "json"}, wantAdmin: true, wantJSON: true, adminBody: strings.Replace(adminUsageFixture, `"usedThisMonthTokens":3000000`, `"usedThisMonthTokens":1000000`, 1)},
		{name: "JSON field selection", args: []string{"--json", "yourUsage"}, wantAdmin: true, wantOutput: `{"yourUsage":{"metrics":{"prompts":{"used":30,"limit":100,"month":"2026-11"},"tokens":{"used":42,"limit":0,"month":"2026-11"}}}}`},
		{name: "jq filters attention array", args: []string{"--jq", ".orgUsage.usersNeedingAttention | length"}, wantAdmin: true, wantOutput: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.ResetForTesting() })
			var mu sync.Mutex
			requests := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				path := strings.TrimPrefix(r.URL.Path, "/api/plugins/grafana-assistant-app/resources/api/v1/usage/")
				if path != r.URL.Path {
					assert.Empty(t, r.URL.RawQuery)
				}
				mu.Lock()
				requests[path]++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch path {
				case "limits/prompt":
					fmt.Fprint(w, `{"data":{"total":30,"limit":100,"month":"2026-11"}}`)
				case "limits/tokens":
					fmt.Fprint(w, `{"data":{"total":42,"limit":0,"month":"2026-11","scope":"user"}}`)
				case "limits/investigation":
					fmt.Fprint(w, `{"data":{"total":25,"limit":500,"effectiveLimit":100,"month":"2026-11"}}`)
				case "tokens/teams":
					if !tc.wantAdmin {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					body := tc.adminBody
					if body == "" {
						body = adminUsageFixture
					}
					fmt.Fprint(w, body)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			cfg := writeAssistantTestConfig(t, server.URL)
			root := assistantcmd.Command()
			root.SilenceUsage = true
			root.SilenceErrors = true
			root.SetContext(context.Background())
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			args := make([]string, 0, 4+len(tc.args))
			args = append(args, "usage", "get", "--config", cfg)
			root.SetArgs(append(args, tc.args...))
			require.NoError(t, root.Execute())
			for _, path := range []string{"limits/prompt", "limits/tokens", "limits/investigation"} {
				assert.Equal(t, 1, requests[path])
			}
			assert.Equal(t, 1, requests["tokens/teams"])
			if tc.wantOutput != "" {
				assert.JSONEq(t, tc.wantOutput, stdout.String())
				return
			}
			if tc.wantJSON {
				assertUsageJSON(t, stdout.Bytes(), tc.wantAdmin, tc.wantCount)
				return
			}
			assert.Contains(t, stdout.String(), "YOUR USAGE")
			assert.Contains(t, stdout.String(), "[####--------]  30.0%")
			assert.Contains(t, stdout.String(), "No cap reported")
			assert.NotContains(t, stdout.String(), "SCOPE")
			if tc.wantAdmin {
				personal, organization, found := strings.Cut(stdout.String(), "ORGANIZATION")
				require.True(t, found)
				assert.NotContains(t, personal, "Investigations")
				assert.Contains(t, organization, "25 / 100")
				assert.Contains(t, stdout.String(), "5M / 10M")
				assert.Contains(t, stdout.String(), "showing 1 of 1")
				assert.Contains(t, stdout.String(), "bob@example.com")
				assert.NotContains(t, stdout.String(), "alice@example.com")
				assert.Contains(t, stdout.String(), "team data is incomplete")
			} else {
				assert.NotContains(t, stdout.String(), "ORGANIZATION")
				assert.NotContains(t, stdout.String(), "Investigations")
				assert.NotContains(t, stdout.String(), "bob@example.com")
			}
		})
	}
}

func TestUsageCommandRejectsInvalidInputBeforeRequests(t *testing.T) {
	for _, args := range [][]string{{"unexpected"}, {"--admin"}, {"--timeout", "1m"}, {"--top", "-1"}, {"-o", "invalid"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected HTTP request") }))
			t.Cleanup(server.Close)
			root := assistantcmd.Command()
			root.SilenceUsage = true
			root.SilenceErrors = true
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(append([]string{"usage", "get", "--config", writeAssistantTestConfig(t, server.URL)}, args...))
			require.Error(t, root.Execute())
		})
	}
}

func TestUsageCommandFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		admin  bool
		want   string
	}{
		{"forbidden", 403, `{"message":"forbidden"}`, false, "403"},
		{"missing endpoint", 404, `{}`, false, "404"},
		{"server error", 500, `{}`, false, "500"},
		{"invalid JSON", 200, `{`, false, "decode"},
		{"missing envelope", 200, `{}`, false, "missing"},
		{"null data", 200, `{"data":null}`, false, "missing"},
		{"missing limit", 200, `{"data":{"total":0,"month":"2026-11"}}`, false, "missing"},
		{"negative count", 200, `{"data":{"total":-1,"limit":10,"month":"2026-11"}}`, false, "negative"},
		{"invalid month", 200, `{"data":{"total":0,"limit":10,"month":"bad"}}`, false, "month"},
		{"admin unauthenticated", 401, `{}`, true, "401"},
		{"admin server failure", 500, `{}`, true, "500"},
		{"admin unavailable", 404, `{}`, true, "404"},
		{"admin missing members", 200, `{"data":{"stackUsedThisMonthTokens":0,"thisMonth":{"start":"2026-11-01T00:00:00Z","end":"2026-12-01T00:00:00Z"}}}`, true, "missing"},
		{"admin overflowing cap", 200, strings.Replace(adminUsageFixture, `"stackTotalLimitMtok":10`, `"stackTotalLimitMtok":9223372036854775807`, 1), true, "invalid token limit"},
		{"admin negative member", 200, strings.Replace(adminUsageFixture, `"usedThisMonthTokens":1000000`, `"usedThisMonthTokens":-1`, 1), true, "negative member"},
		{"admin missing counter", 200, `{"data":{"thisMonth":{"start":"2026-11-01T00:00:00Z","end":"2026-12-01T00:00:00Z"}}}`, true, "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				target := strings.HasSuffix(r.URL.Path, "/prompt")
				if tc.admin {
					target = strings.HasSuffix(r.URL.Path, "/teams")
				}
				if target {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
					return
				}
				fmt.Fprint(w, `{"data":{"total":0,"limit":0,"month":"2026-11","scope":"user"}}`)
			}))
			t.Cleanup(server.Close)
			root := assistantcmd.Command()
			root.SilenceUsage = true
			root.SilenceErrors = true
			var stdout bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&bytes.Buffer{})
			args := []string{"usage", "get", "--config", writeAssistantTestConfig(t, server.URL), "-o", "json"}
			root.SetArgs(args)
			require.ErrorContains(t, root.Execute(), tc.want)
			assert.Empty(t, stdout.String(), "must not emit a misleading partial snapshot")
		})
	}
}

func assertUsageJSON(t *testing.T, data []byte, wantAdmin bool, wantCount int) {
	t.Helper()
	var result assistant.UsageView
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, assistant.UsageMetric{Used: 30, Limit: 100, Month: "2026-11"}, result.YourUsage.Metrics["prompts"])
	assert.Equal(t, assistant.UsageMetric{Used: 42, Limit: 0, Month: "2026-11"}, result.YourUsage.Metrics["tokens"])
	assert.NotContains(t, result.YourUsage.Metrics, "investigations")
	var sections map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &sections))
	require.NotNil(t, result.OrgUsage, "shared counters must be visible to every caller")
	assert.Len(t, sections, 2)
	if !wantAdmin {
		assert.Equal(t, assistant.UsageMetric{Used: 25, Limit: 100, Month: "2026-11"}, result.OrgUsage.Metrics["investigations"])
		assert.Nil(t, result.OrgUsage.UsersNeedingAttention, "attention list is admin-only")
		return
	}
	assert.Equal(t, assistant.UsageMetric{Used: 25, Limit: 100, Month: "2026-11"}, result.OrgUsage.Metrics["investigations"])
	assert.Equal(t, assistant.UsageMetric{Used: 5000000, Limit: 10000000, Month: "2026-11"}, result.OrgUsage.Metrics["tokens"])
	assert.True(t, result.OrgUsage.TeamDataPartial)
	members := result.OrgUsage.UsersNeedingAttention
	require.NotNil(t, members, "no matches must encode as [] rather than null")
	require.Len(t, members, wantCount)
	if wantCount == 0 {
		return
	}
	assert.Equal(t, "b", members[0].ID)
	assert.EqualValues(t, 3000000, members[0].UsedThisMonthTokens)
	assert.EqualValues(t, 2, members[0].EffectiveLimitMtok)
	if wantCount == 2 {
		assert.Equal(t, "edge", members[1].ID)
	}
}

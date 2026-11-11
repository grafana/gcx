package assistant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	assistantcmd "github.com/grafana/gcx/internal/providers/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const apiPrefix = "/api/plugins/grafana-assistant-app/resources/api/v1/"

// Group IDs are UUIDs on the wire; these keep the fixtures readable.
const (
	groupSeats    = "00000000-0000-0000-0000-000000000001"
	groupInfra    = "00000000-0000-0000-0000-000000000002"
	groupPayments = "00000000-0000-0000-0000-000000000003"
)

// Shaped like GET /limit-request-groups in grafana-assistant-app#15977.
const groupsFixture = `{"status":"success","data":{"groups":[
{"id":"` + groupSeats + `","limitKey":"active_users","groupKey":"active_users","scope":{"kind":"stack"},"pendingCount":7,"firstRequestedAt":"2026-11-02T09:00:00Z","period":"2026-11"},
{"id":"` + groupInfra + `","limitKey":"user_tokens","groupKey":"user_tokens:team:t-infra","scope":{"kind":"team","teamUid":"t-infra"},"pendingCount":2,"firstRequestedAt":"2026-11-03T09:00:00Z","period":"2026-11"},
{"id":"` + groupPayments + `","limitKey":"user_tokens","groupKey":"user_tokens:team:t-pay","scope":{"kind":"team","teamUid":"t-pay"},"pendingCount":1,"firstRequestedAt":"2026-11-04T09:00:00Z","period":"2026-11"}],
"nextCursor":"","totalPending":10}}`

// The server lists a group's requests newest first.
const infraRequestsFixture = `{"status":"success","data":{"requests":[
{"id":"r2","limitKey":"user_tokens","groupId":"` + groupInfra + `","state":"pending","note":"","origin":"api","period":"2026-11","usedAtRequest":41000000,"limitAtRequest":40000000,"createdAt":"2026-11-05T10:00:00Z","resolvedAt":null,"requester":"carol@example.com"},
{"id":"r1","limitKey":"user_tokens","groupId":"` + groupInfra + `","state":"pending","note":"On call this week","origin":"api","period":"2026-11","usedAtRequest":40000000,"limitAtRequest":40000000,"createdAt":"2026-11-03T09:00:00Z","resolvedAt":null,"requester":"bob@example.com"}],
"totalCount":2}}`

const teamsSnapshot = `{"data":{"generatedAt":"2026-11-10T12:00:00Z","thisMonth":{"start":"2026-11-01T00:00:00Z","end":"2026-12-01T00:00:00Z"},"stackUsedThisMonthTokens":0,"members":[],
"teams":[{"uid":"t-infra","name":"infra","memberCount":4},{"uid":"t-pay","name":"payments","memberCount":2}]}}`

func runAssistant(t *testing.T, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	stdout, _, err := runAssistantIO(t, handler, args...)
	return stdout, err
}

func runAssistantIO(t *testing.T, handler http.HandlerFunc, args ...string) (string, string, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	root := assistantcmd.Command()
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetContext(context.Background())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append(append([]string{}, args...), "--config", writeAssistantTestConfig(t, server.URL)))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// usageServer serves PR #1790's counters plus the given limit-request paths.
// Absent paths answer 404, like a stack with limit requests turned off.
func usageServer(admin bool, tokens string, extra map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, apiPrefix)
		if body, ok := extra[path]; ok {
			fmt.Fprint(w, body)
			return
		}
		switch path {
		case "usage/limits/prompt":
			fmt.Fprint(w, `{"data":{"total":0,"limit":0,"month":"2026-11"}}`)
		case "usage/limits/tokens":
			fmt.Fprint(w, tokens)
		case "usage/limits/investigation":
			fmt.Fprint(w, `{"data":{"total":3,"limit":100,"month":"2026-11"}}`)
		case "usage/tokens/teams":
			if !admin {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fmt.Fprint(w, adminUsageFixture)
		case "usage/limits/monthly-unique-users", "usage/monthly-active-users":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}
}

const tokensFree = `{"data":{"total":0,"limit":40000000,"month":"2026-11","scope":"user"}}`

func TestUsageShowsHowToAskForMore(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tokens     string
		mine       string
		wantBlock  string
		wantAction string
		wantReach  []string
	}{
		{
			name:       "seat request pending is the only seat signal",
			tokens:     tokensFree,
			mine:       `{"data":{"requests":[{"id":"r1","limitKey":"active_users","state":"pending","period":"2026-11","limitAtRequest":10,"createdAt":"2026-11-09T08:00:00Z"}],"nextCursor":"","requestsEnabled":true}}`,
			wantBlock:  "✗ You can't use Assistant this month.\n  All 10 Assistant seats on this stack are taken. Seats open again on Dec 1 (UTC).",
			wantAction: "You asked your admins on Nov 9.",
			wantReach:  []string{"active_users"},
		},
		{
			name:       "own token limit reached and requests on",
			tokens:     `{"data":{"total":40000000,"limit":40000000,"month":"2026-11","scope":"user"}}`,
			mine:       `{"data":{"requests":[],"nextCursor":"","requestsEnabled":true}}`,
			wantBlock:  "✗ You've used all your Assistant tokens this month.\n  Your limit is 40M tokens.",
			wantAction: `gcx assistant limit-requests create user_tokens --note "<why>"`,
			wantReach:  []string{"user_tokens"},
		},
		{
			name:       "stack token limit reached is a stack_tokens request",
			tokens:     `{"data":{"total":500,"limit":500,"month":"2026-11","scope":"stack"}}`,
			mine:       `{"data":{"requests":[],"nextCursor":"","requestsEnabled":true}}`,
			wantBlock:  "✗ This stack has used all its Assistant tokens this month.",
			wantAction: "gcx assistant limit-requests create stack_tokens",
			wantReach:  []string{"stack_tokens"},
		},
		{
			name:       "admins turned requests off",
			tokens:     `{"data":{"total":40000000,"limit":40000000,"month":"2026-11","scope":"user"}}`,
			mine:       `{"data":{"requests":[],"nextCursor":"","requestsEnabled":false}}`,
			wantBlock:  "✗ You've used all your Assistant tokens this month.",
			wantAction: "Contact your Assistant Admins to raise it.",
			wantReach:  []string{"user_tokens"},
		},
		{
			name:      "not blocked",
			tokens:    tokensFree,
			mine:      `{"data":{"requests":[],"nextCursor":"","requestsEnabled":true}}`,
			wantReach: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := usageServer(false, tc.tokens, map[string]string{"limit-requests/mine": tc.mine})
			text, err := runAssistant(t, handler, "usage", "get")
			require.NoError(t, err)
			if tc.wantBlock == "" {
				assert.NotContains(t, text, "✗")
			} else {
				assert.Contains(t, text, tc.wantBlock)
				assert.Contains(t, text, tc.wantAction)
			}
			if !strings.Contains(tc.wantAction, "limit-requests create") {
				assert.NotContains(t, text, "limit-requests create")
			}

			out, err := runAssistant(t, handler, "usage", "get", "-o", "json")
			require.NoError(t, err)
			var view assistant.UsageView
			require.NoError(t, json.Unmarshal([]byte(out), &view))
			assert.ElementsMatch(t, tc.wantReach, view.YourUsage.LimitsReached)
			require.NotNil(t, view.YourUsage.LimitRequestsEnabled)
		})
	}
}

func TestUsageWithoutLimitRequestBackend(t *testing.T) {
	handler := usageServer(true, `{"data":{"total":40000000,"limit":40000000,"month":"2026-11","scope":"user"}}`, nil)
	text, err := runAssistant(t, handler, "usage", "get")
	require.NoError(t, err)
	assert.Contains(t, text, "Contact your Assistant Admins to raise it.")
	assert.NotContains(t, text, "PENDING LIMIT REQUESTS")

	out, err := runAssistant(t, handler, "usage", "get", "-o", "json")
	require.NoError(t, err)
	assert.NotContains(t, out, "limitRequestsEnabled")
	assert.NotContains(t, out, "pendingLimitRequests")
}

func TestUsageShowsSeatsAndPendingGroupsToAdmins(t *testing.T) {
	extra := map[string]string{
		"limit-requests/mine":               `{"data":{"requests":[],"nextCursor":"","requestsEnabled":true}}`,
		"limit-request-groups":              groupsFixture,
		"usage/limits/monthly-unique-users": `{"data":{"limit":10}}`,
		"usage/monthly-active-users":        `{"data":{"count":10,"month":"2026-11"}}`,
		"usage/tokens/teams":                strings.Replace(adminUsageFixture, `"teams":[]`, `"teams":[{"uid":"t-infra","name":"infra","memberCount":4}]`, 1),
	}
	text, err := runAssistant(t, usageServer(true, tokensFree, extra), "usage", "get")
	require.NoError(t, err)
	assert.Regexp(t, `Seats +10 / 10`, text)
	_, pending, found := strings.Cut(text, "PENDING LIMIT REQUESTS\n10 pending requests in 3 groups\n")
	require.True(t, found, text)
	// Named from the usage snapshot when known, by UID otherwise.
	assert.Regexp(t, `(?s)^LIMIT +SCOPE +REQUESTS\s*\nseats +stack +7\s*\ntokens per user +team: infra +2\s*\ntokens per user +team: t-pay +1`, pending)

	out, err := runAssistant(t, usageServer(true, tokensFree, extra), "usage", "get", "-o", "json")
	require.NoError(t, err)
	var view assistant.UsageView
	require.NoError(t, json.Unmarshal([]byte(out), &view))
	require.NotNil(t, view.OrgUsage)
	assert.Equal(t, assistant.UsageMetric{Used: 10, Limit: 10, Month: "2026-11"}, view.OrgUsage.Metrics["seats"])
	require.Len(t, view.OrgUsage.PendingLimitRequests, 3)
	assert.Equal(t, "infra", view.OrgUsage.PendingLimitRequests[1].TeamName)

	// Any failure other than "off" or "forbidden" fails the snapshot.
	failing := usageServer(true, tokensFree, extra)
	_, err = runAssistant(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/limit-request-groups") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		failing(w, r)
	}, "usage", "get", "-o", "json")
	require.ErrorContains(t, err, "500")
}

func TestLimitRequestsCreate(t *testing.T) {
	var got map[string]string
	created := true
	handler := func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, apiPrefix+"limit-requests", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		got = nil
		assert.NoError(t, json.Unmarshal(body, &got))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"status":"success","data":{"request":{"id":"r9","limitKey":"active_users","state":"pending","period":"2026-11","createdAt":"2026-11-08T12:00:00Z"},"created":%t}}`, created)
	}
	out, err := runAssistant(t, handler, "limit-requests", "create", "active_users", "--note", "Debugging the checkout latency alert")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"limitKey": "active_users", "note": "Debugging the checkout latency alert"}, got)
	assert.Contains(t, out, "Request sent to your Assistant Admins.\n  You'll get a Grafana notification when they respond.")

	created = false
	out, err = runAssistant(t, handler, "limit-requests", "create", "active_users")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"limitKey": "active_users"}, got, "an omitted note is not sent")
	assert.Contains(t, out, "You already asked on Nov 8")

	out, err = runAssistant(t, handler, "limit-requests", "create", "active_users", "-o", "json")
	require.NoError(t, err)
	var result assistant.CreatedLimitRequest
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "r9", result.Request.ID)
	assert.False(t, result.Created)
}

func TestLimitRequestsCreateRefusals(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusConflict, `{"name":"Conflict","message":"You're not blocked by this limit.","details":{"reason":"not_blocked"}}`, "You're not blocked by this limit."},
		{http.StatusConflict, `{"name":"Conflict","message":"Limit requests are turned off for this stack. Contact your Assistant Admins."}`, "turned off for this stack"},
		{http.StatusNotFound, `{"name":"NotFound","message":"Limit requests not found"}`, "does not accept limit requests"},
	} {
		_, err := runAssistant(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			fmt.Fprint(w, tc.body)
		}, "limit-requests", "create", "user_tokens")
		require.ErrorContains(t, err, tc.want)
	}
}

func TestLimitRequestsRejectInvalidInputBeforeRequests(t *testing.T) {
	for _, args := range [][]string{
		{"create"},
		{"create", "investigations"},
		{"create", "users"},
		{"create", "active_users", "user_tokens"},
		{"create", "active_users", "--note", strings.Repeat("x", 281)},
		{"list", "--type", "seats"},
		{"list", "unexpected"},
	} {
		t.Run(strings.Join(args, " ")[:min(40, len(strings.Join(args, " ")))], func(t *testing.T) {
			_, err := runAssistant(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request") }, append([]string{"limit-requests"}, args...)...)
			require.Error(t, err)
		})
	}
}

func listServer(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, apiPrefix) {
		case "limit-request-groups":
			assert.Equal(t, r.URL.Query().Get("limitKey") != "", strings.Contains(r.URL.RawQuery, "limitKey="))
			fmt.Fprint(w, groupsFixture)
		case "limit-request-groups/" + groupInfra + "/requests":
			fmt.Fprint(w, infraRequestsFixture)
		case "usage/tokens/teams":
			fmt.Fprint(w, teamsSnapshot)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func TestLimitRequestsList(t *testing.T) {
	out, err := runAssistant(t, listServer(t), "limit-requests", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "10 pending requests in 3 groups")
	assert.Regexp(t, `(?s)seats +stack +7.*tokens per user +team: infra +2.*tokens per user +team: payments +1`, out)
	assert.Contains(t, out, "See each request: gcx assistant limit-requests list --type active_users")
	assert.NotContains(t, out, "bob@example.com", "the summary does not list requesters")

	out, err = runAssistant(t, listServer(t), "limit-requests", "list", "--type", "user_tokens", "--team", "INFRA")
	require.NoError(t, err)
	assert.Regexp(t, `(?s)bob@example.com .*On call this week.*carol@example.com`, out, "oldest request first")

	out, err = runAssistant(t, listServer(t), "limit-requests", "list", "--team", "t-infra", "-o", "json")
	require.NoError(t, err)
	var groups []struct {
		assistant.LimitRequestGroup

		Requests []assistant.LimitRequest `json:"requests"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &groups))
	require.Len(t, groups, 1)
	assert.Equal(t, "infra", groups[0].TeamName)
	require.Len(t, groups[0].Requests, 2)
	assert.Equal(t, "r1", groups[0].Requests[0].ID)

	_, err = runAssistant(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, "limit-requests", "list")
	forbidden, ok := errors.AsType[*assistanthttp.ForbiddenError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, "grafana-assistant-app.usage:read", forbidden.RequiredPermission(), "the error names the missing permission")
}

const groupDefault = "00000000-0000-0000-0000-000000000004"

// raiseGroups are pending groups keyed by limitKey, like the server's
// filtered GET /limit-request-groups.
var raiseGroups = map[string][]string{ //nolint:gochecknoglobals // read-only test fixture
	"active_users": {`{"id":"` + groupSeats + `","limitKey":"active_users","groupKey":"active_users","scope":{"kind":"stack"},"pendingCount":7,"firstRequestedAt":"2026-11-02T09:00:00Z","period":"2026-11"}`},
	"user_tokens": {
		`{"id":"` + groupInfra + `","limitKey":"user_tokens","groupKey":"user_tokens:team:t-infra","scope":{"kind":"team","teamUid":"t-infra"},"pendingCount":2,"firstRequestedAt":"2026-11-03T09:00:00Z","period":"2026-11"}`,
		`{"id":"` + groupDefault + `","limitKey":"user_tokens","groupKey":"user_tokens:default","scope":{"kind":"default_per_user"},"pendingCount":3,"firstRequestedAt":"2026-11-04T09:00:00Z","period":"2026-11"}`,
	},
}

// limitWriteServer serves the limit-request groups and the existing limit
// endpoints, and records every write as "METHOD path body".
func limitWriteServer(t *testing.T, writes *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, apiPrefix)
		if r.Method != http.MethodGet {
			body, _ := io.ReadAll(r.Body)
			*writes = append(*writes, r.Method+" "+path+" "+strings.TrimSpace(string(body)))
		}
		switch {
		case path == "limit-request-groups":
			fmt.Fprintf(w, `{"data":{"groups":[%s],"nextCursor":""}}`, strings.Join(raiseGroups[r.URL.Query().Get("limitKey")], ","))
		case path == "usage/tokens/teams":
			fmt.Fprint(w, teamsSnapshot)
		case strings.HasSuffix(path, "/raise-preview"):
			fmt.Fprintf(w, `{"data":{"proposedValue":%s,"resolves":2}}`, r.URL.Query().Get("value"))
		case path == "usage/limits/monthly-unique-users":
			fmt.Fprint(w, `{"data":{"limit":10}}`)
		case path == "usage/tokens/limits" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"data":{"stackTotalMtok":500,"perUserDefaultMtok":40,"serviceAccountPoolMtok":25,"teams":[{"teamUid":"t-infra","perUserMtok":50}]}}`)
		case r.Method != http.MethodGet:
			fmt.Fprint(w, `{"data":{}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	}
}

func TestLimitRequestsRaise(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantWrite string
		wantOut   string
	}{
		{
			name: "default per-user limit keeps the other overrides",
			args: []string{"user_tokens", "--to", "60M"},
			// PUT replaces every override, so the stack total and pool must survive.
			wantWrite: `PUT usage/tokens/limits {"stackTotalMtok":500,"perUserDefaultMtok":60,"serviceAccountPoolMtok":25}`,
			wantOut:   "Raised tokens per user (default per user) from 40M to 60M tokens.\n  That resolves 2 of 3 pending requests",
		},
		{
			name:      "team grant goes to the team route",
			args:      []string{"user_tokens", "--team", "infra", "--to", "80M"},
			wantWrite: `PUT usage/tokens/limits/teams/t-infra {"perUserMtok":80}`,
			wantOut:   "from 50M to 80M tokens",
		},
		{
			name:      "seats",
			args:      []string{"active_users", "--to", "15"},
			wantWrite: `PUT usage/limits/monthly-unique-users {"limit":15}`,
			wantOut:   "Raised seats (stack) from 10 to 15 seats.",
		},
		{
			name:    "dry run writes nothing",
			args:    []string{"active_users", "--to", "15", "--dry-run"},
			wantOut: "Would raise seats (stack) from 10 to 15 seats.\n  That resolves 2 of 7 pending requests. Run again without --dry-run",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes []string
			out, err := runAssistant(t, limitWriteServer(t, &writes), append([]string{"limit-requests", "raise"}, tc.args...)...)
			require.NoError(t, err)
			assert.Contains(t, out, tc.wantOut)
			if tc.wantWrite == "" {
				assert.Empty(t, writes)
			} else {
				assert.Equal(t, []string{tc.wantWrite}, writes)
			}
		})
	}
}

func TestLimitRequestsRaiseRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"active_users", "--to", "10"}, "already 10"},
		{[]string{"user_tokens", "--to", "30M"}, "already 40M"},
		{[]string{"user_tokens", "--to", "1500000"}, "whole millions"},
		{[]string{"stack_tokens", "--to", "900M"}, "no pending requests for stack tokens"},
		{[]string{"user_tokens", "--team", "payments", "--to", "90M"}, "no pending requests for tokens per user for team payments"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var writes []string
			_, err := runAssistant(t, limitWriteServer(t, &writes), append([]string{"limit-requests", "raise"}, tc.args...)...)
			require.ErrorContains(t, err, tc.want)
			assert.Empty(t, writes, "a refused raise writes nothing")
		})
	}
}

func TestLimitRequestsDismiss(t *testing.T) {
	var writes []string
	out, err := runAssistant(t, limitWriteServer(t, &writes), "limit-requests", "dismiss", "r1", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "Dismissed 1 request.")

	_, err = runAssistant(t, limitWriteServer(t, &writes), "limit-requests", "dismiss", "--type", "user_tokens", "--team", "infra", "--force", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"POST limit-requests/r1/dismiss ", "POST limit-request-groups/" + groupInfra + "/dismiss "}, writes)

	writes = nil
	_, err = runAssistant(t, limitWriteServer(t, &writes), "limit-requests", "dismiss", "r1")
	require.Error(t, err, "without --force a closed stdin declines")
	assert.Empty(t, writes)
}

func TestLimitRequestsSettingsUpdateKeepsUnsetFields(t *testing.T) {
	var put string
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, apiPrefix+"usage/limit-requests/settings", r.URL.Path)
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			put = string(body)
			fmt.Fprintf(w, `{"data":{"settings":%s}}`, body)
			return
		}
		fmt.Fprint(w, `{"data":{"settings":{"enabled":false,"adminChannels":{"inbox":true,"emailDigest":false},"alert":{"enabled":true,"thresholdPercent":80,"email":true}}}}`)
	}
	out, err := runAssistant(t, handler, "limit-requests", "settings", "update", "--enabled", "--alert-threshold", "90")
	require.NoError(t, err)
	assert.JSONEq(t, `{"enabled":true,"adminChannels":{"inbox":true,"emailDigest":false},"alert":{"enabled":true,"thresholdPercent":90,"email":true}}`, put)
	assert.Regexp(t, `Limit requests +on`, out)
	assert.Regexp(t, `Admin email digest +off`, out)

	for _, args := range [][]string{{}, {"--alert-threshold", "100"}} {
		_, err := runAssistant(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request") }, append([]string{"limit-requests", "settings", "update"}, args...)...)
		require.Error(t, err)
	}
}

func TestLimitRequestsListMinePages(t *testing.T) {
	var queries []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, apiPrefix+"limit-requests/mine", r.URL.Path)
		queries = append(queries, r.URL.Query().Get("state")+"|"+r.URL.Query().Get("cursor"))
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"data":{"requests":[{"id":"r3","limitKey":"user_tokens","state":"resolved","resolution":"limit_raised","period":"2026-11","createdAt":"2026-11-08T08:00:00Z","resolvedAt":"2026-11-09T10:30:00Z","note":"Release week"}],"nextCursor":"c2","requestsEnabled":true}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"requests":[{"id":"r1","limitKey":"active_users","state":"dismissed","resolution":"dismissed","period":"2026-10","createdAt":"2026-10-02T08:00:00Z","resolvedAt":"2026-10-03T08:00:00Z"}],"nextCursor":"","requestsEnabled":true}}`)
	}
	out, err := runAssistant(t, handler, "limit-requests", "list", "--mine")
	require.NoError(t, err)
	assert.Equal(t, []string{"|", "|c2"}, queries)
	assert.Regexp(t, `(?s)tokens per user +resolved \(limit raised\) +Nov 8 08:00 +Nov 9 10:30 +Release week.*seats +dismissed +Oct 2`, out)

	queries = nil
	_, err = runAssistant(t, handler, "limit-requests", "list", "--mine", "--state", "resolved", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, "resolved|", queries[0])

	for _, args := range [][]string{{"--state", "pending"}, {"--mine", "--type", "user_tokens"}, {"--mine", "--state", "open"}} {
		_, err := runAssistant(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected HTTP request") }, append([]string{"limit-requests", "list"}, args...)...)
		require.Error(t, err)
	}
}

func TestLimitRequestsCreateAsServiceAccount(t *testing.T) {
	_, err := runAssistant(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"name":"Forbidden","message":"Limit requests are made by the blocked person"}`)
	}, "limit-requests", "create", "user_tokens")
	require.ErrorContains(t, err, "Only the blocked person can ask to raise a limit")
	require.ErrorContains(t, err, "Limit requests are made by the blocked person")
}

func TestPromptRefusedByLimitExplainsHowToAsk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	refusal := `{"kind":"status-update","taskId":"task-1","contextId":"ctx-1","final":true,"status":{"state":"failed","message":{"kind":"message","role":"assistant","messageId":"m1","parts":[{"kind":"text","text":"Monthly Assistant token usage limit reached. The request was blocked."}],"metadata":{"errorKind":"ga:resource-limit-exceeded"}}}}`
	usage := usageServer(false, `{"data":{"total":40000000,"limit":40000000,"month":"2026-11","scope":"user"}}`, map[string]string{
		"limit-requests/mine": `{"data":{"requests":[],"nextCursor":"","requestsEnabled":true}}`,
	})
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/a2a/agents/") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"jsonrpc":"2.0","result":`+refusal+"}\n\n")
			return
		}
		usage(w, r)
	}
	_, stderr, err := runAssistantIO(t, handler, "prompt", "hello")
	require.Error(t, err)
	assert.Contains(t, stderr, "Monthly Assistant token usage limit reached")
	assert.Contains(t, stderr, "✗ You've used all your Assistant tokens this month.")
	assert.Contains(t, stderr, `gcx assistant limit-requests create user_tokens --note "<why>"`)

	// Another failure gets no limit block.
	other := strings.ReplaceAll(refusal, "ga:resource-limit-exceeded", "ga:internal-error")
	_, stderr, err = runAssistantIO(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/a2a/agents/") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"jsonrpc":"2.0","result":`+other+"}\n\n")
			return
		}
		t.Errorf("unexpected request %s", r.URL.Path)
	}, "prompt", "hello")
	require.Error(t, err)
	assert.NotContains(t, stderr, "limit-requests create")
}

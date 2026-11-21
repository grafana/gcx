package errortracking_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorsListEvents(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors/42/events"
	f.handle(route, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"events":[{"id":1,"eventId":"a1"},{"id":2,"eventId":"a2"}],"nextCursor":"c1"}`))
		case "c1":
			_, _ = w.Write([]byte(`{"events":[{"id":3,"eventId":"a3"}]}`))
		}
	})

	list := func(args ...string) []string { return append([]string{"errors", "events", "list"}, args...) }
	cases := []cliCase{
		{name: "a list cut off at --limit says so", args: list("42", "--project", "314", "--limit", "2", "-o", "json"),
			calls: []string{route}, check: func(t *testing.T, out string, _ error, calls []etCall) {
				t.Helper()
				var list struct {
					Items []struct {
						EventID string `json:"eventId"`
					} `json:"items"`
					ListMeta *struct {
						Truncated bool `json:"truncated"`
					} `json:"list_meta"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &list))
				assert.Len(t, list.Items, 2)
				require.NotNil(t, list.ListMeta)
				assert.True(t, list.ListMeta.Truncated)
				assert.Equal(t, "2", calls[0].Query.Get("limit"))
				assert.Empty(t, calls[0].Query.Get("from"))
			}},
		{name: "--limit 0 follows every page", args: list("42", "--project", "314", "--limit", "0", "--since", "1h", "--json", "eventId"),
			calls: []string{route, route}, json: `{"items":[{"eventId":"a1"},{"eventId":"a2"},{"eventId":"a3"}]}`,
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.Equal(t, "200", calls[0].Query.Get("limit"))
				assert.Equal(t, "c1", calls[1].Query.Get("cursor"))
				assert.NotEmpty(t, calls[1].Query.Get("from"))
			}},
		{name: "a release is a custom field", args: list("42", "--project", "314", "--custom-field", "release:v1.2.3", "--limit", "1", "-o", "json"),
			calls: []string{route}, query: map[string][]string{"customField": {"release:v1.2.3"}, "limit": {"1"}}},
		{name: "no error ID", args: list("--project", "314"), exit: gcxerrors.ExitUsageError, err: "accepts 1 arg(s), received 0"},
		{name: "a custom field without a value", args: list("42", "--project", "314", "--custom-field", "region"),
			exit: gcxerrors.ExitUsageError, err: `--custom-field "region": use key:value, !key:value, has:key or key:[a,b]`},
	}
	f.runCases(t, cases, usageCases(list, []string{"0", "--project", "314"}, []string{"x", "--project", "314"}))
}

func TestEventsGet(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors/42/events/0f6c2b1e"
	f.reply(route, 200, `{"id":7,"eventId":"0f6c2b1e","release":"v1.2","occurredAt":"2026-11-16T10:00:00.000Z","payload":{"level":"error"}}`)

	get := func(id string, args ...string) []string {
		return append([]string{"errors", "events", "get", "42", id, "--project", "314"}, args...)
	}
	cases := []cliCase{
		{name: "selected fields", args: get("0f6c2b1e", "--json", "eventId,release"), calls: []string{route}, json: `{"eventId":"0f6c2b1e","release":"v1.2"}`},
		{name: "table", args: get("0f6c2b1e", "-o", "table"), calls: []string{route}, has: []string{"v1.2"}},
		{name: "the numeric id is explained, not looked up", args: get("201869"), exit: gcxerrors.ExitUsageError,
			err: `<event> "201869" looks like the numeric id; events are addressed by EVENT ID (32 hex characters), not the numeric id`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See them: gcx error-tracking errors events list 42 --project 314")
			}},
		{name: "no event says what it takes", args: []string{"errors", "events", "get", "42", "--project", "314"}, exit: gcxerrors.ExitUsageError,
			err: "accepts 2 arg(s), received 1"},
		{name: "a non-numeric error gets the positional's message", args: []string{"errors", "events", "get", "abc", "0f6c2b1e", "--project", "314"},
			exit: gcxerrors.ExitUsageError, err: `<error> "abc" must be a positive error ID`, hasNot: []string{"strconv"}},
	}
	byID := func(args ...string) []string { return get(args[0], args[1:]...) }
	f.runCases(t, cases, usageCases(byID, []string{" "}, []string{"."}, []string{".."}))
}

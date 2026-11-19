package errortracking_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorsUpdate(t *testing.T) {
	f := newFakeET(t)
	const get, route = "GET projects/314/errors/42", "PATCH projects/314/errors/42"
	f.reply(get, 200, errorFixture)
	f.reply(route, 200, `{"id":42,"status":"ignored","history":[]}`)
	f.reply("GET projects/314/errors/43", 200, `{"id":43,"projectName":"Checkout API","status":"resolved","exceptionType":"Error","message":"gateway timeout","history":[]}`)
	f.reply("GET projects/314/errors/44", 200, `{"id":44,"projectName":"Checkout API","status":"unresolved","exceptionType":"Error","message":"x","history":[]}`)
	f.reply("PATCH projects/314/errors/44", http.StatusConflict, `{"error":{"code":"invalid_transition","message":"cannot move an error from unresolved to resolved","currentStatus":"unresolved"}}`)
	resolved := func(f *fakeET) { f.reply(route, 200, `{"id":42,"status":"resolved","history":[]}`) }

	update := func(args ...string) []string {
		return append([]string{"errors", "update", "42", "--project", "314"}, args...)
	}
	cases := []cliCase{
		{name: "a batch result carries the project and no row for a plain success", args: update("--status", "ignored", "--more-events", "10", "-o", "json"),
			calls: []string{get, route}, body: `{"status":"ignored","moreEvents":10}`,
			json: `{"type":"gcx.mutation_batch","schema_version":"1","action":"updated","summary":{"succeeded":1,"failed":0},
				"failures":[],"project":{"id":314,"name":"Checkout API"},"results":[]}`},
		{name: "ignore for a while or for more events", args: update("--status", "ignored", "--until", "7d", "--more-events", "100", "-o", "json"),
			calls: []string{get, route}, check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				var body struct {
					Status         string `json:"status"`
					IgnoredUntilAt string `json:"ignoredUntilAt"`
					MoreEvents     int64  `json:"moreEvents"`
				}
				require.NoError(t, json.Unmarshal([]byte(calls[1].Body), &body))
				assert.Equal(t, "ignored", body.Status)
				assert.Equal(t, int64(100), body.MoreEvents)
				until, err := time.Parse(time.RFC3339, body.IgnoredUntilAt)
				require.NoError(t, err)
				assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), until, time.Minute)
			}},
		{name: "a time goes out in UTC", args: update("--status", "ignored", "--until", "2099-01-01T00:00:00+01:00", "-o", "json"),
			calls: []string{get, route}, body: `{"status":"ignored","ignoredUntilAt":"2098-12-31T23:00:00Z"}`},
		{name: "text says what changed and where, with no next-step hint", args: update("--status", "ignored", "--more-events", "10"),
			calls: []string{get, route}, has: []string{"✔ Ignored 1 of 1 errors in project Checkout API (ID 314)"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "hint:")
			}},
		{name: "re-ignoring an ignored error sends the new condition", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectName":"Checkout API","status":"ignored","ignoredUntilAt":"2026-11-26T07:06:00Z","eventCount":3,"history":[]}`)
		}, args: update("--status", "ignored", "--more-events", "5"), calls: []string{get, route}, body: `{"status":"ignored","moreEvents":5}`,
			has: []string{"✔ Ignored 1 of 1 errors in project Checkout API (ID 314)"}},
		{name: "leaving ignored resolves it", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectName":"Checkout API","status":"ignored","ignoredUntilAt":"2026-11-26T07:06:00Z","history":[]}`)
			f.reply(route, 200, `{"id":42,"status":"resolved","history":[]}`)
		}, args: update("--status", "resolved"), calls: []string{get, route}, body: `{"status":"resolved"}`,
			has: []string{"✔ Resolved 1 of 1 errors in project Checkout API (ID 314)"}},
		{name: "re-ignoring an ignore without conditions sends the new one", setup: func(f *fakeET) {
			f.reply(get, 200, `{"id":42,"projectName":"Checkout API","status":"ignored","history":[]}`)
		}, args: update("--status", "ignored", "--until", "2099-01-01T00:00:00Z"), calls: []string{get, route},
			body: `{"status":"ignored","ignoredUntilAt":"2099-01-01T00:00:00Z"}`, has: []string{"✔ Ignored 1 of 1 errors in project Checkout API (ID 314)"}},
		{name: "an ignore without conditions sends none", args: update("--status", "ignored"),
			calls: []string{get, route}, body: `{"status":"ignored"}`, has: []string{"✔ Ignored 1 of 1 errors"}},
		{name: "a rate and a users condition", args: update("--status", "ignored", "--rate-events", "50", "--rate-window", "1h", "--more-users", "20", "-o", "json"),
			calls: []string{get, route}, body: `{"status":"ignored","rateEvents":50,"rateWindowMinutes":60,"moreUsers":20}`},
		{name: "a rate window goes out in minutes", args: update("--status", "ignored", "--rate-events", "5", "--rate-window", "90m"),
			calls: []string{get, route}, body: `{"status":"ignored","rateEvents":5,"rateWindowMinutes":90}`},
		{name: "a day-long rate window", args: update("--status", "ignored", "--rate-events", "5", "--rate-window", "1d"),
			calls: []string{get, route}, body: `{"status":"ignored","rateEvents":5,"rateWindowMinutes":1440}`},
		{name: "the widest rate window", args: update("--status", "ignored", "--rate-events", "1000000000", "--rate-window", "7d", "-o", "json"),
			calls: []string{get, route}, body: `{"status":"ignored","rateEvents":1000000000,"rateWindowMinutes":10080}`},
		{name: "resolving", setup: resolved, args: update("--status", "resolved"),
			calls: []string{get, route}, has: []string{"✔ Resolved 1 of 1 errors in project Checkout API (ID 314)"}},
		{name: "the same status again is a success with changed: false and no write", args: []string{"errors", "update", "43", "--project", "314", "--status", "resolved", "-o", "json"},
			calls: []string{"GET projects/314/errors/43"},
			json: `{"type":"gcx.mutation_batch","schema_version":"1","action":"updated","summary":{"succeeded":0,"failed":0,"skipped":1},
				"skipped_reasons":{"already resolved":1},"failures":[],"project":{"id":314,"name":"Checkout API"},
				"results":[{"id":43,"previousStatus":"resolved","status":"resolved","changed":false}]}`},
		{name: "the same status as text", args: []string{"errors", "update", "43", "--project", "314", "--status", "resolved"},
			calls: []string{"GET projects/314/errors/43"}, has: []string{"✔ Resolved 0 of 1 errors in project Checkout API (ID 314); 1 was already resolved", "resolved (no change)"}},
		{name: "a 409 for the status it already has is changed: false", setup: func(f *fakeET) {
			f.reply(route, http.StatusConflict, `{"error":{"code":"invalid_transition","message":"cannot move an error from resolved to resolved","currentStatus":"resolved"}}`)
		}, args: update("--status", "resolved", "-o", "json"), calls: []string{get, route}, has: []string{`"changed": false`, `"status": "resolved"`}},
		{name: "another refused move says how to see the status", exit: gcxerrors.ExitGeneralError, args: []string{"errors", "update", "44", "--project", "314", "--status", "resolved"},
			calls: []string{"GET projects/314/errors/44", "PATCH projects/314/errors/44"}, err: "cannot move an error from unresolved to resolved (current status: unresolved)",
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See its status: gcx error-tracking errors get 44 --project 314")
				assert.Contains(t, rendered(t, err), "Resource conflict")
			}},
		{name: "several IDs give a batch with a row each", setup: resolved, args: []string{"errors", "update", "42", "43", "--project", "314", "--status", "resolved"},
			calls: []string{get, "GET projects/314/errors/43", route}, anyOrd: true,
			has: []string{"✔ Resolved 1 of 2 errors in project Checkout API (ID 314); 1 was already resolved", "WAS", "NOW", "resolved (no change)", "gateway timeout"}},
		{name: "a failed ID in a batch exits 4 and is listed once, with no row for the successes", setup: resolved, args: []string{"errors", "update", "42", "44", "42", "--project", "314", "--status", "resolved", "-o", "json"},
			calls: []string{get, "GET projects/314/errors/44", route, "PATCH projects/314/errors/44"}, anyOrd: true, exit: gcxerrors.ExitPartialFailure,
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.JSONEq(t, `{"type":"gcx.mutation_batch","schema_version":"1","action":"updated",
					"summary":{"succeeded":1,"failed":1},
					"failures":[{"target":{"kind":"Error","id":"44","namespace":"stacks-123"},"error":"cannot move an error from unresolved to resolved (current status: unresolved)"}],
					"project":{"id":314,"name":"Checkout API"},
					"results":[]}`, out)
			}},
		{name: "a failure in text is flagged in the summary and exits 4", setup: resolved, args: []string{"errors", "update", "42", "44", "--project", "314", "--status", "resolved"},
			calls: []string{get, "GET projects/314/errors/44", route, "PATCH projects/314/errors/44"}, anyOrd: true, exit: gcxerrors.ExitPartialFailure,
			has:    []string{"⚠ Resolved 1 of 2 errors in project Checkout API (ID 314); 1 failed", "44: cannot move an error from unresolved to resolved (current status: unresolved)"},
			hasNot: []string{"✔"}},
		{name: "every ID failing exits with the shared code and lists each failure once", setup: func(f *fakeET) {
			f.reply(get, 404, `{"error":{"code":"not_found","message":"not found"}}`)
			f.reply("GET projects/314/errors/43", 404, `{"error":{"code":"not_found","message":"not found"}}`)
		}, args: []string{"errors", "update", "42", "43", "--project", "314", "--status", "resolved", "-o", "json"},
			calls: []string{get, "GET projects/314/errors/43"}, anyOrd: true, exit: gcxerrors.ExitGeneralError,
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.JSONEq(t, `{"type":"gcx.mutation_batch","schema_version":"1","action":"updated","summary":{"succeeded":0,"failed":2},
					"failures":[{"target":{"kind":"Error","id":"42","namespace":"stacks-123"},"error":"Error Tracking has no error 42 in project 314"},
						{"target":{"kind":"Error","id":"43","namespace":"stacks-123"},"error":"Error Tracking has no error 43 in project 314"}],
					"project":{"id":314},
					"results":[]}`, out)
			}},
		{name: "every ID failing in text gets the failure mark and exits 1", setup: func(f *fakeET) {
			f.reply(get, 404, `{"error":{"code":"not_found","message":"not found"}}`)
			f.reply("GET projects/314/errors/43", 404, `{"error":{"code":"not_found","message":"not found"}}`)
		}, args: []string{"errors", "update", "42", "43", "--project", "314", "--status", "resolved"},
			calls: []string{get, "GET projects/314/errors/43"}, anyOrd: true, exit: gcxerrors.ExitGeneralError,
			has: []string{"✘ Resolved 0 of 2 errors in project 314; 2 failed"}, hasNot: []string{"✔", "⚠"}},
		{name: "a bad error ID", exit: gcxerrors.ExitUsageError, args: []string{"errors", "update", "x", "--project", "314", "--status", "resolved"}, err: `<error> "x" must be a positive error ID`},
		{name: "a bad second error ID", exit: gcxerrors.ExitUsageError, args: []string{"errors", "update", "42", "0", "--project", "314", "--status", "resolved"}, err: `<error> "0" must be a positive error ID`},
		{name: "no error ID", args: []string{"errors", "update", "--project", "314", "--status", "resolved"}, exit: gcxerrors.ExitUsageError, err: "requires at least 1 arg(s), only received 0"},
		{name: "a bad value still gets gcx's own flag error", args: update("--status", "resolved", "--rate-events", "x"), exit: gcxerrors.ExitUsageError, err: `invalid argument "x" for "--rate-events" flag`},
	}
	bad := [][]string{
		{},
		{"--status", ""},
		{"--status", "regressed"},
		{"--status", "ignored", "--rate-events", "5"},
		{"--status", "ignored", "--rate-window", "1h"},
		{"--status", "ignored", "--rate-events", "0", "--rate-window", "1h"},
		{"--status", "ignored", "--rate-events", "1000000001", "--rate-window", "1h"},
		{"--status", "ignored", "--rate-events", "5", "--rate-window", "10m"},
		{"--status", "ignored", "--rate-events", "5", "--rate-window", "17m"},
		{"--status", "ignored", "--rate-events", "5", "--rate-window", "7d5m"},
		{"--status", "ignored", "--rate-events", "5", "--rate-window", ""},
		{"--status", "ignored", "--more-users", "0"},
		{"--status", "ignored", "--more-users", "1000001"},
		{"--status", "ignored", "--more-events", "1000000001"},
		{"--status", "resolved", "--rate-events", "5", "--rate-window", "1h"},
		{"--status", "unresolved", "--more-users", "3"},
		{"--status", "resolved", "--until", "7d"},
		{"--status", "unresolved", "--more-events", "3"},
		{"--status", "ignored", "--more-events", "0"},
		{"--status", "ignored", "--until", "now-1h"},
		{"--status", "ignored", "--until", "soon"},
		{"--status", "resolved", "--until", ""},
		{"--status", "ignored", "--until", " ", "--more-events", "5"},
	}
	f.runCases(t, cases, usageCases(update, bad...))
}

func TestErrorsTimeline(t *testing.T) {
	f := newFakeET(t)
	tl := `{"bucket":"1h","buckets":[{"bucket":"2026-11-16T09:00:00.000Z","eventCount":0},{"bucket":"2026-11-16T10:00:00.000Z","eventCount":4},{"bucket":"2026-11-16T11:00:00.000Z","eventCount":1},{"bucket":"2026-11-16T12:00:00.000Z","eventCount":0},{"bucket":"2026-11-16T13:00:00.000Z","eventCount":0}],"affectedUsersCoverage":"full","affectedUsersInRange":1,"identifiedEventsInRange":3}`
	const route = "GET projects/314/errors/42/timeline"
	f.reply(route, 200, tl)

	timeline := func(args ...string) []string {
		return append([]string{"errors", "timeline", "42", "--project", "314"}, args...)
	}
	f.runCases(t, []cliCase{
		{name: "the API needs a window, so it defaults to the last day; JSON keeps every bucket", args: timeline("-o", "json"),
			calls: []string{route}, json: tl, check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assertWindow(t, calls[0], 24*time.Hour)
				assert.NotContains(t, f.stderr.String(), "empty bucket")
			}},
		{name: "a table shows every bucket and draws bars", args: timeline("--from", "2026-11-01T00:00:00Z", "--to", "2026-11-02T00:00:00Z", "-o", "table"),
			calls: []string{route}, has: []string{"BUCKET", "2026-11-16 09:00Z  0", "2026-11-16 10:00Z  4", "████████████████████", "2026-11-16 11:00Z  1", "2026-11-16 13:00Z  0"},
			query: map[string][]string{"from": {"2026-11-01T00:00:00Z"}, "to": {"2026-11-02T00:00:00Z"}},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "empty bucket")
			}},
		{name: "an empty --since does not fall back to the last day", args: timeline("--since", "", "-o", "json"), exit: gcxerrors.ExitUsageError},
		{name: "more than 30 days is refused before the request", args: timeline("--since", "60d"), exit: gcxerrors.ExitUsageError, err: "--since 60d: a timeline covers at most 30 days"},
		{name: "a fixed window over 30 days too", args: timeline("--from", "2026-01-01T00:00:00Z", "--to", "2026-03-01T00:00:00Z"),
			exit: gcxerrors.ExitUsageError, err: "--from 2026-01-01T00:00:00Z --to 2026-03-01T00:00:00Z: a timeline covers at most 30 days"},
	})
}

func assertWindow(t *testing.T, call etCall, want time.Duration) {
	t.Helper()
	from, err := time.Parse(time.RFC3339, call.Query.Get("from"))
	require.NoError(t, err)
	to, err := time.Parse(time.RFC3339, call.Query.Get("to"))
	require.NoError(t, err)
	assert.Equal(t, want, to.Sub(from))
}

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
		{name: "no events says so on stderr", setup: func(f *fakeET) { f.reply(route, 200, `{"events":[]}`) },
			args: list("42", "--project", "314", "-o", "json"), calls: []string{route}, json: `{"items":[]}`,
			stderr: "No events of error 42 match in project 314; see the error: gcx error-tracking errors inspect 42 --project 314"},
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

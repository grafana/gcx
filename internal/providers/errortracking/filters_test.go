package errortracking_test

import (
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sinceOnly(t *testing.T, _ string, _ error, calls []etCall) {
	t.Helper()
	assert.NotEmpty(t, calls[0].Query.Get("from"))
	assert.NotContains(t, calls[0].Query, "to", "--since without --to sends no to")
}

func TestFilters(t *testing.T) {
	f := newFakeET(t)
	const someErrors = `{"errors":[{"id":1,"status":"unresolved"}],"total":1}`
	f.reply("GET projects/314/errors", 200, someErrors)
	f.reply("GET projects/314/timeline", 200, `{"bucket":"5m","buckets":[]}`)
	f.reply("GET projects/314/errors/42/timeline", 200, `{"bucket":"15m","buckets":[]}`)
	f.reply("GET projects/314/errors/42/events", 200, `{"events":[]}`)
	f.reply("GET projects/314/customFields/values", 200, `{"key":"browser","values":[]}`)
	f.reply("GET projects/314/customFields", 200, `{"keys":[]}`)
	f.reply("GET projects/314/environments", 200, `{"environments":[]}`)
	f.reply("GET projects/314/releases", 200, `{"releases":[{"release":"v1.2.3","firstSeen":"2026-11-16T08:00:00Z","lastSeen":"2026-11-16T09:00:00Z","eventCount":3}]}`)

	const from, to = "2026-11-16T00:00:00Z", "2026-11-17T00:00:00Z"
	window := func(args ...string) []string { return append([]string{"--from", from, "--to", to}, args...) }
	list := func(args ...string) []string {
		return append([]string{"errors", "list", "--project", "314"}, args...)
	}
	f.runCases(t, []cliCase{
		{name: "errors list sends the ET app's filters", args: list(window("-o", "json",
			"--reviewed", "--transaction", " /checkout ", "--reopened-from", "now-1h", "--reopened-by", "person",
			"--assignee", "u1", "--assignee", "u2", "--custom-field", "browser:Chrome", "--custom-field", "has:tenant",
			"--order", "asc", "--sort", "events-in-range")...),
			calls: []string{"GET projects/314/errors"},
			check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				q := maps.Clone(map[string][]string(calls[0].Query))
				reopened := q["reopenedFrom"]
				require.Len(t, reopened, 1)
				at, err := time.Parse(time.RFC3339, reopened[0])
				require.NoError(t, err)
				assert.WithinDuration(t, time.Now().Add(-time.Hour), at, time.Minute)
				delete(q, "reopenedFrom")
				assert.Equal(t, map[string][]string{
					"status": {"unresolved"}, "from": {from}, "to": {to}, "limit": {"50"}, "sort": {"eventsInRange"}, "order": {"asc"},
					"reviewed": {"true"}, "transaction": {"/checkout"}, "reopenedBy": {"person"},
					"assigneeUser": {"u1", "u2"}, "customField": {"browser:Chrome", "has:tenant"},
				}, q)
			}},
		{name: "errors list for unassigned errors", args: list("--assigned=false", "-o", "json"),
			calls: []string{"GET projects/314/errors"}, query: map[string][]string{"status": {"unresolved"}, "sort": {"lastSeen"}, "limit": {"50"}, "assigned": {"false"}}},
		{name: "a user without the role filtering by assignee gets a clear 403", setup: func(f *fakeET) {
			f.reply("GET projects/314/errors", http.StatusForbidden, serviceAccount403)
		}, args: list("--assignee", "u1", "-o", "json"), calls: []string{"GET projects/314/errors"}, exit: gcxerrors.ExitAuthFailure, check: assertHumanOnly},
		{name: "a service account token filtering by assignee is refused before the request", setup: func(f *fakeET) { f.useToken(serviceAccountToken) },
			args: list("--mine"), exit: gcxerrors.ExitAuthFailure,
			check: func(t *testing.T, out string, err error, calls []etCall) {
				t.Helper()
				refusedBeforeRequest("errors list --mine needs a signed-in Grafana user", "the errors: gcx error-tracking errors list --project 314")(t, out, err, calls)
				f.useToken("test-token")
			}},
		{name: "a plain list keeps the standard 403", setup: func(f *fakeET) {
			f.reply("GET projects/314/errors", http.StatusForbidden, serviceAccount403)
		}, args: list("-o", "json"), calls: []string{"GET projects/314/errors"}, exit: gcxerrors.ExitAuthFailure,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, rendered(t, err), "service account token")
			}},
		{name: "--reviewed takes a bool", args: list("--reviewed=maybe"), exit: gcxerrors.ExitUsageError, err: `invalid argument "maybe" for "--reviewed" flag`},
		{name: "--assigned takes a bool", args: list("--assigned=yes"), exit: gcxerrors.ExitUsageError, err: `invalid argument "yes" for "--assigned" flag`},
		{name: "a service account token is refused before --assigned is sent", setup: func(f *fakeET) { f.useToken(serviceAccountToken) },
			args: list("--assigned=false"), exit: gcxerrors.ExitAuthFailure,
			check: refusedBeforeRequest("errors list --assigned needs a signed-in Grafana user", "the errors: gcx error-tracking errors list --project 314")},
		{name: "a service account token is refused before --assignee is sent", setup: func(f *fakeET) { f.useToken(serviceAccountToken) },
			args: list("--assignee", "u1"), exit: gcxerrors.ExitAuthFailure,
			check: refusedBeforeRequest("errors list --assignee needs a signed-in Grafana user", "the errors: gcx error-tracking errors list --project 314")},
		{name: "errors list for my errors", args: list("--mine", "-o", "json"),
			calls: []string{"GET projects/314/errors"}, query: map[string][]string{"status": {"unresolved"}, "sort": {"lastSeen"}, "limit": {"50"}, "assignedToMe": {"true"}}},
		{name: "projects timeline filters and bucket", args: append([]string{"projects", "timeline", "314", "-o", "json",
			"--step", "5m", "--status", "unresolved", "--search", "TypeError", "--transaction", "/checkout", "--release", "v1.2",
			"--custom-field", "browser:Chrome"}, window()...),
			calls: []string{"GET projects/314/timeline"}, query: map[string][]string{
				"from": {from}, "to": {to}, "bucket": {"5m"}, "status": {"unresolved"}, "search": {"TypeError"},
				"transaction": {"/checkout"}, "release": {"v1.2"}, "customField": {"browser:Chrome"},
			}},
		{name: "projects timeline keeps a literal all for every filter but --status", args: []string{"projects", "timeline", "314", "-o", "json",
			"--status", "all", "--search", "all", "--transaction", "all", "--release", "all", "--from", from, "--to", to},
			calls: []string{"GET projects/314/timeline"}, query: map[string][]string{"from": {from}, "to": {to}, "search": {"all"}, "transaction": {"all"}, "release": {"all"}}},
		{name: "a release without a first event is refused, not a window from nothing", setup: func(f *fakeET) {
			f.reply("GET projects/314/releases", 200, `{"releases":[{"release":"v0","firstSeen":""}]}`)
		}, args: list("--status", "new", "--since-release", "v0"), calls: []string{"GET projects/314/releases"}, exit: gcxerrors.ExitGeneralError,
			err: `release "v0" in project 314 has no first event yet, so there is no window to start from`},
		{name: "re-ignoring an ignored error that the API refuses reports nothing changed", setup: func(f *fakeET) {
			f.reply("GET projects/314/errors/42", 200, `{"id":42,"status":"ignored","history":[]}`)
			f.reply("PATCH projects/314/errors/42", 409, `{"error":{"code":"invalid_transition","message":"cannot move an error from ignored to ignored","currentStatus":"ignored"}}`)
		}, args: []string{"errors", "update", "42", "--project", "314", "--status", "ignored", "--more-events", "5"},
			calls: []string{"GET projects/314/errors/42", "PATCH projects/314/errors/42"}, has: []string{"Ignored 0 of 1 errors in project 314; 1 was already ignored", "ignored (no change)"}},
		{name: "errors timeline bucket", args: append([]string{"errors", "timeline", "42", "--project", "314", "--step", "15m", "-o", "json"}, window()...),
			calls: []string{"GET projects/314/errors/42/timeline"}, query: map[string][]string{"from": {from}, "to": {to}, "bucket": {"15m"}}},
		{name: "new takes --from and runs to now", args: list("--status", "new", "--from", from, "-o", "json"),
			calls: []string{"GET projects/314/errors"}, query: map[string][]string{"status": {"unresolved"}, "reviewed": {"false"}, "firstSeenFrom": {from}, "sort": {"lastSeen"}, "limit": {"50"}}},
		{name: "regressed takes --since-release", args: list("--status", "regressed", "--since-release", "v1.2.3", "-o", "json"),
			calls: []string{"GET projects/314/releases", "GET projects/314/errors"}, query: map[string][]string{"status": {"regressed"}, "regressedFrom": {"2026-11-16T08:00:00Z"}, "sort": {"lastSeen"}, "limit": {"50"}}},
		{name: "an unknown --since-release", args: list("--status", "new", "--since-release", "v9"),
			calls: []string{"GET projects/314/releases"}, exit: gcxerrors.ExitGeneralError, err: `project 314 has no release "v9" among its latest 100. Counts can lag a minute or two behind new events`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See what arrived: gcx error-tracking raw-events list --project 314")
				assert.NotContains(t, rendered(t, err), "no event carried one")
			}},
		{name: "an empty list says to widen the filters", setup: func(f *fakeET) { f.reply("GET projects/314/errors", 200, `{"errors":[]}`) },
			args: list("--search", "zzzz"), calls: []string{"GET projects/314/errors"},
			stderr: "hint: No errors match; widen the filters or --since, or see every state with --status all. Counts can lag a minute or two behind new events; raw-events list shows arrivals at once"},
		{name: "an empty list under --status all does not suggest --status all", setup: func(f *fakeET) { f.reply("GET projects/314/errors", 200, `{"errors":[]}`) },
			args: list("--status", "all", "--search", "zzzz"), calls: []string{"GET projects/314/errors"},
			stderr: "hint: No errors match; widen the filters or --since. Counts can lag a minute or two behind new events; raw-events list shows arrivals at once"},
		{name: "an empty list in JSON keeps stdout one value", setup: func(f *fakeET) { f.reply("GET projects/314/errors", 200, `{"errors":[]}`) },
			args: list("-o", "json"), calls: []string{"GET projects/314/errors"}, json: `{"items":[]}`},
		{name: "events list custom field", args: []string{"errors", "events", "list", "42", "--project", "314", "--custom-field", "!browser:Chrome", "-o", "json"},
			calls: []string{"GET projects/314/errors/42/events"}, query: map[string][]string{"limit": {"50"}, "customField": {"!browser:Chrome"}}},
		{name: "fields list-values window", args: append([]string{"fields", "list-values", "browser", "--project", "314", "-o", "json"}, window()...),
			calls: []string{"GET projects/314/customFields/values"}, query: map[string][]string{"key": {"browser"}, "from": {from}, "to": {to}, "limit": {"100"}}},
		{name: "fields list-values --since leaves the window open", args: []string{"fields", "list-values", "browser", "--project", "314", "--since", "1h", "-o", "json"},
			calls: []string{"GET projects/314/customFields/values"}, check: sinceOnly},
		{name: "fields list --since leaves the window open", args: []string{"fields", "list", "--project", "314", "--since", "1h", "-o", "json"},
			calls: []string{"GET projects/314/customFields"}, check: sinceOnly},
		{name: "environments list window", args: append([]string{"environments", "list", "--project", "314", "-o", "json"}, window()...),
			calls: []string{"GET projects/314/environments"}, query: map[string][]string{"from": {from}, "to": {to}, "limit": {"100"}}},
		{name: "releases list window", args: append([]string{"releases", "list", "--project", "314", "-o", "json"}, window()...),
			calls: []string{"GET projects/314/releases"}, query: map[string][]string{"from": {from}, "to": {to}, "limit": {"100"}}},
		{name: "events list window", args: append([]string{"errors", "events", "list", "42", "--project", "314", "-o", "json"}, window()...),
			calls: []string{"GET projects/314/errors/42/events"}, query: map[string][]string{"limit": {"50"}, "from": {from}, "to": {to}}},
	}, usageCases(func(args ...string) []string { return args },
		list("--status", "new", "--since", "1h", "--reviewed=false"),
		list("--order", "up"),
		list("--transaction", " "),
		list("--reopened-by", "person"),
		list("--reopened-from", "now-1h", "--reopened-by", "robot"),
		list("--reopened-from", "soon"),
		list("--reopened-from", "now-8d"),
		list("--reopened-from", "2020-01-01T00:00:00Z"),
		list("--reopened-from", ""),
		list("--assignee", " "),
		list("--assignee", "u1", "--assigned"),
		list("--assigned=false", "--mine"),
		list("--mine", "--status", "resolved"),
		list("--mine", "--status", "ignored"),
		list("--mine", "--status", "all"),
		list("--custom-field", ""),
		list(strings.Split(strings.Repeat("--custom-field a:b ", 9), " ")[:18]...),
		list("--sort", "events-in-range"),
		list("--sort", "events-in-range", "--status", "new", "--since", "1h"),
		[]string{"projects", "timeline", "314", "--step", "2h"},
		[]string{"projects", "timeline", "314", "--status", "new"},
		[]string{"projects", "timeline", "314", "--search", ""},
		[]string{"projects", "timeline", "314", "--custom-field", " "},
		[]string{"errors", "timeline", "42", "--project", "314", "--step", "1w"},
		[]string{"errors", "events", "list", "42", "--project", "314", "--custom-field", ""},
		list("--custom-field", "region"),
		list("--custom-field", "region:[eu"),
		list("--custom-field", "has:"),
		list("--since-release", "v1"),
		list("--status", "new", "--since-release", "v1", "--since", "1h"),
		list("--status", "new", "--from", "2026-11-16T00:00:00Z", "--to", "2026-11-17T00:00:00Z"),
	))
}

func TestFiltersEdgeCases(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/errors", 200, `{"errors":[{"id":1,"status":"unresolved"}],"total":1}`)
	list := func(args ...string) []string {
		return append([]string{"errors", "list", "--project", "314"}, args...)
	}
	f.runCases(t, []cliCase{
		{name: "--mine=false is a no-op a service account can run", setup: func(f *fakeET) { f.useToken(serviceAccountToken) },
			args: list("--mine=false", "--json", "id"), calls: []string{"GET projects/314/errors"},
			query: map[string][]string{"status": {"unresolved"}, "sort": {"lastSeen"}, "limit": {"50"}}, json: `{"items":[{"id":1}]}`},
		{name: "--mine with a service account is still refused", setup: func(f *fakeET) { f.useToken(serviceAccountToken) },
			args: list("--mine"), exit: gcxerrors.ExitAuthFailure, err: "errors list --mine needs a signed-in Grafana user"},
	})
}

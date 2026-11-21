package errortracking_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

func TestFacetsRequests(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	f.reply("GET projects/314/timeline", 200, `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T00:00:00Z","eventCount":4}]}`)
	f.reply("GET projects/314/environments", 200, `{"environments":[{"environment":"prod","eventCount":9}]}`)
	f.reply("GET projects/314/transactions", 200, `{"transactions":[{"transaction":"/checkout","errorCount":2,"eventCount":9}]}`)
	f.reply("GET projects/314/customFields", 200, `{"keys":[{"key":"browser","valueCount":2}]}`)
	f.reply("GET projects/314/customFields/values", 200, `{"key":"browser","values":[{"value":"Chrome 130","eventCount":3}]}`)

	window := map[string][]string{"from": {"2026-11-16T00:00:00Z"}, "to": {"2026-11-17T00:00:00Z"}}
	windowCapped := map[string][]string{"from": {"2026-11-16T00:00:00Z"}, "to": {"2026-11-17T00:00:00Z"}, "limit": {"100"}}
	none := map[string][]string{"limit": {"100"}}
	f.runCases(t, []cliCase{
		{name: "projects timeline", args: []string{"projects", "timeline", "314", "--from", "2026-11-16T00:00:00Z", "--to", "2026-11-17T00:00:00Z", "-o", "json"},
			calls: []string{"GET projects/314/timeline"}, query: window,
			json: `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T00:00:00Z","eventCount":4}]}`},
		{name: "environments list", args: []string{"environments", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/environments"}, query: none,
			json: `{"items":[{"environment":"prod","firstSeen":"","lastSeen":"","eventCount":9}]}`},
		{name: "transactions list", args: []string{"transactions", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/transactions"}, query: none,
			json: `{"items":[{"transaction":"/checkout","errorCount":2,"eventCount":9}]}`},
		{name: "fields list", args: []string{"fields", "list", "--project", "314", "--from", "2026-11-16T00:00:00Z", "--to", "2026-11-17T00:00:00Z", "-o", "json"},
			calls: []string{"GET projects/314/customFields"}, query: windowCapped,
			json: `{"items":[{"key":"browser","valueCount":2,"errorCount":0,"eventCount":0}]}`},
		{name: "fields list-values with a prefix", args: []string{"fields", "list-values", "browser", "--project", "314", "--prefix", "Chr", "-o", "json"},
			calls: []string{"GET projects/314/customFields/values"}, query: map[string][]string{"key": {"browser"}, "prefix": {"Chr"}, "limit": {"100"}},
			json: `{"items":[{"value":"Chrome 130","errorCount":0,"eventCount":3,"firstSeen":"","lastSeen":""}]}`},
		{name: "fields list-values", args: []string{"fields", "list-values", "browser", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/customFields/values"}, query: map[string][]string{"key": {"browser"}, "limit": {"100"}}},
		{name: "--since without --to sends no to", args: []string{"environments", "list", "--project", "checkout-api-314", "--since", "24h", "-o", "json"},
			calls: []string{"GET projects", "GET projects/314/environments"}, check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assert.NotEmpty(t, calls[1].Query.Get("from"))
				assert.NotContains(t, calls[1].Query, "to")
			}},
		{name: "a table", args: []string{"transactions", "list", "--project", "314", "-o", "table"},
			calls: []string{"GET projects/314/transactions"}, has: []string{"/checkout"}},
		{name: "selected fields", args: []string{"transactions", "list", "--project", "314", "--json", "transaction,eventCount"},
			calls: []string{"GET projects/314/transactions"}, has: []string{`"/checkout"`}, hasNot: []string{"errorCount"}},
	})
}

func TestFacetsEmptyAndUsage(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/environments", 200, `{"environments":[]}`)
	f.reply("GET projects/314/transactions", 200, `{}`)
	f.reply("GET projects/314/timeline", 200, `{"bucket":"1h","buckets":[]}`)

	cases := []cliCase{
		{name: "no environments", args: []string{"environments", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/environments"}, json: `{"items":[]}`},
		{name: "no transactions field", args: []string{"transactions", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/transactions"}, json: `{"items":[]}`},
		{name: "no buckets", args: []string{"projects", "timeline", "314", "--since", "1h", "-o", "json"},
			calls: []string{"GET projects/314/timeline"}, json: `{"bucket":"1h","buckets":[]}`},
		{name: "the timeline defaults to the last day", args: []string{"projects", "timeline", "314", "-o", "json"},
			calls: []string{"GET projects/314/timeline"}, check: func(t *testing.T, _ string, _ error, calls []etCall) {
				t.Helper()
				assertWindow(t, calls[0], 24*time.Hour)
			}},
		{name: "a list at ET's 100 cap says so", setup: func(f *fakeET) {
			f.reply("GET projects/314/environments", 200, `{"environments":[`+strings.Repeat(`{"environment":"e"},`, 99)+`{"environment":"e"}]}`)
		}, args: []string{"environments", "list", "--project", "314", "--jq", ".list_meta", "-o", "json"},
			calls: []string{"GET projects/314/environments"}, json: `{"truncated":true,"returned":100,"cap":100}`},
		{name: "users list --limit -1", args: []string{"users", "list", "--project", "314", "--limit", "-1", "-o", "json"}, exit: gcxerrors.ExitUsageError, err: "invalid --limit -1: must be 1 to 100"},
	}
	f.runCases(t, cases, usageCases(func(args ...string) []string { return args },
		[]string{"fields", "list-values", " ", "--project", "314"},
		[]string{"users", "list", "--project", ""},
		[]string{"users", "list", "--project", "314", "--search", ""},
		[]string{"fields", "list-values", "browser", "--project", "314", "--prefix", ""},
	))
}

func TestUsersList(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/users"
	f.reply(route, 200, `{"users":[{"uid":"u1","name":"Alex","login":"alex"},{"uid":"u2","name":"Alexis","login":"alexis"}],"page":1,"perPage":100,"totalCount":3}`)

	f.runCases(t, []cliCase{
		{name: "one request with the search", args: []string{"users", "list", "--project", "314", "--search", "alex", "-o", "json"},
			calls: []string{route}, query: map[string][]string{"page": {"1"}, "limit": {"51"}, "query": {"alex"}}, json: `{"items":[
				{"uid":"u1","name":"Alex","login":"alex","avatarUrl":""},
				{"uid":"u2","name":"Alexis","login":"alexis","avatarUrl":""}]}`},
		{name: "the table leads with the UID", args: []string{"users", "list", "--project", "314", "-o", "table"},
			calls: []string{route}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.Regexp(t, `(?m)^UID +NAME +LOGIN\n`, out)
				assert.Regexp(t, `(?m)^u1 +Alex +alex\n`, out)
			}},
		{name: "one more than --limit is a cut that continues with a runnable --limit", args: []string{"users", "list", "--project", "314", "--limit", "1", "--jq", "[(.items | length), .list_meta.truncated, .list_meta.total, (.list_meta.continue | endswith(\"--limit 2\"))]", "-o", "json"},
			setup: func(f *fakeET) {
				withArgv(f.t, "gcx", "error-tracking", "users", "list", "--project", "314", "--limit", "1")
			},
			calls: []string{route}, query: map[string][]string{"page": {"1"}, "limit": {"2"}}, json: `[1, true, null, true]`},
		{name: "a full page of 100 without a total is a cut", args: []string{"users", "list", "--project", "314", "--limit", "100", "--jq", "[(.items | length), .list_meta.truncated, .list_meta.cap]", "-o", "json"},
			setup: func(f *fakeET) {
				f.reply(route, 200, `{"users":[`+strings.TrimSuffix(strings.Repeat(`{"uid":"u","name":"A","login":"a"},`, 100), ",")+`]}`)
			}, calls: []string{route}, json: `[100, true, 100]`},
		{name: "a cut below 100 continues with --limit 100", args: []string{"users", "list", "--project", "314", "--limit", "60", "--jq", "[.list_meta.cap, (.list_meta.continue | endswith(\"--limit 100\"))]", "-o", "json"},
			setup: func(f *fakeET) {
				withArgv(f.t, "gcx", "error-tracking", "users", "list", "--project", "314", "--limit", "60")
				f.reply(route, 200, `{"users":[`+strings.TrimSuffix(strings.Repeat(`{"uid":"u","name":"A","login":"a"},`, 61), ",")+`],"totalCount":150}`)
			}, calls: []string{route}, json: `[null, true]`},
		{name: "a cut at 100 says to narrow with --search", args: []string{"users", "list", "--project", "314", "--limit", "100", "-o", "json"},
			setup: func(f *fakeET) {
				f.reply(route, 200, `{"users":[`+strings.TrimSuffix(strings.Repeat(`{"uid":"u","name":"A","login":"a"},`, 100), ",")+`],"totalCount":150}`)
			}, calls: []string{route}, stderr: "showing first 100 (safety cap). Refine filters to narrow the result set"},
		{name: "a full page with more on the server is a cut", args: []string{"users", "list", "--project", "314", "--limit", "2", "--jq", "[(.items | length), .list_meta.truncated]", "-o", "json"},
			calls: []string{route}, json: `[2, true]`},
		{name: "--limit 100 asks for one page of 100", args: []string{"users", "list", "--project", "314", "--limit", "100", "-o", "json"},
			calls: []string{route}, query: map[string][]string{"page": {"1"}, "limit": {"100"}}},
		{name: "a signed-in user without the role gets the API's 403", setup: func(f *fakeET) {
			f.reply(route, http.StatusForbidden, `{"error":{"code":"forbidden","message":"current organization membership and sufficient role are required"}}`)
		}, args: []string{"users", "list", "--project", "314", "-o", "json"},
			calls: []string{route}, exit: gcxerrors.ExitAuthFailure, check: assertHumanOnly},
		{name: "a service account token is refused before any request", setup: func(f *fakeET) { f.useToken(serviceAccountToken) },
			args: []string{"users", "list", "--project", "314"}, exit: gcxerrors.ExitAuthFailure,
			check: refusedBeforeRequest("users list needs a signed-in Grafana user", "the errors: gcx error-tracking errors list --project 314")},
	})
}

func TestAPIRefusalsNameTheInput(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/customFields/values", http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"key is required and must be a custom field key"}}`)
	f.reply("GET projects/314/errors/42", 200, `{"id":42,"projectName":"Checkout API","status":"unresolved","history":[]}`)
	f.reply("PATCH projects/314/errors/42", http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"Users are not counted on this deployment, so a users condition can never be met."}}`)
	f.reply("GET projects/314/errors/42/assignees", 200, `{"assignees":[]}`)
	f.reply("PUT projects/314/errors/42/assignees/u9", http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"assignee must be an active user of this organization"}}`)
	f.runCases(t, []cliCase{
		{name: "a refused assignee shows the server message and points at the users", args: []string{"errors", "assignees", "add", "42", "u9", "--project", "314"},
			calls: []string{"GET projects/314/errors/42", "GET projects/314/errors/42/assignees", "PUT projects/314/errors/42/assignees/u9"}, exit: gcxerrors.ExitGeneralError,
			err: "assignee must be an active user of this organization (HTTP 400)",
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "Find a user's UID: gcx error-tracking users list --project 314")
			}},
		{name: "a refused custom field lookup shows the server message and points at the keys", args: []string{"fields", "list-values", "no pe", "--project", "314"},
			calls: []string{"GET projects/314/customFields/values"}, exit: gcxerrors.ExitGeneralError,
			err: "key is required and must be a custom field key (HTTP 400)",
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See the keys: gcx error-tracking fields list --project 314")
			}},
		{name: "an unsupported --more-users names the flag and the other conditions", args: []string{"errors", "update", "42", "--project", "314", "--status", "ignored", "--more-users", "3"},
			calls: []string{"GET projects/314/errors/42", "PATCH projects/314/errors/42"}, exit: gcxerrors.ExitGeneralError,
			err: "Users are not counted on this deployment, so a users condition can never be met. (HTTP 400)",
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "--more-users: ignore by --more-events, --rate-events with --rate-window, or --until instead")
			}},
	})
}

func TestUsersListEmptySaysWhy(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/users"
	f.reply(route, 200, `{"users":[],"totalCount":0}`)
	f.runCases(t, []cliCase{
		{name: "no users names the cause", args: []string{"users", "list", "--project", "314"}, calls: []string{route},
			has: []string{"UID"}, stderr: "hint: No users in project 314: the list holds the Grafana users who can see the project\n"},
		{name: "a search that matches nobody says how to see all", args: []string{"users", "list", "--project", "314", "--search", "zz", "-o", "json"}, calls: []string{route},
			json: `{"items":[]}`, stderr: `hint: No user of project 314 matches the search "zz"; see them all: gcx error-tracking users list --project 314`},
		{name: "JSON discovery prints no hint", args: []string{"users", "list", "--project", "314", "--json", "?"}, calls: []string{route},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Empty(t, f.stderr.String())
			}},
	})
}

func TestUsersListWithoutTotalCount(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/users"
	f.reply(route, 200, `{"users":[{"uid":"u1","name":"A","login":"a"}]}`)
	two := func(f *fakeET) {
		f.reply(route, 200, `{"users":[{"uid":"u1","name":"A","login":"a"},{"uid":"u2","name":"B","login":"b"}]}`)
	}
	f.runCases(t, []cliCase{
		{name: "a list of exactly --limit users is not cut", args: []string{"users", "list", "--project", "314", "--limit", "1", "-o", "json"},
			calls: []string{route}, query: map[string][]string{"page": {"1"}, "limit": {"2"}},
			json: `{"items":[{"uid":"u1","name":"A","login":"a","avatarUrl":""}]}`},
		{name: "one more than --limit means more exist", setup: two, args: []string{"users", "list", "--project", "314", "--limit", "1", "--jq", ".list_meta.truncated", "-o", "json"},
			calls: []string{route}, json: `true`},
	})
}

func TestUsersListLimitFlag(t *testing.T) {
	f := newFakeET(t)
	f.runCases(t, []cliCase{
		{name: "--limit names its range", args: []string{"users", "list", "--help"},
			has:    []string{"Maximum number of users to show, 1 to 100", "errors assignees add and delete take the UID it shows"},
			hasNot: []string{"0 means all results are returned", "10,000", "name or login contains"}},
		{name: "--limit 0 is refused before any request", args: []string{"users", "list", "--project", "314", "--limit", "0"},
			exit: gcxerrors.ExitUsageError, err: "invalid --limit 0: must be 1 to 100"},
		{name: "and so is one past the server's page", args: []string{"users", "list", "--project", "314", "--limit", "101"},
			exit: gcxerrors.ExitUsageError, err: "invalid --limit 101: must be 1 to 100"},
	})
}

package errortracking_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const twoProjects = `{"projects":[
 {"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","stats":{"unresolvedErrors":3,"eventsLast24h":40,"dailyEvents":[]}},
 {"id":5,"name":"api-2","ingestKey":"k5","stats":{"unresolvedErrors":0,"eventsLast24h":0,"dailyEvents":[]}},
 {"id":2,"name":"web","ingestKey":"k2","stats":{"unresolvedErrors":0,"eventsLast24h":0,"dailyEvents":[]}}]}`

func TestProjects(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","stats":{"dailyEvents":[]}}`)
	f.reply("GET projects/314/errors", 200, oneError)

	f.runCases(t, []cliCase{
		{name: "list as JSON has no ingest keys or DSNs", args: []string{"projects", "list", "-o", "json"},
			calls: []string{"GET projects"}, query: map[string][]string{}, hasNot: []string{"k314", "ingestKey", "dsn"}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				var list struct {
					Items []struct {
						APIVersion string `json:"apiVersion"`
						Kind       string `json:"kind"`
						Metadata   struct {
							Name      string `json:"name"`
							Namespace string `json:"namespace"`
						} `json:"metadata"`
					} `json:"items"`
					ListMeta any `json:"list_meta"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &list))
				require.Len(t, list.Items, 3)
				assert.Equal(t, "error-tracking.ext.grafana.app/v0alpha1", list.Items[0].APIVersion)
				assert.Equal(t, "Project", list.Items[0].Kind)
				assert.Equal(t, "checkout-api-314", list.Items[0].Metadata.Name)
				assert.Equal(t, "stacks-123", list.Items[0].Metadata.Namespace)
				assert.Nil(t, list.ListMeta, "a complete list has no list_meta")
			}},
		{name: "the table leads with the composite NAME and has no ID column", args: []string{"projects", "list", "-o", "table"},
			calls: []string{"GET projects"}, has: []string{"NAME", "checkout-api-314"}, hasNot: []string{"ID", "INGEST KEY", "k314", "Checkout API"},
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				assert.True(t, strings.HasPrefix(out, "NAME "), "NAME is the first column: %q", out)
			}},
		{name: "a list in YAML has no ingest keys either", args: []string{"projects", "list", "-o", "yaml"},
			calls: []string{"GET projects"}, hasNot: []string{"k314", "ingestKey", "dsn"}},
		{name: "wide shows neither", args: []string{"projects", "list", "-o", "wide"},
			calls: []string{"GET projects"}, has: []string{"REPO", "CREATED"}, hasNot: []string{"INGEST KEY", "k314", "DSN"}},
		{name: "get's table shows no ingest key", args: []string{"projects", "get", "314", "-o", "table"},
			calls: []string{"GET projects/314"}, has: []string{"NAME", "checkout-api-314"}, hasNot: []string{"ID", "INGEST KEY", "k314"}},
		{name: "get's wide table shows the DSN", args: []string{"projects", "get", "314", "-o", "wide"},
			calls: []string{"GET projects/314"}, has: []string{"DSN", "https://k314@ingest.example.com/314"}, hasNot: []string{"INGEST KEY"}},
		{name: "get's JSON has the ingest key and the DSN", args: []string{"projects", "get", "314", "--json", "spec.ingestKey,spec.dsn"},
			calls: []string{"GET projects/314"}, json: `{"spec.ingestKey":"k314","spec.dsn":"https://k314@ingest.example.com/314"}`},
		{name: "--search keeps names that contain the text, any case", args: []string{"projects", "list", "--search", "CHECK", "--json", "spec.id"},
			calls: []string{"GET projects"}, query: map[string][]string{}, json: `{"items":[{"spec.id":314}]}`},
		{name: "there is no --name alias", args: []string{"projects", "list", "--name", "api"}, exit: gcxerrors.ExitUsageError, err: "unknown flag: --name"},
		{name: "no match says so on stderr", args: []string{"projects", "list", "--search", "nope", "-o", "json"},
			calls: []string{"GET projects"}, json: `{"items":[]}`, stderr: `hint: No project name contains "nope"; see all: gcx error-tracking projects list`},
		{name: "no projects at all", setup: func(f *fakeET) { f.reply("GET projects", 200, `{"projects":[]}`) }, args: []string{"projects", "list"},
			calls: []string{"GET projects"}, stderr: "hint: No Error Tracking projects yet; create one: gcx error-tracking projects create --name <name>",
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				f.reply("GET projects", 200, twoProjects)
			}},
		{name: "--project takes the NAME", setup: func(f *fakeET) { f.reply("GET projects", 200, twoProjects) },
			args:  []string{"errors", "list", "--project", "checkout-api-314", "-o", "json"},
			calls: []string{"GET projects", "GET projects/314/errors"}},
		{name: "get by ID keeps the name-id as metadata.name", args: []string{"projects", "get", "314", "-o", "json"},
			calls: []string{"GET projects/314"}, has: []string{`"name": "checkout-api-314"`}},
	})
}

func TestProjectFlag(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	for _, id := range []string{"314", "5", "2"} {
		f.reply("GET projects/"+id+"/errors", 200, oneError)
	}
	list := func(ref string) []string { return []string{"errors", "list", "--project", ref, "--json", "id"} }
	found := func(ref string, routes ...string) cliCase {
		return cliCase{name: ref, args: list(ref), calls: routes, json: `{"items":[{"id":1}]}`}
	}
	usage := func(ref string) cliCase {
		return cliCase{name: fmt.Sprintf("%q", ref), args: list(ref), exit: gcxerrors.ExitUsageError, err: "--project must",
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), gcxerrors.SummaryInvalidCommandUsage)
			}}
	}
	f.runCases(t, []cliCase{
		found("314", "GET projects/314/errors"),
		found(" 314 ", "GET projects/314/errors"),
		found("checkout-api-314", "GET projects", "GET projects/314/errors"),
		found("CHECKOUT API", "GET projects", "GET projects/314/errors"),
		found("api-2", "GET projects", "GET projects/5/errors"),
		found("web-2", "GET projects", "GET projects/2/errors"),
		{name: "an unknown name", exit: gcxerrors.ExitGeneralError, args: list("nope"), calls: []string{"GET projects"},
			err: `no Error Tracking project has the ID or name "nope"`},
		{name: "an ambiguous NAME", setup: func(f *fakeET) {
			f.reply("GET projects", 200, `{"projects":[{"id":3,"name":"x"},{"id":9,"name":"x-3"}]}`)
		}, args: list("x-3"), calls: []string{"GET projects"}, exit: gcxerrors.ExitUsageError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "matches 2 projects: x (3), x-3 (9)")
				assert.Contains(t, rendered(t, err), "Pass one of these project IDs instead: 3, 9")
			}},
		usage(""),
		usage("0"),
		usage("-4"),
	})
}

const oneError = `{"errors":[{"id":1,"status":"unresolved"}],"total":1}`

func TestErrorsListRequest(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/errors", 200, oneError)
	for _, tc := range []struct {
		name  string
		args  []string
		check func(t *testing.T, q map[string]string)
	}{
		{"defaults", nil, func(t *testing.T, q map[string]string) {
			t.Helper()
			assert.Equal(t, map[string]string{"status": "unresolved", "sort": "lastSeen", "limit": "50"}, q)
		}},
		{"new moves the window to first seen", []string{"--status", "new", "--since", "30m"}, func(t *testing.T, q map[string]string) {
			t.Helper()
			assert.Equal(t, "unresolved", q["status"])
			assert.Equal(t, "false", q["reviewed"], "a reviewed error is not new")
			assert.NotEmpty(t, q["firstSeenFrom"])
			assert.NotContains(t, q, "to")
			assert.NotContains(t, q, "from")
		}},
		{"regressed moves the window to regression time", []string{"--status", "regressed", "--since", "1h"}, func(t *testing.T, q map[string]string) {
			t.Helper()
			assert.Equal(t, "regressed", q["status"])
			assert.NotEmpty(t, q["regressedFrom"])
			assert.NotContains(t, q, "to")
			assert.NotContains(t, q, "from")
		}},
		{"filters", []string{"--status", "all", "--search", " TypeError ", "--release", "v1.2", "--sort", "event-count", "--limit", "7"}, func(t *testing.T, q map[string]string) {
			t.Helper()
			assert.Equal(t, map[string]string{"status": "all", "search": "TypeError", "release": "v1.2", "sort": "eventCount", "limit": "7"}, q)
		}},
		{"a window pages by 50, so ET returns the affected users", []string{"--since", "1h", "--limit", "0"}, func(t *testing.T, q map[string]string) {
			t.Helper()
			assert.Equal(t, "50", q["limit"])
		}},
		{"relative times go out as RFC 3339 UTC", []string{"--from", "now-1h", "--to", "now"}, func(t *testing.T, q map[string]string) {
			t.Helper()
			from, err := time.Parse(time.RFC3339, q["from"])
			require.NoError(t, err)
			assert.True(t, strings.HasSuffix(q["from"], "Z"))
			assert.WithinDuration(t, time.Now().Add(-time.Hour), from, time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.run(append([]string{"errors", "list", "--project", "314", "-o", "json"}, tc.args...)...)
			require.NoError(t, err)
			calls := f.takeCalls()
			require.Len(t, calls, 1)
			q := map[string]string{}
			for k := range calls[0].Query {
				q[k] = calls[0].Query.Get(k)
			}
			tc.check(t, q)
		})
	}

	for _, bad := range [][]string{
		{"--status", "new"},
		{"--status", "open"},
		{"--sort", "newest"},
		{"--from", "yesterday-ish", "--to", "now"},
		{"--from", "now-1h"},
		{"--status", "new", "--since", "1h", "--to", "now"},
		{"--status", "regressed", "--from", "now-1h", "--to", "now"},
		{"--search", ""},
		{"--release", " "},
		{"--since", ""},
		{"--from", "", "--to", ""},
		{"--to", " "},
	} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			_, err := f.run(append([]string{"errors", "list", "--project", "314", "-o", "json"}, bad...)...)
			require.Error(t, err)
			assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err), "%v", err)
		})
	}
	_, err := f.run("errors", "list", "--project", "314", "--limit", "-1", "-o", "json")
	require.ErrorContains(t, err, "must be >= 0")
	assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err))
	assert.Contains(t, rendered(t, err), gcxerrors.SummaryInvalidCommandUsage)
	assert.Empty(t, f.takeCalls(), "bad input fails before any call")
}

func TestIgnoredUntilColumn(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/errors", 200, `{"errors":[
		{"id":1,"status":"ignored","eventCount":3,"ignoredUntilEvents":13,"ignoredUntilRateEvents":5,"ignoredUntilRateWindowMinutes":60},
		{"id":2,"status":"ignored","ignoredUntilAt":"2026-11-25T00:00:00Z","ignoredUntilUsers":20},
		{"id":3,"status":"unresolved"}],"total":3}`)
	f.runCases(t, []cliCase{
		{name: "wide says how far each ignore has to go", args: []string{"errors", "list", "--project", "314", "--status", "all", "-o", "wide"},
			calls: []string{"GET projects/314/errors"}, has: []string{"13 events (10 more), 5 events in 1h", "2026-11-25 00:00Z, 20 users"}, hasNot: []string{"0s"}},
	})
}

func TestErrorsReviewFields(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/errors", 200, `{"errors":[{"id":1,"reviewed":true,"reviewedAt":"2026-11-16T10:00:00Z","reviewedBy":"u1","reviewedByName":"Alex","reopenedAt":"2026-11-17T09:00:00Z"},{"id":2}]}`)
	f.runCases(t, []cliCase{
		{name: "JSON keeps the review and reopen fields", args: []string{"errors", "list", "--project", "314", "--json", "id,reviewed,reviewedAt,reviewedBy,reviewedByName,reopenedAt"},
			calls: []string{"GET projects/314/errors"}, json: `{"items":[
				{"id":1,"reviewed":true,"reviewedAt":"2026-11-16T10:00:00Z","reviewedBy":"u1","reviewedByName":"Alex","reopenedAt":"2026-11-17T09:00:00Z"},
				{"id":2,"reviewed":false,"reviewedAt":null,"reviewedBy":null,"reviewedByName":null,"reopenedAt":null}]}`},
	})
}

func TestErrorsListPaging(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors"
	f.handle(route, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"errors":[{"id":1},{"id":2}],"total":3,"nextCursor":"c1"}`))
		case "c1":
			_, _ = w.Write([]byte(`{"errors":[{"id":3}],"total":3}`))
		}
	})
	type list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		ListMeta *struct {
			Truncated bool `json:"truncated"`
			Returned  int  `json:"returned"`
		} `json:"list_meta"`
	}

	f.runCases(t, []cliCase{
		{name: "--limit 0 follows every page", args: []string{"errors", "list", "--project", "314", "--limit", "0", "-o", "json"},
			calls: []string{route, route}, check: func(t *testing.T, out string, _ error, calls []etCall) {
				t.Helper()
				var got list
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				assert.Len(t, got.Items, 3)
				assert.Nil(t, got.ListMeta)
				assert.Equal(t, "200", calls[0].Query.Get("limit"))
				assert.Equal(t, "c1", calls[1].Query.Get("cursor"))
			}},
		{name: "the limit is reached on the first page", args: []string{"errors", "list", "--project", "314", "--limit", "2", "-o", "json"},
			calls: []string{route}, check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				var got list
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				assert.Len(t, got.Items, 2)
				require.NotNil(t, got.ListMeta)
				assert.True(t, got.ListMeta.Truncated)
				assert.Equal(t, 2, got.ListMeta.Returned)
			}},
		{name: "selected fields from every page", args: []string{"errors", "list", "--project", "314", "--limit", "0", "--json", "id"},
			calls: []string{route, route}, json: `{"items":[{"id":1},{"id":2},{"id":3}]}`},
	})
}

func TestTablesDropControlCharacters(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors"
	f.reply(route, 200, `{"errors":[{"id":1,"message":"boom\u001b]0;x\u0007 \u001b[31mred\u009b\u202e\u2028\u2029"}]}`)
	f.runCases(t, []cliCase{
		{name: "a table", args: []string{"errors", "list", "--project", "314", "-o", "table"}, calls: []string{route},
			has: []string{"boom]0;x [31mred"}, hasNot: []string{"\x1b", "\a", "\u009b", "\u202e", "\u2028", "\u2029"}},
		{name: "JSON keeps the raw text", args: []string{"errors", "list", "--project", "314", "-o", "json"}, calls: []string{route},
			has: []string{`\u001b[31m`}},
	})
}

func TestPagingStops(t *testing.T) {
	f := newFakeET(t)
	pages := map[string]string{
		"":  `{"errors":[{"id":1}],"nextCursor":"a"}`,
		"a": `{"errors":[{"id":2}],"nextCursor":"b"}`,
		"b": `{"errors":[{"id":3}],"nextCursor":"a"}`,
	}
	f.handle("GET projects/314/errors", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, pages[r.URL.Query().Get("cursor")])
	})
	_, err := f.run("errors", "list", "--project", "314", "--limit", "0", "--jq", "[.items[].id]")
	require.ErrorContains(t, err, "the server repeated a page cursor")
	calls := f.takeCalls()
	cursors := make([]string, 0, len(calls))
	for _, c := range calls {
		cursors = append(cursors, c.Query.Get("cursor"))
	}
	assert.Equal(t, []string{"", "a", "b"}, cursors, "paging stops instead of looping")
}

func TestErrorsGet(t *testing.T) {
	f := newFakeET(t)
	const route = "GET projects/314/errors/42"
	f.reply(route, 200, `{"id":42,"projectId":314,"status":"unresolved","regressed":true,"message":"boom\nat x","eventCount":9,"ignoredUsersSeen":3,
		"affectedUsersCoverage":"partial","affectedUsersInRange":2,"identifiedEventsInRange":5,
		"latestEvent":{"id":7,"eventId":"e7","occurredAt":"2026-11-16T10:00:00.000Z","payload":{"exception":{"values":[]}}},"history":[]}`)

	f.runCases(t, []cliCase{
		{name: "selected fields", args: []string{"errors", "get", "42", "--project", "314", "--json", "id,message,eventCount,ignoredUsersSeen,affectedUsersCoverage,affectedUsersInRange,identifiedEventsInRange"},
			calls: []string{route}, json: `{"id":42,"message":"boom\nat x","eventCount":9,"ignoredUsersSeen":3,
				"affectedUsersCoverage":"partial","affectedUsersInRange":2,"identifiedEventsInRange":5}`},
		{name: "table cells stay on one line", args: []string{"error", "get", "42", "--project", "314", "-o", "table"},
			calls: []string{route}, has: []string{"regressed", "boom …", " ago"}, hasNot: []string{"at x", "10:00:00.000Z"}},
		{name: "wide shows full UTC times", args: []string{"error", "get", "42", "--project", "314", "-o", "wide"},
			calls: []string{route}, has: []string{"2026-11-16 10:00:00Z"}, hasNot: []string{" ago"}},
		{name: "a bad error ID", exit: gcxerrors.ExitUsageError, args: []string{"errors", "get", "x", "--project", "314"}, err: `<error> "x" must be a positive error ID`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See them: gcx error-tracking errors list --project 314")
			}},
		{name: "no error ID", args: []string{"errors", "get", "--project", "314"}, exit: gcxerrors.ExitUsageError, err: "accepts 1 arg(s), received 0"},
		{name: "a missing error names the project when it is known", exit: gcxerrors.ExitGeneralError, setup: func(f *fakeET) {
			f.reply("GET projects", 200, twoProjects)
			f.reply("GET projects/314/errors/99", 404, `{"error":{"code":"not_found","message":"not found"}}`)
		}, args: []string{"errors", "get", "99", "--project", "Checkout API"}, calls: []string{"GET projects", "GET projects/314/errors/99"},
			err: "Error Tracking has no error 99 in project Checkout API (ID 314)"},
	})
}

func TestReleasesList(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/releases", 200, `{"releases":[{"release":"v1.2","eventCount":4}]}`)
	out, err := f.run("releases", "list", "--project", "314", "--since", "7d", "-o", "json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{"release":"v1.2","firstSeen":"","lastSeen":"","eventCount":4}]}`, out)
	q := f.takeCalls()[0].Query
	assert.NotEmpty(t, q.Get("from"))
	assert.NotContains(t, q, "to")
}

func TestForbidden(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", http.StatusForbidden, `{"error":{"code":"forbidden","message":"You cannot read projects in this stack."}}`)
	_, err := f.run("projects", "list", "-o", "json")
	out := rendered(t, err)
	assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
	assert.Contains(t, out, "You cannot read projects in this stack.")
	assert.Contains(t, out, "Check your roles and access-policy scopes")
	assert.Equal(t, gcxerrors.ExitAuthFailure, exitCode(err))
}

func TestErrorTrackingNotAvailable(t *testing.T) {
	f := newFakeET(t)
	const k8s404 = `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"the server could not find the requested resource","reason":"NotFound","code":404}`
	const notEnabled = "Error Tracking isn't enabled for this stack.\nAsk your Grafana Labs account team to enable it."
	for _, tc := range []struct {
		name, body, summary, details string
		status                       int
	}{
		{"ET 404", `{"error":{"code":"not_found","message":"project 314 not found"}}`, gcxerrors.SummaryResourceNotFound, "", http.StatusNotFound},
		{"ET 503", `{"error":{"code":"unavailable","message":"try later"}}`, gcxerrors.SummaryAPIError, "", http.StatusServiceUnavailable},
		{"k8s 404", k8s404, gcxerrors.SummaryEndpointNotAvailable, notEnabled, http.StatusNotFound},
		{"plain 404", "404 page not found", gcxerrors.SummaryEndpointNotAvailable, notEnabled, http.StatusNotFound},
		{"a 503 without an Error Tracking body is a server error", "no healthy upstream", gcxerrors.SummaryAPIError, "", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.reply("GET projects/314", tc.status, tc.body)
			_, err := f.run("projects", "get", "314", "-o", "json")
			d := fail.ErrorToDetailedError(err)
			require.NotNil(t, d)
			var out bytes.Buffer
			require.NoError(t, d.WriteJSON(&out, exitCode(err)))
			var got struct {
				Error struct {
					Summary  string `json:"summary"`
					ExitCode int    `json:"exitCode"`
					Details  string `json:"details"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			assert.Equal(t, tc.summary, got.Error.Summary)
			assert.Equal(t, gcxerrors.ExitGeneralError, got.Error.ExitCode)
			if tc.details != "" {
				assert.Equal(t, tc.details, got.Error.Details)
			} else {
				assert.NotContains(t, got.Error.Details, "not available")
			}
			want := []string{"GET projects/314"}
			if tc.status == http.StatusServiceUnavailable {
				want = []string{"GET projects/314", "GET projects/314", "GET projects/314", "GET projects/314"}
			}
			assert.Equal(t, want, routesOf(f.takeCalls()), "reads retry a 503 and never a 404")
		})
	}
}

func TestErrorTextDropsControlCharacters(t *testing.T) {
	f := newFakeET(t)
	for _, body := range []string{
		`{"error":{"code":"bad","message":"no\u001b[31m\u009b2J"}}`,
		`{"message":"no","traceID":"t\u001b]0;x\u0007"}`,
		`{"err":"no\u001b[2J\u202e"}`,
	} {
		t.Run(body, func(t *testing.T) {
			f.reply("GET projects", http.StatusBadRequest, body)
			_, err := f.run("projects", "list", "-o", "json")
			out := rendered(t, err)
			assert.NotContains(t, out, "\x1b")
			assert.NotContains(t, out, "\u009b")
			assert.NotContains(t, out, "\u202e")
			assert.Contains(t, out, "no")
		})
	}
	f.takeCalls()
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if emitted, ok := errors.AsType[*gcxerrors.EmittedError](err); ok {
		return emitted.Code
	}
	if d := fail.ErrorToDetailedError(err); d != nil && d.ExitCode != nil {
		return *d.ExitCode
	}
	return 1
}

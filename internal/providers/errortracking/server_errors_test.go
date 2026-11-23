package errortracking_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/errortracking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rendered(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	d := fail.ErrorToDetailedError(err)
	require.NotNil(t, d, "%v", err)
	return d.Error()
}

func withValue(args []string, flag, value string) ([]string, bool) {
	out := append([]string{}, args...)
	for i, a := range out {
		if a == flag && i+1 < len(out) {
			out[i+1] = value
			return out, true
		}
	}
	return nil, false
}

func byName(args []string) ([]string, bool) {
	out := append([]string{}, args...)
	for i, a := range out {
		if a == "--project" && i+1 < len(out) {
			out[i+1] = "checkout-api"
			return out, true
		}
	}
	if len(out) > 2 && (out[0] == "projects" && out[2] == "314" || out[0] == "heatmaps" && out[2] == "3") {
		out[2] = "by name"
		return out, true
	}
	return nil, false
}

type serverErrorCase struct {
	args           []string
	route, missing string
	human          bool
	pre            []string
	conflict       string
}

func serverErrorCases() []serverErrorCase {
	p, e := "project 314", "error 42 in project 314"
	return []serverErrorCase{
		{args: []string{"projects", "list"}, route: "GET projects"},
		{args: []string{"projects", "get", "314"}, route: "GET projects/314", missing: p},
		{args: []string{"projects", "use", "314"}, route: "GET projects/314", missing: p},
		{args: []string{"projects", "timeline", "314"}, route: "GET projects/314/timeline", missing: p},
		{args: []string{"projects", "create", "--name", "web"}, route: "POST projects", conflict: "GET projects"},
		{args: []string{"projects", "update", "314", "--description", "web"}, route: "PATCH projects/314", missing: p},
		{args: []string{"projects", "delete", "314", "--force"}, route: "DELETE projects/314", missing: "project web (ID 314)", pre: []string{"GET projects/314"}},
		{args: []string{"projects", "ingest-keys", "rotate", "314", "--force"}, route: "POST projects/314/ingestKey/rotate", missing: p},
		{args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force"}, route: "POST projects/314/ingestKey/revokePrevious", missing: "project web (ID 314)", pre: []string{"GET projects/314"}},
		{args: []string{"errors", "list", "--project", "314"}, route: "GET projects/314/errors", missing: p},
		{args: []string{"errors", "list", "--project", "314", "--mine"}, route: "GET projects/314/errors", missing: p, human: true},
		{args: []string{"errors", "get", "42", "--project", "314"}, route: "GET projects/314/errors/42", missing: e},
		{args: []string{"errors", "inspect", "42", "--project", "314"}, route: "GET projects/314/errors/42", missing: e},
		{args: []string{"errors", "update", "42", "--project", "314", "--status", "resolved"}, route: "PATCH projects/314/errors/42", missing: e, pre: []string{"GET projects/314/errors/42"}},
		{args: []string{"errors", "update", "42", "--project", "314", "--reviewed"}, route: "POST projects/314/errors/42/review", missing: e, human: true, pre: []string{"GET projects/314/errors/42"}},
		{args: []string{"errors", "timeline", "42", "--project", "314"}, route: "GET projects/314/errors/42/timeline", missing: e},
		{args: []string{"errors", "events", "list", "42", "--project", "314"}, route: "GET projects/314/errors/42/events", missing: e},
		{args: []string{"errors", "fields", "list", "42", "--project", "314"}, route: "GET projects/314/errors/42/customFields", missing: e},
		{args: []string{"errors", "activity", "list", "42", "--project", "314"}, route: "GET projects/314/errors/42/activity", missing: e},
		{args: []string{"errors", "comments", "create", "42", "--project", "314", "--body", "x"}, route: "POST projects/314/errors/42/comments", missing: "error 42 in project 314"},
		{args: []string{"errors", "comments", "update", "42", "7", "--project", "314", "--body", "x"}, route: "PATCH projects/314/errors/42/comments/7", missing: "comment 7 in error 42 in project 314"},
		{args: []string{"errors", "comments", "delete", "42", "7", "--project", "314", "--force"}, route: "DELETE projects/314/errors/42/comments/7", missing: "comment 7 in error 42 in project 314"},
		{args: []string{"errors", "assignees", "list", "42", "--project", "314"}, route: "GET projects/314/errors/42/assignees", missing: e, human: true},
		{args: []string{"errors", "assignees", "add", "42", "u2", "--project", "314"}, route: "PUT projects/314/errors/42/assignees/u2", missing: e, human: true, pre: []string{"GET projects/314/errors/42", "GET projects/314/errors/42/assignees"}},
		{args: []string{"errors", "assignees", "add", "42", "me", "--project", "314"}, route: "GET projects/314/errors/42", missing: e, human: true},
		{args: []string{"errors", "assignees", "delete", "42", "me", "--project", "314"}, route: "GET projects/314/errors/42", missing: e, human: true},
		{args: []string{"errors", "assignees", "delete", "42", "u1", "--project", "314"}, route: "DELETE projects/314/errors/42/assignees/u1", missing: e, human: true, pre: []string{"GET projects/314/errors/42", "GET projects/314/errors/42/assignees"}},
		{args: []string{"errors", "events", "get", "42", "e7", "--project", "314"}, route: "GET projects/314/errors/42/events/e7", missing: "event e7 in " + e},
		{args: []string{"raw-events", "list", "--project", "314"}, route: "GET projects/314/rawEvents", missing: p},
		{args: []string{"raw-events", "get", "9", "--project", "314"}, route: "GET projects/314/rawEvents/9", missing: "raw event 9 in " + p},
		{args: []string{"releases", "list", "--project", "314"}, route: "GET projects/314/releases", missing: p},
		{args: []string{"releases", "status", "--project", "314"}, route: "GET projects/314/releases", missing: p},
		{args: []string{"environments", "list", "--project", "314"}, route: "GET projects/314/environments", missing: p},
		{args: []string{"transactions", "list", "--project", "314"}, route: "GET projects/314/transactions", missing: p},
		{args: []string{"fields", "list", "--project", "314"}, route: "GET projects/314/customFields", missing: p},
		{args: []string{"fields", "list-values", "browser", "--project", "314"}, route: "GET projects/314/customFields/values", missing: p},
		{args: []string{"users", "list", "--project", "314"}, route: "GET projects/314/users", missing: p, human: true},
		{args: []string{"heatmaps", "list"}, route: "GET heatmaps"},
		{args: []string{"heatmaps", "get", "3"}, route: "GET heatmaps/3", missing: "heatmap 3"},
		{args: []string{"heatmaps", "create", "--name", "x", "--field", "region", "--window-days", "7", "--projects", "314"}, route: "POST heatmaps", conflict: "GET heatmaps"},
		{args: []string{"heatmaps", "update", "3", "--window-days", "7"}, route: "PATCH heatmaps/3", missing: "heatmap 3"},
		{args: []string{"heatmaps", "delete", "3", "--force"}, route: "DELETE heatmaps/3", missing: "heatmap 3", pre: []string{"GET heatmaps/3"}},
		{args: []string{"heatmaps", "report", "3"}, route: "GET heatmaps/3/data", missing: "heatmap 3"},
		{args: []string{"heatmaps", "breakdown", "3", "--value", "eu", "--since", "1d"}, route: "GET heatmaps/3/data/breakdown", missing: "heatmap 3"},
	}
}

func TestServerErrors(t *testing.T) {
	before := map[string]string{
		"GET projects/314":                     `{"id":314,"name":"web","previousIngestKeyUntil":"2099-11-18T09:00:00Z","stats":{"dailyEvents":[]}}`,
		"GET projects/314/errors/42":           `{"id":42,"status":"unresolved","history":[]}`,
		"GET projects/314/errors/42/assignees": `{"assignees":[{"userUid":"u1"}]}`,
		"GET heatmaps/3":                       `{"id":3,"name":"x","windowDays":7,"projectIds":[]}`,
		"GET projects":                         `{"projects":[]}`,
		"GET heatmaps":                         `{"heatmaps":[]}`,
	}
	for _, tc := range serverErrorCases() {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Run("a bad output format", func(t *testing.T) {
				f := newFakeET(t)
				_, err := f.run(append(tc.args, "-o", "bogus")...)
				assert.Empty(t, routesOf(f.takeCalls()), "nothing is sent")
				assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err))
				assert.Contains(t, rendered(t, err), gcxerrors.SummaryInvalidCommandUsage)
				assert.Contains(t, rendered(t, err), "unknown output format 'bogus'. Valid formats are: agents, json, ")
			})
			t.Run("no Grafana server", func(t *testing.T) {
				f := newFakeET(t)
				f.cfg = write(t, t.TempDir(), "config.yaml", "version: 1\ncurrent-context: test\ncontexts:\n  test:\n    stack: test\nstacks:\n  test:\n    slug: test\n")
				_, err := f.run(append(tc.args, "-o", "json")...)
				assert.Empty(t, routesOf(f.takeCalls()), "nothing is sent")
				assert.Equal(t, gcxerrors.ExitGeneralError, exitCode(err))
				out := rendered(t, err)
				assert.Contains(t, out, gcxerrors.SummaryInvalidConfiguration)
				assert.Contains(t, out, "context references no stack with grafana config")
			})
			for _, flag := range []string{"--project"} {
				if bad, ok := withValue(tc.args, flag, "0"); ok {
					t.Run(flag+" 0", func(t *testing.T) {
						f := newFakeET(t)
						_, err := f.run(append(bad, "-o", "json")...)
						assert.Empty(t, routesOf(f.takeCalls()), "nothing is sent")
						assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err), "%v", err)
						assert.Contains(t, rendered(t, err), flag)
						assert.Contains(t, rendered(t, err), "must be a positive")
					})
				}
			}
			if len(tc.args) > 2 && strings.Trim(tc.args[2], "0123456789") == "" {
				t.Run("a positional ID of 0", func(t *testing.T) {
					f := newFakeET(t)
					bad := append([]string{}, tc.args...)
					bad[2] = "0"
					_, err := f.run(append(bad, "-o", "json")...)
					assert.Empty(t, routesOf(f.takeCalls()), "nothing is sent")
					assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err), "%v", err)
					assert.Contains(t, rendered(t, err), "must be a positive")
				})
			}
			if c, _, err := (&errortracking.Provider{}).Commands()[0].Find(tc.args); err == nil && c.Flags().Lookup("since") != nil {
				t.Run("an empty --since", func(t *testing.T) {
					f := newFakeET(t)
					_, err := f.run(replaced(tc.args, "--since", "", "-o", "json")...)
					assert.Empty(t, routesOf(f.takeCalls()), "nothing is sent")
					assert.Equal(t, gcxerrors.ExitUsageError, exitCode(err), "%v", err)
					assert.Contains(t, rendered(t, err), "--since must not be empty")
				})
			}
			if named, ok := byName(tc.args); ok {
				t.Run("a name lookup that fails", func(t *testing.T) {
					f := newFakeET(t)
					f.reply("GET projects", 500, `{"error":{"code":"x","message":"server says no"}}`)
					f.reply("GET heatmaps", 500, `{"error":{"code":"x","message":"server says no"}}`)
					_, err := f.run(append(named, "-o", "json")...)
					list := "GET projects"
					if named[0] == "heatmaps" && named[2] == "by name" {
						list = "GET heatmaps"
					}
					assert.Equal(t, []string{list}, routesOf(f.takeCalls()), "only the lookup is sent")
					assert.Equal(t, gcxerrors.ExitGeneralError, exitCode(err))
					assert.Contains(t, rendered(t, err), "server says no (HTTP 500)")
				})
			}
			for _, st := range []struct {
				status, exit int
				summary      string
			}{
				{401, gcxerrors.ExitAuthFailure, gcxerrors.SummaryAuthenticationFailed},
				{403, gcxerrors.ExitAuthFailure, gcxerrors.SummaryAuthorizationFailed},
				{404, gcxerrors.ExitGeneralError, gcxerrors.SummaryResourceNotFound},
				{409, gcxerrors.ExitGeneralError, "Resource conflict"},
				{500, gcxerrors.ExitGeneralError, gcxerrors.SummaryAPIError},
				{503, gcxerrors.ExitGeneralError, gcxerrors.SummaryAPIError},
			} {
				if st.status == 503 && strings.HasPrefix(tc.route, "GET ") && tc.route != "GET projects" {
					continue
				}
				t.Run(strconv.Itoa(st.status), func(t *testing.T) {
					f := newFakeET(t)
					for _, r := range append(append([]string{}, tc.pre...), tc.conflict) {
						if r != "" {
							f.reply(r, 200, before[r])
						}
					}
					f.reply(tc.route, st.status, `{"error":{"code":"x","message":"server says no"}}`)
					_, err := f.run(append(tc.args, "-o", "json")...)
					want := append(append([]string{}, tc.pre...), tc.route)
					if st.status == 503 && strings.HasPrefix(tc.route, "GET ") {
						want = []string{tc.route, tc.route, tc.route, tc.route}
					}
					if st.status == 409 && tc.conflict != "" {
						want = append(want, tc.conflict)
					}
					assert.Equal(t, want, routesOf(f.takeCalls()), "reads are retried on 503, writes are sent once")
					assert.Equal(t, st.exit, exitCode(err), "%v", err)
					out := rendered(t, err)
					assert.Contains(t, out, st.summary)
					switch {
					case st.status == 404 && tc.missing != "":
						assert.Contains(t, out, "Error Tracking has no "+tc.missing)
						assert.Contains(t, out, "List what exists: gcx error-tracking ")
					default:
						assert.Equal(t, 1, strings.Count(out, "server says no"), "the server message is printed once: %s", out)
					}
					if st.status == 403 && tc.human {
						assert.Contains(t, out, "Ask an organization admin for the ")
					}
				})
			}
		})
	}
}

func TestConfirmLookupFails(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314", 500, `{"error":{"code":"x","message":"server says no"}}`)
	f.reply("GET heatmaps/3", 500, `{"error":{"code":"x","message":"server says no"}}`)
	human := func(*fakeET) { agent.SetFlag(false) }
	t.Setenv("GCX_AUTO_APPROVE", "")
	os.Unsetenv("GCX_AUTO_APPROVE")
	t.Cleanup(agent.ResetForTesting)
	cases := make([]cliCase, 0, 4)
	for _, args := range [][]string{
		{"projects", "delete", "314"}, {"projects", "ingest-keys", "rotate", "314"},
		{"projects", "ingest-keys", "delete-previous", "314"}, {"heatmaps", "delete", "3"},
	} {
		route := "GET projects/314"
		if args[0] == "heatmaps" {
			route = "GET heatmaps/3"
		}
		cases = append(cases, cliCase{name: strings.Join(args, " "), setup: human, stdin: "y\n", args: args,
			calls: []string{route}, exit: gcxerrors.ExitGeneralError, err: "server says no (HTTP 500)"})
	}
	f.runCases(t, cases)
}

func TestTransportErrors(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314/errors", 200, `{"errors":`)
	f.reply("GET projects/314/releases", 200, `{"releases":{"v1":1}}`)
	f.reply("GET projects", 200, twoProjects)
	f.runCases(t, []cliCase{
		{name: "a facet list of the wrong shape", args: []string{"releases", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/releases"}, err: "cannot unmarshal object", exit: gcxerrors.ExitGeneralError},
		{name: "a heatmap project name that matches nothing", exit: gcxerrors.ExitGeneralError, args: []string{"heatmaps", "create", "--name", "x", "--field", "f", "--window-days", "7", "--projects", "nope"},
			calls: []string{"GET projects"}, err: `no Error Tracking project has the ID or name "nope"`},
		{name: "a new heatmap project set that matches nothing", exit: gcxerrors.ExitGeneralError, args: []string{"heatmaps", "update", "3", "--projects", "nope"},
			calls: []string{"GET projects"}, err: `no Error Tracking project has the ID or name "nope"`},
		{name: "a response that is not JSON", args: []string{"errors", "list", "--project", "314", "-o", "json"},
			calls: []string{"GET projects/314/errors"}, err: "error tracking: decode projects/314/errors response", exit: gcxerrors.ExitGeneralError},
	})

	dir := scratchEnv(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	cfg := write(t, dir, "config.yaml", "version: 1\ncurrent-context: test\ncontexts:\n  test:\n    stack: test\nstacks:\n  test:\n    grafana:\n      server: "+closed.URL+"\n      token: test-token\n      stack-id: 123\n")
	cmd := (&errortracking.Provider{}).Commands()[0]
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--config", cfg, "projects", "list", "-o", "json"})
	err := cmd.ExecuteContext(t.Context())
	t.Run("an unreachable Grafana server", func(t *testing.T) {
		out := rendered(t, err)
		assert.Contains(t, out, gcxerrors.SummaryNetworkError)
		assert.Contains(t, out, closed.URL+"/apis/error-tracking.ext.grafana.app/v0alpha1/namespaces/stacks-123/projects")
		assert.Contains(t, out, "dial tcp "+strings.TrimPrefix(closed.URL, "http://"))
		assert.Contains(t, out, "Make sure that the API is reachable")
		assert.Equal(t, gcxerrors.ExitGeneralError, exitCode(err))
	})
}

package errortracking_test

import (
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

func byName(args []string) ([]string, bool) {
	out := append([]string{}, args...)
	if len(out) > 2 && out[0] == "projects" && out[2] == "314" {
		out[2] = "by name"
		return out, true
	}
	return nil, false
}

type serverErrorCase struct {
	args           []string
	route, missing string
	pre            []string
	conflict       string
}

func serverErrorCases() []serverErrorCase {
	p := "project 314"
	return []serverErrorCase{
		{args: []string{"projects", "list"}, route: "GET projects"},
		{args: []string{"projects", "get", "314"}, route: "GET projects/314", missing: p},
		{args: []string{"projects", "timeline", "314"}, route: "GET projects/314/timeline", missing: p},
		{args: []string{"projects", "create", "--name", "web"}, route: "POST projects", conflict: "GET projects"},
		{args: []string{"projects", "update", "314", "--description", "web"}, route: "PATCH projects/314", missing: p},
		{args: []string{"projects", "delete", "314", "--force"}, route: "DELETE projects/314", missing: "project web (ID 314)", pre: []string{"GET projects/314"}},
		{args: []string{"projects", "ingest-keys", "rotate", "314", "--force"}, route: "POST projects/314/ingestKey/rotate", missing: p},
		{args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force"}, route: "POST projects/314/ingestKey/revokePrevious", missing: "project web (ID 314)", pre: []string{"GET projects/314"}},
	}
}

func TestServerErrors(t *testing.T) {
	before := map[string]string{
		"GET projects/314": `{"id":314,"name":"web","previousIngestKeyUntil":"2099-11-18T09:00:00Z","stats":{"dailyEvents":[]}}`,
		"GET projects":     `{"projects":[]}`,
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
					_, err := f.run(append(named, "-o", "json")...)
					list := "GET projects"
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
				})
			}
		})
	}
}

func TestConfirmLookupFails(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314", 500, `{"error":{"code":"x","message":"server says no"}}`)
	human := func(*fakeET) { agent.SetFlag(false) }
	t.Setenv("GCX_AUTO_APPROVE", "")
	os.Unsetenv("GCX_AUTO_APPROVE")
	t.Cleanup(agent.ResetForTesting)
	cases := make([]cliCase, 0, 4)
	for _, args := range [][]string{
		{"projects", "delete", "314"}, {"projects", "ingest-keys", "rotate", "314"},
		{"projects", "ingest-keys", "delete-previous", "314"},
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

package errortracking_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectsCreate(t *testing.T) {
	f := newFakeET(t)
	f.reply("POST projects", http.StatusCreated, `{"id":18,"name":"Checkout API","stats":{"dailyEvents":[]}}`)
	taken := func(f *fakeET) {
		f.reply("POST projects", http.StatusConflict, `{"error":{"code":"conflict","message":"a project with this name already exists"}}`)
		f.reply("GET projects", 200, twoProjects)
	}

	f.runCases(t, []cliCase{
		{name: "unset flags are not sent", args: []string{"projects", "create", "--name", "Checkout API", "--repo-url", "https://git.example.com/acme/checkout", "-o", "json"},
			calls: []string{"POST projects"}, body: `{"name":"Checkout API","sourceRepoUrl":"https://git.example.com/acme/checkout"}`,
			json: `{"type":"gcx.mutation","schema_version":"1","action":"created","changed":true,
				"target":{"kind":"Project","name":"Checkout API","id":"18","namespace":"stacks-123"}}`},
		{name: "a description", args: []string{"projects", "create", "--name", "web", "--description", "Checkout service", "-o", "json"},
			calls: []string{"POST projects"}, body: `{"name":"web","description":"Checkout service"}`},
		{name: "text names the project and is quiet on stderr", args: []string{"projects", "create", "--name", "web", "-o", "text"},
			calls: []string{"POST projects"}, has: []string{"Created project Checkout API (ID 18)"}, hasNot: []string{"checkout-api-18"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "hint")
			}},
		{name: "agent mode gets the mutation result and no hints", setup: asAgent, args: []string{"projects", "create", "--name", "web"},
			calls: []string{"POST projects"},
			check: func(t *testing.T, out string, _ error, _ []etCall) {
				t.Helper()
				agent.SetFlag(false)
				assert.NotContains(t, f.stderr.String(), "make it the default")
				assert.JSONEq(t, `{"type":"gcx.mutation","schema_version":"1","action":"created","changed":true,"target":{"kind":"Project","name":"Checkout API","id":"18","namespace":"stacks-123"}}`, out)
			}},
		{name: "a taken name names the project that has it", exit: gcxerrors.ExitGeneralError, setup: taken, args: []string{"projects", "create", "--name", "checkout api", "-o", "json"},
			calls: []string{"POST projects", "GET projects"}, err: `a project named "checkout api" already exists (ID 314)`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "See it: gcx error-tracking projects get 314")
				assert.Contains(t, rendered(t, err), "Resource conflict")
			}},
		{name: "a conflict without a match keeps the API's text", setup: taken, args: []string{"projects", "create", "--name", "other", "-o", "json"},
			calls: []string{"POST projects", "GET projects"}, exit: gcxerrors.ExitGeneralError, err: "a project with this name already exists"},
		{name: "an empty name", args: []string{"projects", "create", "--name", " ", "-o", "json"}, exit: gcxerrors.ExitUsageError, err: "--name must not be empty"},
		{name: "a name over 255 characters", args: []string{"projects", "create", "--name", strings.Repeat("ü", 256), "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: "--name is 256 characters; use at most 255"},
		{name: "a repository that is not an http(s) URL", args: []string{"projects", "create", "--name", "web", "--repo-url", "notaurl", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `--repo-url "notaurl": use an http(s) URL`},
	})
}

func TestWritesAreSentOnce(t *testing.T) {
	f := newFakeET(t)
	f.reply("POST projects", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)
	f.reply("PATCH projects/314", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)
	f.reply("DELETE projects/314", http.StatusServiceUnavailable, `{"error":{"message":"try later"}}`)
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`)

	f.runCases(t, []cliCase{
		{name: "create", exit: gcxerrors.ExitGeneralError, args: []string{"projects", "create", "--name", "web", "-o", "json"},
			calls: []string{"POST projects"}, err: "slow down"},
		{name: "update", exit: gcxerrors.ExitGeneralError, args: []string{"projects", "update", "314", "--description", "web", "-o", "json"},
			calls: []string{"PATCH projects/314"}, err: "slow down"},
		{name: "delete", exit: gcxerrors.ExitGeneralError, args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, err: "try later"},
	})
}

func TestProjectsUpdate(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`)
	f.reply("PATCH projects/314", 200, `{"id":314,"name":"checkout","stats":{"dailyEvents":[]}}`)

	f.runCases(t, []cliCase{
		{name: "a rename by name shows the old and the new name", args: []string{"projects", "update", "Checkout API", "--name", "checkout", "--description", "", "-o", "json"},
			calls: []string{"GET projects", "PATCH projects/314"}, body: `{"name":"checkout","description":""}`,
			json: `{"type":"gcx.mutation","schema_version":"1","action":"updated","changed":true,
				"target":{"kind":"Project","name":"checkout","id":"314","namespace":"stacks-123"}}`},
		{name: "a rename by ID reads the old name first", args: []string{"projects", "update", "314", "--name", "checkout"},
			calls: []string{"GET projects/314", "PATCH projects/314"}, has: []string{"Renamed project 314: Checkout API → checkout"}},
		{name: "a stale name-id says what the project is called now", exit: gcxerrors.ExitGeneralError, args: []string{"projects", "update", "checkout-api-old-314", "--description", "x"},
			calls: []string{"GET projects"}, err: `no project is called "checkout-api-old-314"; project 314 is now "Checkout API"`,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "Use the ID: gcx error-tracking projects update 314")
			}},
		{name: "the repository can be set", args: []string{"projects", "update", "314", "--repo-url", "https://git.example.com/acme/web"},
			calls: []string{"PATCH projects/314"}, body: `{"sourceRepoUrl":"https://git.example.com/acme/web"}`,
			has: []string{"Changed the repository of project checkout (ID 314)"}},
		{name: "an empty repository clears it", args: []string{"projects", "update", "314", "--repo-url", "", "-o", "json"},
			calls: []string{"PATCH projects/314"}, body: `{"sourceRepoUrl":""}`},
		{name: "no flags", args: []string{"projects", "update", "314", "-o", "json"}, exit: gcxerrors.ExitUsageError},
		{name: "an empty name", args: []string{"projects", "update", "314", "--name", " ", "-o", "json"}, exit: gcxerrors.ExitUsageError, err: "--name must not be empty"},
		{name: "a bad repository URL", args: []string{"projects", "update", "314", "--repo-url", "ftp://x", "-o", "json"}, exit: gcxerrors.ExitUsageError, err: `--repo-url "ftp://x": use an http(s) URL`},
	})
}

func TestProjectsDelete(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`)
	f.reply("DELETE projects/314", 200, `{"status":"deleted"}`)
	t.Setenv("GCX_AUTO_APPROVE", "")
	os.Unsetenv("GCX_AUTO_APPROVE")
	t.Cleanup(agent.ResetForTesting)
	redirected := 0
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected++ }))
	t.Cleanup(elsewhere.Close)

	human := func(*fakeET) { agent.SetFlag(false) }
	f.runCases(t, []cliCase{
		{name: "--force by ID reads the name, then deletes", setup: human, args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, json: `{"type":"gcx.mutation","schema_version":"1","action":"deleted","changed":true,
				"target":{"kind":"Project","name":"Checkout API","id":"314","namespace":"stacks-123"}}`},
		{name: "--force by name needs no extra read", setup: func(f *fakeET) { f.reply("GET projects", 200, twoProjects) }, args: []string{"projects", "delete", "Checkout API", "--force"},
			calls: []string{"GET projects", "DELETE projects/314"}, has: []string{"Deleted project Checkout API (ID 314)"}},
		{name: "a confirmed prompt deletes", stdin: "y\n", args: []string{"projects", "delete", "314"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, has: []string{"Deleted project Checkout API (ID 314)"},
			stderr: "Delete Error Tracking project Checkout API (ID 314) with all its errors and events?"},
		{name: "a declined prompt exits 5 and deletes nothing", stdin: "n\n", args: []string{"projects", "delete", "314", "-o", "json"},
			calls: []string{"GET projects/314"}, exit: gcxerrors.ExitCancelled},
		{name: "closed stdin", args: []string{"projects", "delete", "314", "-o", "json"},
			calls: []string{"GET projects/314"}, exit: gcxerrors.ExitUsageError},
		{name: "agent mode refuses before any call", setup: asAgent, stdin: "y\n", args: []string{"projects", "delete", "314", "-o", "json"},
			exit: gcxerrors.ExitUsageError, check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "destructive operation in agent mode: use --force to proceed")
			}},
		{name: "a redirect is not followed", setup: func(f *fakeET) {
			f.reply("GET projects", 200, twoProjects)
			f.handle("DELETE projects/314", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.URL+"/projects/314", http.StatusFound)
			})
		}, args: []string{"projects", "delete", "Checkout API", "--force", "-o", "text"},
			calls: []string{"GET projects", "DELETE projects/314"}, exit: gcxerrors.ExitGeneralError, err: "redirects to", hasNot: []string{"Deleted"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Zero(t, redirected)
			}},
		{name: "deleting the saved default clears it and says so", setup: withConfig("314"), args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, has: []string{`"clearedDefault": true`},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				data, err := os.ReadFile(f.cfg)
				require.NoError(t, err)
				assert.NotContains(t, string(data), `"314"`)
			}},
		{name: "deleting the project saved as the default by name clears it", setup: withConfig("checkout api"), args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, has: []string{`"clearedDefault": true`}},
		{name: "deleting another project leaves the saved default", setup: withConfig("2"), args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, hasNot: []string{"clearedDefault"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				data, err := os.ReadFile(f.cfg)
				require.NoError(t, err)
				assert.Contains(t, string(data), `project: "2"`)
			}},
		{name: "with the env var set the default is not checked, and a warning says so", setup: withEnv("314"), args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, hasNot: []string{"clearedDefault"},
			stderr: projectEnv + " is set, so the saved default project was not checked"},
		{name: "an unreadable saved default is not cleared, and a warning says so", setup: func(f *fakeET) {
			f.handle("DELETE projects/314", func(w http.ResponseWriter, _ *http.Request) {
				assert.NoError(t, os.WriteFile(f.cfg, []byte("version: [\n"), 0o600))
				w.WriteHeader(http.StatusNoContent)
			})
		}, args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, hasNot: []string{"clearedDefault"},
			stderr: "could not read the saved default project, so it was not checked"},
		{name: "forbidden", setup: func(f *fakeET) {
			f.reply("DELETE projects/314", http.StatusForbidden, `{"error":{"code":"forbidden","message":"no"}}`)
		}, args: []string{"projects", "delete", "314", "--force", "-o", "json"},
			calls: []string{"GET projects/314", "DELETE projects/314"}, exit: gcxerrors.ExitAuthFailure},
	})
}

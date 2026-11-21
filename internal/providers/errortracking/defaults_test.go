package errortracking_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const projectEnv = "GRAFANA_PROVIDER_ERRORTRACKING_PROJECT"

func withConfig(project string) func(*fakeET) {
	return func(f *fakeET) { f.writeConfig("test-token", project) }
}

func withEnv(project string) func(*fakeET) {
	return func(f *fakeET) { f.setenv(projectEnv, project) }
}

func both(setups ...func(*fakeET)) func(*fakeET) {
	return func(f *fakeET) {
		for _, s := range setups {
			s(f)
		}
	}
}

func asAgent(f *fakeET) {
	agent.SetFlag(true)
	f.t.Cleanup(func() { agent.SetFlag(false) })
}

func onlyProject(f *fakeET) {
	f.reply("GET projects", 200, `{"projects":[{"id":7,"name":"solo","stats":{"dailyEvents":[]}}]}`)
}

func withArgv(t *testing.T, argv ...string) {
	t.Helper()
	saved := os.Args
	os.Args = argv
	t.Cleanup(func() { os.Args = saved })
}

func TestDefaultProject(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	f.reply("GET projects/314/errors", 200, oneError)
	f.reply("GET projects/2/errors", 200, oneError)
	f.reply("GET projects/7/errors", 200, oneError)
	f.reply("GET projects/999/errors", 404, `{"error":{"code":"not_found","message":"not found"}}`)
	f.reply("GET projects/314/errors/42", 200, errorFixture)
	f.reply("PATCH projects/314/errors/42", 200, `{"id":42,"status":"resolved","history":[]}`)
	list := []string{"errors", "list", "--json", "id"}
	silent := func(t *testing.T, _ string, _ error, _ []etCall) {
		t.Helper()
		assert.NotContains(t, f.stderr.String(), "default")
		assert.NotContains(t, f.stderr.String(), "project 314")
		assert.NotContains(t, f.stderr.String(), "project 2")
	}
	notSet := func(t *testing.T, _ string, err error, _ []etCall) {
		t.Helper()
		out := rendered(t, err)
		assert.Contains(t, out, "--project is not set")
		assert.Contains(t, out, "Pass it: gcx error-tracking errors list --project <id>")
		assert.Contains(t, out, "List them with gcx error-tracking projects list")
		assert.Contains(t, out, "Remember one for this stack: gcx error-tracking projects use <id>")
	}

	f.runCases(t, []cliCase{
		{name: "the env var gives the project quietly", setup: withEnv("314"), args: list,
			calls: []string{"GET projects/314/errors"}, check: silent},
		{name: "--project wins over the env var and the config", setup: both(withEnv("314"), withConfig("314")),
			args: []string{"errors", "list", "--project", "2", "--json", "id"}, calls: []string{"GET projects/2/errors"}, check: silent},
		{name: "an empty env var means not set", setup: withEnv(""), args: list,
			exit: gcxerrors.ExitUsageError, err: "--project is not set",
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, rendered(t, err), `default project ""`)
			}},
		{name: "a blank env var means not set", setup: withEnv("  "), args: list,
			exit: gcxerrors.ExitUsageError, err: "--project is not set"},
		{name: "a name in the env var is looked up", setup: withEnv("web"), args: list,
			calls: []string{"GET projects", "GET projects/2/errors"}, check: silent},
		{name: "a name in the env var that no project has", setup: withEnv("gone"), args: list,
			calls: []string{"GET projects"}, exit: gcxerrors.ExitGeneralError, err: `the default project "gone" (from ` + projectEnv + `) no longer exists`},
		{name: "the config gives the project quietly", setup: withConfig("314"), args: list,
			calls: []string{"GET projects/314/errors"}, check: silent},
		{name: "the saved default is found by its name-ID in any case", setup: withConfig("Checkout-API-314"), args: list,
			calls: []string{"GET projects", "GET projects/314/errors"}, check: silent},
		{name: "the env var wins over the config", setup: both(withConfig("314"), withEnv("2")), args: list,
			calls: []string{"GET projects/2/errors"}, check: silent},
		{name: "a default that no longer exists says so", setup: withConfig("999"), args: list,
			calls: []string{"GET projects/999/errors"}, exit: gcxerrors.ExitGeneralError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, gcxerrors.SummaryResourceNotFound)
				assert.Contains(t, out, "the default project 999 (from the stack's saved default) no longer exists")
				assert.Contains(t, out, "If "+projectEnv+" is set, unset it or set it to a project")
				assert.Contains(t, out, "Pick another: gcx error-tracking projects use <id>")
				assert.Contains(t, out, "Or pass one: gcx error-tracking errors list --project <id>")
				assert.Contains(t, out, "See what exists: gcx error-tracking projects list")
			}},
		{name: "a stack with one project is not picked for a person", setup: onlyProject, args: list,
			exit: gcxerrors.ExitUsageError, check: notSet},
		{name: "an agent never gets the only project picked for it", setup: both(onlyProject, asAgent), args: list,
			exit: gcxerrors.ExitUsageError, check: notSet},
		{name: "an agent takes the env var quietly", setup: both(onlyProject, withEnv("314"), asAgent), args: list,
			calls: []string{"GET projects/314/errors"}, check: silent},
		{name: "an agent takes the config quietly", setup: both(onlyProject, withConfig("314"), asAgent), args: list,
			calls: []string{"GET projects/314/errors"}, check: silent},
		{name: "a write command takes the default too", setup: withConfig("314"),
			args: []string{"errors", "update", "42", "--status", "resolved"}, calls: []string{"GET projects/314/errors/42", "PATCH projects/314/errors/42"}, check: silent},
		{name: "a cut list pins the default in its continuation", setup: both(withConfig("314"), func(f *fakeET) {
			withArgv(f.t, "gcx", "error-tracking", "errors", "list", "--limit", "1")
			f.reply("GET projects/314/errors", 200, `{"errors":[{"id":1},{"id":2}],"total":2}`)
		}), args: []string{"errors", "list", "--limit", "1"}, calls: []string{"GET projects/314/errors"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Contains(t, f.stderr.String(), cmdio.BuildListLimitCommand([]string{"gcx", "error-tracking", "errors", "list", "--project", "314"}, 2)+"\n")
				assert.NotContains(t, f.stderr.String(), "-test.", "the continuation is the command's own, not the process's arguments")
			}},
		{name: "no default is a usage error without a lookup", args: list, exit: gcxerrors.ExitUsageError, check: notSet},
		{name: "projects list says nothing about the default", setup: withConfig("314"), args: []string{"projects", "list"},
			calls: []string{"GET projects"}, check: silent},
		{name: "projects use help says which JSON each form gives", args: []string{"projects", "use", "--help"},
			has: []string{"With no\nargument the JSON is the default's own shape"}},
		{name: "an empty --step is refused before any request", args: []string{"errors", "timeline", "42", "--project", "314", "--step", ""},
			exit: gcxerrors.ExitUsageError, err: "--step must not be empty"},
		{name: "projects timeline too", args: []string{"projects", "timeline", "314", "--step", " "},
			exit: gcxerrors.ExitUsageError, err: "--step must not be empty"},
	})
}

func TestPositionalProjectTakesNoDefault(t *testing.T) {
	f := newFakeET(t)
	leaves := [][]string{
		{"projects", "get"}, {"projects", "update", "--name", "x"}, {"projects", "delete", "--force"}, {"projects", "timeline"},
		{"projects", "ingest-keys", "rotate", "--force"}, {"projects", "ingest-keys", "delete-previous", "--force"},
	}
	cases := make([]cliCase, 0, len(leaves))
	for _, leaf := range leaves {
		cases = append(cases, cliCase{name: strings.Join(leaf, " "), setup: both(withConfig("314"), withEnv("314")), args: leaf,
			exit: gcxerrors.ExitUsageError, err: "accepts 1 arg(s), received 0"})
	}
	f.runCases(t, cases)
}

func TestProjectsUse(t *testing.T) {
	f := newFakeET(t)
	f.reply("GET projects", 200, twoProjects)
	f.reply("GET projects/314", 200, `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`)
	f.reply("GET projects/2", 200, `{"id":2,"name":"web","stats":{"dailyEvents":[]}}`)
	f.reply("GET projects/9", 404, `{"error":{"code":"not_found","message":"not found"}}`)
	saved := func(want string) func(t *testing.T, _ string, _ error, _ []etCall) {
		return func(t *testing.T, _ string, _ error, _ []etCall) {
			t.Helper()
			data, err := os.ReadFile(f.cfg)
			require.NoError(t, err)
			assert.Contains(t, string(data), "project: \""+want+"\"")
		}
	}
	const saved314 = "Default project for stack test: 314 (from the stack's saved default)"
	const fromEnv = "(from " + projectEnv + ")"

	f.runCases(t, []cliCase{
		{name: "no default yet", args: []string{"projects", "use"}, has: []string{"No default project for stack test"}},
		{name: "set by ID checks the project exists and saves its ID", args: []string{"projects", "use", "314"},
			calls: []string{"GET projects/314"}, has: []string{"Default project for stack test: 314 (Checkout API) (from the stack's saved default)"},
			check: saved("314")},
		{name: "show reads it back", setup: withConfig("314"), args: []string{"projects", "use", "-o", "json"},
			json: `{"stack":"test","project":{"id":314},"source":"the stack's saved default"}`},
		{name: "show a stored name leaves the unknown ID out", setup: withConfig("web"), args: []string{"projects", "use", "-o", "json"},
			json: `{"stack":"test","project":{"name":"web"},"source":"the stack's saved default"}`},
		{name: "show a stored negative value fails as --project does", setup: withConfig("-5"), args: []string{"projects", "use", "-o", "json"},
			exit: gcxerrors.ExitGeneralError, err: `the default project "-5" (from the stack's saved default) is not a project ID or name`},
		{name: "show a stored ID too large to be a project ID fails the same way", setup: withConfig("9223372036854775807"), args: []string{"projects", "use", "-o", "json"},
			exit: gcxerrors.ExitGeneralError, err: `the default project "9223372036854775807" (from the stack's saved default) is not a project ID or name`},
		{name: "a stored ID past int64 fails the same way", setup: withEnv("9223372036854775808"), args: []string{"projects", "use"},
			exit: gcxerrors.ExitGeneralError, err: `the default project "9223372036854775808"`},
		{name: "a command using a too-large stored default fails before any request", setup: withConfig("9223372036854775807"), args: []string{"errors", "list"},
			exit: gcxerrors.ExitGeneralError, err: `the default project "9223372036854775807" (from the stack's saved default) is not a project ID or name`},
		{name: "projects use refuses an ID too large and saves nothing", setup: withConfig("2"), args: []string{"projects", "use", "9223372036854775807"},
			exit: gcxerrors.ExitUsageError, err: "<project> must be a positive project ID up to 9007199254740991 or a project name", check: saved("2")},
		{name: "--project refuses it too", args: []string{"errors", "list", "--project", "9223372036854775808"},
			exit: gcxerrors.ExitUsageError, err: "--project must be a positive project ID up to 9007199254740991 or a project name"},
		{name: "show as text", setup: withConfig("314"), args: []string{"projects", "use"}, has: []string{saved314}},
		{name: "show names the env var when it is the source", setup: withEnv("314"), args: []string{"projects", "use"},
			has: []string{"Default project for stack test: 314 " + fromEnv}},
		{name: "set by name saves the ID, as JSON", args: []string{"projects", "use", "web", "-o", "json"}, calls: []string{"GET projects"},
			json:  `{"type":"gcx.mutation","schema_version":"1","action":"use","target":{"kind":"Project","id":"2"},"project":{"id":2,"name":"web"},"stack":"test","source":"the stack's saved default"}`,
			check: saved("2")},
		{name: "setting the default it already has succeeds", setup: withConfig("314"), args: []string{"projects", "use", "314", "-o", "json"},
			calls: []string{"GET projects/314"}, has: []string{`"action": "use"`}, hasNot: []string{`"changed"`}, check: saved("314")},
		{name: "a project that does not exist is not saved", setup: withConfig("2"), args: []string{"projects", "use", "9"},
			calls: []string{"GET projects/9"}, exit: gcxerrors.ExitGeneralError, err: "Error Tracking has no project 9", check: saved("2")},
		{name: "an env var that overrides the saved default is a success with a warning", setup: withEnv("5"), args: []string{"projects", "use", "314"},
			calls: []string{"GET projects/314"}, has: []string{"Default project for stack test: 5 " + fromEnv},
			stderr: projectEnv + "=5 wins over the stack's saved default, so commands here use project 5; unset it to use what projects use saves",
			check:  saved("314")},
		{name: "the override warning is a JSON line in agent mode", setup: both(withEnv("5"), asAgent), args: []string{"projects", "use", "314"},
			calls:  []string{"GET projects/314"},
			stderr: `{"class":"warning","summary":"` + projectEnv + `=5 wins over the stack's saved default, so commands here use project 5; unset it to use what projects use saves"}`},
		{name: "an env var that overrides a cleared default is a success with a warning", setup: both(withConfig("314"), withEnv("5")), args: []string{"projects", "use", "--unset"},
			has: []string{"Default project for stack test: 5 " + fromEnv}, stderr: projectEnv + "=5 wins over the stack's saved default, so commands here use project 5", check: saved("")},
		{name: "a default that reads back empty was not saved", setup: withEnv(""), args: []string{"projects", "use", "314"},
			calls: []string{"GET projects/314"}, exit: gcxerrors.ExitGeneralError,
			err: `the default project reads as empty after saving "314": no gcx config file took it, or ` + projectEnv + ` is set to empty in this environment`},
		{name: "--unset clears it", setup: withConfig("314"), args: []string{"projects", "use", "--unset"},
			has: []string{"No default project for stack test"}, check: saved("")},
		{name: "--unset with no default succeeds", args: []string{"projects", "use", "--unset", "-o", "json"},
			has: []string{`"action": "unset"`}, hasNot: []string{`"changed"`}},
		{name: "--unset takes no project", args: []string{"projects", "use", "314", "--unset"}, exit: gcxerrors.ExitUsageError, err: "--unset takes no <project>"},
		{name: "two projects", args: []string{"projects", "use", "314", "2"}, exit: gcxerrors.ExitUsageError, err: "accepts at most 1 arg(s), received 2"},
		{name: "with no config file, gcx creates one and saves there", setup: func(f *fakeET) {
			f.setenv("GRAFANA_SERVER", f.url)
			f.setenv("GRAFANA_TOKEN", "test-token")
		}, bare: true, args: []string{"projects", "use", "314"}, calls: []string{"GET projects/314"},
			has: []string{"Default project for stack default: 314 (Checkout API)"},
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				home, err := os.UserHomeDir()
				require.NoError(t, err)
				path := filepath.Join(home, ".config", "gcx", "config.yaml")
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Contains(t, string(data), "project: \"314\"")
				require.NoError(t, os.Remove(path))
			}},
	})
}

func TestTruncationContinuationRunsAsPrinted(t *testing.T) {
	f := newFakeET(t)
	f.setenv("GRAFANA_SERVER", f.url)
	f.setenv("GRAFANA_TOKEN", "test-token")
	f.reply("GET projects/314/errors", 200, `{"errors":[{"id":1},{"id":2}],"total":2}`)
	typed := []string{"gcx", "error-tracking", "errors", "list", "--project", "314", "--search", "x", "--limit", "1", "-o", "json"}
	withArgv(t, typed...)
	out, err := f.runArgs("", true, typed[2:]...)
	require.NoError(t, err)
	first := f.takeCalls()
	var page struct {
		ListMeta struct {
			Continue string `json:"continue"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &page))
	argv := strings.Fields(page.ListMeta.Continue)
	require.Greater(t, len(argv), 2, page.ListMeta.Continue)
	assert.Equal(t, []string{"gcx", "error-tracking"}, argv[:2])
	assert.Equal(t, []string{"--limit", "2"}, argv[len(argv)-2:])
	assert.Contains(t, argv, "--search")
	assert.NotContains(t, page.ListMeta.Continue, "-test.")

	out, err = f.runArgs("", true, argv[2:]...)
	require.NoError(t, err)
	again := f.takeCalls()
	require.NotEmpty(t, again)
	assert.Equal(t, first[0].Query.Get("search"), again[0].Query.Get("search"))
	assert.Equal(t, "x", again[0].Query.Get("search"))
	assert.NotContains(t, out, "list_meta", "the continuation reads the whole list")
}

func TestProjectsUseUnsetWithoutAServer(t *testing.T) {
	f := newFakeET(t)
	noServer := func(f *fakeET) {
		require.NoError(t, os.WriteFile(f.cfg, []byte(`version: 1
current-context: test
contexts:
  test:
    stack: test
stacks:
  test:
    providers:
      errortracking:
        project: "314"
`), 0o600))
	}
	unset := `{"type":"gcx.mutation","schema_version":"1","action":"unset","target":{"kind":"Project"},"stack":"test"}`
	f.runCases(t, []cliCase{
		{name: "--unset clears the default of a context that has no server", setup: noServer, args: []string{"projects", "use", "--unset", "-o", "json"},
			json: unset,
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				data, err := os.ReadFile(f.cfg)
				require.NoError(t, err)
				assert.NotContains(t, string(data), `"314"`)
			}},
		{name: "--unset with a server gives the same result", setup: withConfig("314"), args: []string{"projects", "use", "--unset", "-o", "json"},
			json: unset},
		{name: "and says so in text", setup: noServer, args: []string{"projects", "use", "--unset"},
			has: []string{"No default project for stack test"}},
		{name: "while setting one still needs the server", setup: noServer, args: []string{"projects", "use", "314"},
			exit: gcxerrors.ExitGeneralError},
	})
}

func TestNameHelpSaysHowNamesMatch(t *testing.T) {
	f := newFakeET(t)
	for _, args := range [][]string{
		{"errors", "list", "--help"},
		{"projects", "get", "--help"},
		{"heatmaps", "get", "--help"},
		{"heatmaps", "create", "--help"},
		{"heatmaps", "delete", "--help"},
	} {
		out, err := f.run(args...)
		require.NoError(t, err)
		assert.Contains(t, strings.Join(strings.Fields(out), " "), "name (case-insensitive) or name-ID", strings.Join(args, " "))
		assert.NotContains(t, out, "exact name", strings.Join(args, " "))
	}
}

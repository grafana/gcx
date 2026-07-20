package config_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// isolatedConfigEnv points HOME and XDG_CONFIG_HOME at empty temp dirs and
// chdirs into a working directory, so layered config discovery only sees what
// the test writes. Returns the user-config directory and the working directory.
func isolatedConfigEnv(t *testing.T) (string, string) {
	t.Helper()
	userDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("GCX_CONFIG", "")
	t.Chdir(workDir)
	return userDir, workDir
}

// writeLocalConfig creates a `.gcx.yaml` in workDir with the given content and
// returns its path.
func writeLocalConfig(t *testing.T, workDir, content string) string {
	t.Helper()
	path := filepath.Join(workDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// writeUserConfig creates the user config file (XDG_CONFIG_HOME/gcx/config.yaml)
// with the given content and returns its path.
func writeUserConfig(t *testing.T, userDir, content string) string {
	t.Helper()
	path := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// runConfigCmd executes a `gcx config ...` invocation against a fresh command
// tree and returns the combined stdout/stderr plus error.
func runConfigCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := config.Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// isolateStateHome points the XDG state home at a per-test tempdir so
// use-context invocations don't pollute the developer's real previous-context
// state file. It returns the directory so callers driving commands through the
// testutils harness (which calls os.Clearenv) can re-inject it via Env — a bare
// t.Setenv does not survive that wipe.
func isolateStateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

func Test_CurrentContextCommand(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"current-context", "--config", "testdata/config.yaml"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains("local"),
		},
	}

	testCase.Run(t)
}

func Test_UseContextCommand(t *testing.T) {
	stateEnv := map[string]string{"XDG_STATE_HOME": isolateStateHome(t)}

	cfg := `current-context: old
contexts:
  old: {}
  new: {}`

	configFile := testutils.CreateTempFile(t, cfg)

	initialConfigTest := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"current-context", "--config", configFile},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains("old"),
		},
	}
	initialConfigTest.Run(t)

	changeConfigTest := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "new"},
		Env:     stateEnv,
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains("Context set to \"new\""),
		},
	}
	changeConfigTest.Run(t)

	newConfigTest := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"current-context", "--config", configFile},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains("new"),
		},
	}
	newConfigTest.Run(t)
}

func Test_UseContextCommand_doesNotPersistEnvSecrets(t *testing.T) {
	stateDir := isolateStateHome(t)

	cfg := `current-context: old
contexts:
  old: {}
  new: {}`

	for _, tc := range []struct {
		name   string
		env    map[string]string
		secret string
	}{
		{
			name:   "GRAFANA_TOKEN",
			env:    map[string]string{"GRAFANA_TOKEN": "secret-from-env"},
			secret: "secret-from-env",
		},
		{
			name:   "GRAFANA_PASSWORD",
			env:    map[string]string{"GRAFANA_PASSWORD": "pass-from-env"},
			secret: "pass-from-env",
		},
		{
			name:   "GRAFANA_PROVIDER_SLO_TOKEN",
			env:    map[string]string{"GRAFANA_PROVIDER_SLO_TOKEN": "slo-secret-from-env"},
			secret: "slo-secret-from-env",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configFile := testutils.CreateTempFile(t, cfg)

			env := map[string]string{"XDG_STATE_HOME": stateDir}
			maps.Copy(env, tc.env)

			testutils.CommandTestCase{
				Cmd:     config.Command(),
				Command: []string{"use-context", "--config", configFile, "new"},
				Assertions: []testutils.CommandAssertion{
					testutils.CommandSuccess(),
				},
				Env: env,
			}.Run(t)

			contents, err := os.ReadFile(configFile)
			if err != nil {
				t.Fatalf("reading config file: %v", err)
			}
			if strings.Contains(string(contents), tc.secret) {
				t.Errorf("env secret %q leaked into config file:\n%s", tc.secret, contents)
			}
			if !strings.Contains(string(contents), "current-context: new") {
				t.Errorf("expected current-context to be updated; got:\n%s", contents)
			}
		})
	}
}

func Test_UseContextCommand_withUnknownContext(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", "testdata/config.yaml", "unknown-context"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("invalid context \"unknown-context\": context not found"),
		},
	}
	testCase.Run(t)
}

// Test_UseContextCommand_noArgsWithoutTTY asserts the picker degrades to a
// helpful, structured error when there is no terminal to drive it. The test
// harness runs commands with a non-TTY stdout, so the no-args path lands here
// rather than blocking on an interactive prompt.
func Test_UseContextCommand_noArgsWithoutTTY(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", "testdata/config.yaml"},
		Env:     map[string]string{"XDG_STATE_HOME": isolateStateHome(t)},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("interactive picker requires a TTY"),
			testutils.CommandErrorContains("Pass a context name"),
		},
	}
	testCase.Run(t)
}

// Test_UseContextCommand_previousWithoutHistory asserts that "use-context -"
// with no recorded history fails with the guidance to switch at least once.
func Test_UseContextCommand_previousWithoutHistory(t *testing.T) {
	cfg := `current-context: old
contexts:
  old: {}
  new: {}`
	configFile := testutils.CreateTempFile(t, cfg)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "-"},
		Env:     map[string]string{"XDG_STATE_HOME": isolateStateHome(t)},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("no previous context recorded"),
		},
	}
	testCase.Run(t)
}

func Test_ViewCommand(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/config.yaml"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`version: 1
stacks:
  local:
    grafana:
      server: http://localhost:3000/
      token: "**REDACTED**"
  prod:
    grafana:
      server: https://grafana.example.com/
      token: "**REDACTED**"
contexts:
  local:
    stack: local
  prod:
    stack: prod
current-context: local`),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_raw(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/config.yaml", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`version: 1
stacks:
  local:
    grafana:
      server: http://localhost:3000/
      token: local_token
  prod:
    grafana:
      server: https://grafana.example.com/
      token: prod_token
contexts:
  local:
    stack: local
  prod:
    stack: prod
current-context: local`),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_minify(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/config.yaml", "--minify"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`version: 1
stacks:
  local:
    grafana:
      server: http://localhost:3000/
      token: "**REDACTED**"
contexts:
  local:
    stack: local
current-context: local`),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_minify_explicitContext(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/config.yaml", "--minify", "--context", "prod"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`version: 1
stacks:
  prod:
    grafana:
      server: https://grafana.example.com/
      token: "**REDACTED**"
contexts:
  prod:
    stack: prod
current-context: prod`),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_outputJson(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/config.yaml", "-o", "json"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`{
  "version": 1,
  "stacks": {
    "local": {
      "grafana": {
        "server": "http://localhost:3000/",
        "token": "**REDACTED**"
      }
    },
    "prod": {
      "grafana": {
        "server": "https://grafana.example.com/",
        "token": "**REDACTED**"
      }
    }
  },
  "contexts": {
    "local": {
      "stack": "local"
    },
    "prod": {
      "stack": "prod"
    }
  },
  "current-context": "local"
}`),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_failsWithNonExistentConfigFile(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "does-not-exist.yaml"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("no such file or directory"),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_failsWithUnknownContext(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/config.yaml", "--context", "unknown-context"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("invalid context \"unknown-context\": context not found"),
		},
	}
	testCase.Run(t)
}

func Test_SetCommand(t *testing.T) {
	cfg := `contexts:
  dev:
    stack: dev
current-context: dev`

	configFile := testutils.CreateTempFile(t, cfg)

	changeConfigTest := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"set", "--config", configFile, "stacks.dev.grafana.server", "https://grafana-dev.example"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
		},
	}
	changeConfigTest.Run(t)

	viewCmd := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`stacks:
  dev:
    grafana:
      server: https://grafana-dev.example
contexts:
  dev:
    stack: dev
current-context: dev`),
		},
	}
	viewCmd.Run(t)
}

func Test_SetCommand_barePathResolvesAgainstCurrentContext(t *testing.T) {
	cfg := `contexts:
  dev:
    stack: dev
current-context: dev`

	configFile := testutils.CreateTempFile(t, cfg)

	// Stack-owned bare paths resolve through the current context's stack.
	setGrafanaServer := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"set", "--config", configFile, "grafana.server", "https://grafana-dev.example"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
		},
	}
	setGrafanaServer.Run(t)

	// Context-owned bare paths resolve against the current context.
	setDatasource := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"set", "--config", configFile, "datasources.prometheus", "prom-uid"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
		},
	}
	setDatasource.Run(t)

	viewCmd := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`stacks:
  dev:
    grafana:
      server: https://grafana-dev.example`),
			testutils.CommandOutputContains(`    datasources:
      prometheus: prom-uid`),
		},
	}
	viewCmd.Run(t)
}

func Test_SetCommand_barePathWithoutCurrentContextErrors(t *testing.T) {
	configFile := testutils.CreateTempFile(t, `contexts: {}`)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"set", "--config", configFile, "datasources.prometheus", "prom-uid"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("no current context set"),
		},
	}
	testCase.Run(t)
}

func Test_SetCommand_legacyCloudPathErrors(t *testing.T) {
	configFile := testutils.CreateTempFile(t, `current-context: dev
contexts:
  dev: {}`)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"set", "--config", configFile, "cloud.token", "glc_abc123"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("cloud credentials now live in named entries; use cloud.<entry>.token"),
		},
	}
	testCase.Run(t)
}

func Test_UnsetCommand(t *testing.T) {
	cfg := `stacks:
  dev:
    grafana:
      server: https://grafana-dev.example
      user: remove-me-please
contexts:
  dev:
    stack: dev
current-context: dev`

	configFile := testutils.CreateTempFile(t, cfg)

	changeConfigTest := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"unset", "--config", configFile, "stacks.dev.grafana.user"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
		},
	}
	changeConfigTest.Run(t)

	viewCmd := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`stacks:
  dev:
    grafana:
      server: https://grafana-dev.example
contexts:
  dev:
    stack: dev
current-context: dev`),
		},
	}
	viewCmd.Run(t)
}

func Test_ViewCommand_withEnvironmentVariables(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", "testdata/partial-config.yaml", "--minify", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputEquals(`version: 1
stacks:
  prod:
    grafana:
      server: https://grafana.example.com/
      token: token
      org-id: 42
contexts:
  prod:
    stack: prod
current-context: prod
`),
		},
		Env: map[string]string{
			"GRAFANA_TOKEN": "token",
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_withEnvVar(t *testing.T) {
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--minify"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains("local"),
			testutils.CommandOutputContains("http://localhost:3000/"),
		},
		Env: map[string]string{
			"GCX_CONFIG": "testdata/config.yaml",
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_redactsProviderSecrets(t *testing.T) {
	cfg := `version: 1
stacks:
  default:
    grafana:
      server: https://grafana.example.com/
      token: grafana-token
    providers:
      slo:
        token: slo-secret-token
contexts:
  default:
    stack: default
current-context: default`

	configFile := testutils.CreateTempFile(t, cfg)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`    providers:
      slo:
        token: "**REDACTED**"`),
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_rawShowsProviderSecrets(t *testing.T) {
	cfg := `version: 1
stacks:
  default:
    grafana:
      server: https://grafana.example.com/
      token: grafana-token
    providers:
      slo:
        token: slo-secret-token
contexts:
  default:
    stack: default
current-context: default`

	configFile := testutils.CreateTempFile(t, cfg)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`    providers:
      slo:
        token: slo-secret-token`),
		},
	}

	testCase.Run(t)
}

// stackBackedProviderConfig gives the current context a stack whose providers
// map exists: env-derived provider config lands in the context's resolved
// Providers map, which view only renders when it is shared with a stack entry.
const stackBackedProviderConfig = `version: 1
stacks:
  default:
    providers: {}
contexts:
  default:
    stack: default
current-context: default`

func Test_ViewCommand_withProviderEnvVar(t *testing.T) {
	configFile := testutils.CreateTempFile(t, stackBackedProviderConfig)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`    providers:
      slo:
        token: my-secret-token`),
		},
		Env: map[string]string{
			"GRAFANA_SERVER":             "https://grafana.example.com/",
			"GRAFANA_PROVIDER_SLO_TOKEN": "my-secret-token",
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_withProviderEnvVar_underscoreToDash(t *testing.T) {
	configFile := testutils.CreateTempFile(t, stackBackedProviderConfig)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`    providers:
      slo:
        org-id: "42"`),
		},
		Env: map[string]string{
			"GRAFANA_SERVER":              "https://grafana.example.com/",
			"GRAFANA_PROVIDER_SLO_ORG_ID": "42",
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_withProviderEnvVar_redacted(t *testing.T) {
	configFile := testutils.CreateTempFile(t, stackBackedProviderConfig)

	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`    providers:
      slo:
        token: "**REDACTED**"`),
		},
		Env: map[string]string{
			"GRAFANA_SERVER":             "https://grafana.example.com/",
			"GRAFANA_PROVIDER_SLO_TOKEN": "my-secret-token",
		},
	}

	testCase.Run(t)
}

func Test_ViewCommand_withEnvironmentVariablesAndEmptyConfig(t *testing.T) {
	configFile := testutils.CreateTempFile(t, "contexts:")

	// Env vars synthesize a default context, but its env-derived grafana config
	// lives on the context's resolved (non-serialized) view, so view renders the
	// bare context — env secrets never appear in the output.
	testCase := testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"view", "--config", configFile, "--minify", "--raw"},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputEquals(`contexts:
  default: {}
current-context: default
`),
		},
		Env: map[string]string{
			"GRAFANA_SERVER": "https://grafana.example.com/",
			"GRAFANA_TOKEN":  "token",
		},
	}

	testCase.Run(t)
}

// Regression test for #564: when only a local .gcx.yaml exists, use-context
// must update that file instead of silently creating/writing the user config.
func Test_UseContextCommand_writesToLocalConfigWhenOnlySource(t *testing.T) {
	isolateStateHome(t)
	_, workDir := isolatedConfigEnv(t)
	localPath := writeLocalConfig(t, workDir, `current-context: old
contexts:
  old: {}
  new: {}
`)

	out, err := runConfigCmd(t, "use-context", "new")
	require.NoError(t, err, out)
	require.Contains(t, out, `Context set to "new"`)

	contents, err := os.ReadFile(localPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), "current-context: new")

	userPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "gcx", "config.yaml")
	_, statErr := os.Stat(userPath)
	require.True(t, os.IsNotExist(statErr), "user config must not be created, got: %v", statErr)
}

// When both user and local configs exist, use-context cannot guess which to
// update, so it errors with guidance pointing at --file. This matches the
// behaviour of `gcx config set` / `unset`.
func Test_UseContextCommand_multipleSourcesRequiresFileFlag(t *testing.T) {
	isolateStateHome(t)
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `current-context: user-ctx
contexts:
  user-ctx: {}
  new: {}
`)
	localPath := writeLocalConfig(t, workDir, `current-context: local-ctx
contexts:
  local-ctx: {}
  new: {}
`)

	out, err := runConfigCmd(t, "use-context", "new")
	require.Error(t, err, out)
	require.Contains(t, err.Error(), "--file")

	for _, p := range []string{userPath, localPath} {
		contents, readErr := os.ReadFile(p)
		require.NoError(t, readErr)
		require.NotContains(t, string(contents), "current-context: new",
			"file %s must not be modified", p)
	}
}

// --file selects the target layer explicitly when multiple sources exist.
func Test_UseContextCommand_fileFlagSelectsLayer(t *testing.T) {
	isolateStateHome(t)
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `current-context: user-ctx
contexts:
  user-ctx: {}
  new: {}
`)
	localPath := writeLocalConfig(t, workDir, `current-context: local-ctx
contexts:
  local-ctx: {}
  new: {}
`)

	out, err := runConfigCmd(t, "use-context", "--file", "local", "new")
	require.NoError(t, err, out)

	localContents, err := os.ReadFile(localPath)
	require.NoError(t, err)
	require.Contains(t, string(localContents), "current-context: new")
	require.NotContains(t, string(localContents), "user-ctx",
		"local config must not absorb user-layer contexts")

	userContents, err := os.ReadFile(userPath)
	require.NoError(t, err)
	require.Contains(t, string(userContents), "current-context: user-ctx",
		"user config must be untouched when --file local is given")
	require.NotContains(t, string(userContents), "local-ctx",
		"user config must not absorb local-layer contexts")
}

// Regression test for the same latent bug in `gcx config set`: with only a
// local .gcx.yaml, set must write to that file rather than fabricating a user
// config.
func Test_SetCommand_writesToLocalConfigWhenOnlySource(t *testing.T) {
	_, workDir := isolatedConfigEnv(t)
	localPath := writeLocalConfig(t, workDir, `current-context: dev
contexts:
  dev: {}
`)

	_, err := runConfigCmd(t, "set", "stacks.dev.grafana.server", "https://example.test")
	require.NoError(t, err)

	contents, err := os.ReadFile(localPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), "server: https://example.test")

	userPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "gcx", "config.yaml")
	_, statErr := os.Stat(userPath)
	require.True(t, os.IsNotExist(statErr), "user config must not be created, got: %v", statErr)
}

func Test_UseContextCommand_PreviousSwitch(t *testing.T) {
	stateDir := isolateStateHome(t)
	stateEnv := map[string]string{"XDG_STATE_HOME": stateDir}

	cfg := `current-context: a
contexts:
  a: {}
  b: {}`

	configFile := testutils.CreateTempFile(t, cfg)

	// a → b records "a" as previous.
	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "b"},
		Env:     stateEnv,
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`Context set to "b"`),
		},
	}.Run(t)

	// "-" resolves to the previously recorded "a".
	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "-"},
		Env:     stateEnv,
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`Context set to "a"`),
		},
	}.Run(t)

	// And another "-" bounces back to "b".
	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "-"},
		Env:     stateEnv,
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`Context set to "b"`),
		},
	}.Run(t)
}

func Test_UseContextCommand_PreviousErrorsWhenNoneRecorded(t *testing.T) {
	stateDir := isolateStateHome(t)

	cfg := `current-context: a
contexts:
  a: {}
  b: {}`
	configFile := testutils.CreateTempFile(t, cfg)

	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "-"},
		Env:     map[string]string{"XDG_STATE_HOME": stateDir},
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("no previous context recorded"),
		},
	}.Run(t)
}

func Test_UseContextCommand_SameContextIsNoop(t *testing.T) {
	stateDir := isolateStateHome(t)
	stateEnv := map[string]string{"XDG_STATE_HOME": stateDir}

	cfg := `current-context: a
contexts:
  a: {}
  b: {}`
	configFile := testutils.CreateTempFile(t, cfg)

	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "a"},
		Env:     stateEnv,
		Assertions: []testutils.CommandAssertion{
			testutils.CommandSuccess(),
			testutils.CommandOutputContains(`Context already set to "a"`),
		},
	}.Run(t)

	// A no-op switch must not record a phantom previous-context entry —
	// otherwise "gctx -" would silently bounce to the same context.
	testutils.CommandTestCase{
		Cmd:     config.Command(),
		Command: []string{"use-context", "--config", configFile, "-"},
		Env:     stateEnv,
		Assertions: []testutils.CommandAssertion{
			testutils.CommandErrorContains("no previous context recorded"),
		},
	}.Run(t)
}

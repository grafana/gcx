//nolint:testpackage // White-box: the browser flow and validation seams are unexported.
package login

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	internallogin "github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAuthFlow struct {
	run func(ctx context.Context) (*internalauth.Result, error)
}

func (f fakeAuthFlow) Run(ctx context.Context) (*internalauth.Result, error) { return f.run(ctx) }

// signupBrowser stands in for the browser step. It records every flow that
// login starts and answers with a connection to stack.
type signupBrowser struct {
	mu      sync.Mutex
	servers []string
	options []internalauth.Options
	stack   string
	// beforeReturn runs inside the browser step, for a test that changes the
	// world while the person is in the browser. An error it returns is the
	// browser step's own failure.
	beforeReturn func() error
}

func (b *signupBrowser) started() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.servers)
}

// stubSignupBrowser replaces the browser flow and connectivity validation. The
// stack is a local server that answers 404, so building the connection's REST
// config never reaches the network.
func stubSignupBrowser(t *testing.T, validate func() error) *signupBrowser {
	t.Helper()
	stack := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(stack.Close)

	browser := &signupBrowser{stack: stack.URL}
	previousFlow, previousValidate := newAuthFlow, validateConnection
	newAuthFlow = func(server string, ao internalauth.Options) internallogin.AuthFlow {
		browser.mu.Lock()
		browser.servers = append(browser.servers, server)
		browser.options = append(browser.options, ao)
		browser.mu.Unlock()
		return fakeAuthFlow{run: func(context.Context) (*internalauth.Result, error) {
			if browser.beforeReturn != nil {
				if err := browser.beforeReturn(); err != nil {
					return nil, err
				}
			}
			return &internalauth.Result{
				Token:            "gat_signup",
				RefreshToken:     "gar_signup",
				ExpiresAt:        "2030-01-01T00:00:00Z",
				RefreshExpiresAt: "2030-06-01T00:00:00Z",
				APIEndpoint:      stack.URL,
				InstanceEndpoint: stack.URL,
			}, nil
		}}
	}
	validateConnection = func(context.Context, internallogin.Options, config.NamespacedRESTConfig) (string, error) {
		if validate != nil {
			return "", validate()
		}
		return "12.0.0", nil
	}
	t.Cleanup(func() { newAuthFlow, validateConnection = previousFlow, previousValidate })
	return browser
}

// signupEnvironment clears the variables that would steer a login, keeps
// credentials in the config file, and sets agent mode (so the run cannot
// prompt) unless the caller turns it off.
func signupEnvironment(t *testing.T, agentMode string) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", agentMode)
	t.Setenv("GCX_KEYCHAIN", "off")
	for _, key := range []string{
		"GRAFANA_SERVER", "GRAFANA_TOKEN", "GRAFANA_CLOUD_TOKEN",
		"GRAFANA_CLOUD_API_URL", "GRAFANA_CLOUD_OAUTH_URL", "GRAFANA_STACK_SLUG",
	} {
		unsetEnvForTest(t, key)
	}
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

func runSignup(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stderr bytes.Buffer
	cmd := SignupCommand()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return stderr.String(), err
}

func writeSignupConfig(t *testing.T, seed config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), seed))
	return path
}

func TestSignupFlagsAreOnlyTheBrowserOnes(t *testing.T) {
	t.Parallel()

	flags := SignupCommand().Flags()
	for _, name := range []string{"oauth-callback-port", "oauth-manual", "config"} {
		assert.NotNil(t, flags.Lookup(name), name)
	}
	for _, name := range []string{
		"server", "token", "cloud-token", "cloud-api-url", "oauth", "basic-auth",
		"user", "cloud", "yes", "allow-server-override", "org-id",
	} {
		assert.Nil(t, flags.Lookup(name), "gcx signup must not accept --%s", name)
	}
}

// TestSignupSavesTheNewStack covers the whole route with the browser step
// stubbed: no prompt, the signup entry page, and a saved connection under the
// context name fixed before the browser opened.
func TestSignupSavesTheNewStack(t *testing.T) {
	signupEnvironment(t, "true")
	browser := stubSignupBrowser(t, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")

	stderr, err := runSignup(t, "--config", path)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, `gcx saves the new stack as context "default"`)

	require.Equal(t, 1, browser.started())
	assert.Empty(t, browser.servers[0], "signup starts without a stack endpoint")
	assert.True(t, browser.options[0].Signup)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	ctx := cfg.Contexts["default"]
	require.NotNil(t, ctx)
	require.NotNil(t, ctx.Grafana)
	assert.Equal(t, browser.stack, ctx.Grafana.Server)
	assert.Equal(t, "oauth", ctx.Grafana.AuthMethod)
	assert.Equal(t, "default", cfg.CurrentContext)
}

// asIfTerminal makes the command see a terminal on stdin, the case where a
// login would prompt.
func asIfTerminal(t *testing.T) {
	t.Helper()
	previous := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = previous })
}

// TestSignupRefusesBeforeTheBrowser pins that every refusal comes before the
// browser step. After it the account may exist, so a refusal then would cost
// the person the whole signup.
func TestSignupRefusesBeforeTheBrowser(t *testing.T) {
	existingStack := func(server string) *config.GrafanaConfig {
		return &config.GrafanaConfig{Server: server, OrgID: 1}
	}

	tests := []struct {
		name string
		seed func() config.Config
		args []string
		env  map[string]string
		want string
	}{
		{
			name: "the current context already has a stack",
			seed: func() config.Config {
				var cfg config.Config
				cfg.SetStack("prod", config.StackConfig{Grafana: existingStack("https://prod.grafana.net")})
				cfg.SetContext("prod", true, config.Context{Stack: "prod"})
				return cfg
			},
			want: `Context "prod" already exists and uses the stack entry "prod"`,
		},
		{
			name: "the named context is bound to a stack entry without a server",
			seed: func() config.Config {
				var cfg config.Config
				cfg.SetStack("leftover", config.StackConfig{Grafana: &config.GrafanaConfig{TLS: &config.TLS{CertFile: "/old/client.pem"}}})
				cfg.SetContext("fresh", false, config.Context{Stack: "leftover"})
				cfg.SetContext("other", true, config.Context{})
				return cfg
			},
			args: []string{"fresh"},
			want: `uses the stack entry "leftover"`,
		},
		{
			name: "the target context holds another account's Cloud entry",
			seed: func() config.Config {
				cfg := config.Config{Cloud: map[string]*config.CloudEntry{"work": {Token: "glc_work", APIUrl: "https://grafana.com"}}}
				cfg.SetContext("default", true, config.Context{Cloud: "work"})
				return cfg
			},
			want: `uses the Grafana Cloud entry "work"`,
		},
		{
			name: "a stack entry named after the new context exists, even without a server",
			seed: func() config.Config {
				var cfg config.Config
				cfg.SetStack("fresh", config.StackConfig{})
				cfg.SetContext("other", true, config.Context{})
				return cfg
			},
			args: []string{"fresh"},
			want: `A stack entry named "fresh" already exists`,
		},
		{
			name: "another context points at the stack entry the save would create",
			seed: func() config.Config {
				var cfg config.Config
				cfg.SetContext("old", true, config.Context{Stack: "fresh"})
				return cfg
			},
			args: []string{"fresh"},
			want: `context "old" already point at a stack entry named "fresh"`,
		},
		{
			name: "GRAFANA_SERVER names a server",
			seed: func() config.Config { return config.Config{} },
			env:  map[string]string{"GRAFANA_SERVER": "https://prod.grafana.net"},
			want: "GRAFANA_SERVER is set",
		},
		{
			name: "GRAFANA_PROXY_ENDPOINT is set, even empty",
			seed: func() config.Config { return config.Config{} },
			env:  map[string]string{"GRAFANA_PROXY_ENDPOINT": ""},
			want: "GRAFANA_PROXY_ENDPOINT set how gcx reaches an existing Grafana server",
		},
		{
			name: "a GRAFANA_TLS variable is set",
			seed: func() config.Config { return config.Config{} },
			env:  map[string]string{"GRAFANA_TLS_CERT_FILE": "/tmp/client.pem"},
			want: "GRAFANA_TLS_CERT_FILE set how gcx reaches",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signupEnvironment(t, "true")
			for _, key := range []string{"GRAFANA_PROXY_ENDPOINT", "GRAFANA_TLS_CERT_FILE", "GRAFANA_TLS_KEY_FILE", "GRAFANA_TLS_CA_FILE"} {
				unsetEnvForTest(t, key)
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			browser := stubSignupBrowser(t, nil)
			path := writeSignupConfig(t, tc.seed())
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			_, err = runSignup(t, append(tc.args, "--config", path)...)
			var det gcxerrors.DetailedError
			require.ErrorAs(t, err, &det)
			assert.Contains(t, det.Details, tc.want)
			assert.Equal(t, "Invalid command usage", det.Summary)
			assert.Zero(t, browser.started(), "the refusal must come before the browser step")

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "a refused signup must not write the config")
		})
	}
}

func TestSignupAcceptsAnEmptyExistingContext(t *testing.T) {
	signupEnvironment(t, "true")
	browser := stubSignupBrowser(t, nil)
	var seed config.Config
	seed.SetContext("default", true, config.Context{})
	path := writeSignupConfig(t, seed)

	stderr, err := runSignup(t, "--config", path)
	require.NoError(t, err, stderr)
	require.Equal(t, 1, browser.started())
	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	require.NotNil(t, cfg.Contexts["default"].Grafana)
	assert.Equal(t, browser.stack, cfg.Contexts["default"].Grafana.Server)
}

func TestSignupRefusesAnAutoDiscoveredRepositoryConfig(t *testing.T) {
	signupEnvironment(t, "true")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	workDir := t.TempDir()
	t.Chdir(workDir)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".gcx.yaml"), []byte("contexts: {}\n"), 0o600))
	browser := stubSignupBrowser(t, nil)

	_, err := runSignup(t)
	require.ErrorContains(t, err, "auto-discovered repository config")
	assert.Zero(t, browser.started())
}

// TestSignupNeverSavesCloudCredentials pins that signup saves the stack
// connection only, from a script and from a terminal alike. A non-interactive
// login imports GRAFANA_CLOUD_TOKEN, but that token belongs to some other
// organization, never to the new account.
func TestSignupNeverSavesCloudCredentials(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "script", true: "terminal"}[terminal], func(t *testing.T) {
			signupEnvironment(t, "false")
			if terminal {
				asIfTerminal(t)
			}
			t.Setenv("GRAFANA_CLOUD_TOKEN", "glc_from_another_org")
			browser := stubSignupBrowser(t, nil)
			path := filepath.Join(t.TempDir(), "config.yaml")

			stderr, err := runSignup(t, "--config", path)
			require.NoError(t, err, stderr)
			require.Equal(t, 1, browser.started())
			assert.Contains(t, stderr, "Add one with: gcx cloud login --context default")

			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "glc_from_another_org")
			cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
			require.NoError(t, err)
			assert.Empty(t, cfg.Contexts["default"].Cloud, "signup must not bind a Cloud entry")
			assert.Empty(t, cfg.Cloud)
		})
	}
}

// TestSignupFailureAfterTheBrowserGivesTheLoginRecovery covers every error
// once the browser step has started. The account may exist by then, so the
// error keeps its own summary and exit code, adds how to finish with gcx login
// in the same context and config file, and never suggests signup again.
func TestSignupFailureAfterTheBrowserGivesTheLoginRecovery(t *testing.T) {
	healthFailure := func() error {
		return &internallogin.HealthCheckError{Server: "https://stack", Status: http.StatusServiceUnavailable, Cause: errors.New("503")}
	}
	tests := []struct {
		name         string
		agentMode    string
		terminal     bool
		validate     func() error
		beforeReturn func(path string) func() error
		// wantServer is whether the failure came after the browser step
		// finished, so the stack URL is known.
		wantServer   bool
		wantRecovery string
	}{
		{name: "validation failure in agent mode", agentMode: "true", validate: healthFailure, wantServer: true, wantRecovery: "Wait a few minutes"},
		{name: "validation failure from a script", agentMode: "false", validate: healthFailure, wantServer: true, wantRecovery: "Wait a few minutes"},
		{
			// In a terminal a login would ask "save the context anyway?".
			name: "validation failure in a terminal", agentMode: "false", terminal: true,
			validate: healthFailure, wantServer: true, wantRecovery: "Wait a few minutes",
		},
		{
			name: "the config changed while the person was in the browser", agentMode: "true",
			beforeReturn: func(path string) func() error {
				return func() error {
					var cfg config.Config
					cfg.SetContext("someone-else", true, config.Context{})
					return config.Write(context.Background(), config.ExplicitConfigFile(path), cfg)
				}
			},
			wantServer: true, wantRecovery: "Once the cause above is fixed",
		},
		{
			name: "Cancel on the Connect gcx page", agentMode: "true",
			beforeReturn: func(string) func() error {
				return func() error { return internalauth.ErrBrowserCancelled }
			},
			wantRecovery: "Sign in instead of signing up again",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signupEnvironment(t, tc.agentMode)
			if tc.terminal {
				asIfTerminal(t)
			}
			browser := stubSignupBrowser(t, tc.validate)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if tc.beforeReturn != nil {
				browser.beforeReturn = tc.beforeReturn(path)
			}

			stderr, err := runSignup(t, "my-stack", "--config", path)
			require.Error(t, err)
			assert.NotContains(t, stderr, "Save the context anyway")

			if tc.validate != nil {
				// The validation failure itself, not the "save anyway?"
				// question that a login would reach.
				var health *internallogin.HealthCheckError
				require.ErrorAs(t, err, &health)
			}

			var incomplete *internallogin.SignupIncompleteError
			require.ErrorAs(t, err, &incomplete)
			wantCommand := "gcx login my-stack --cloud --oauth --config " + path
			if tc.wantServer {
				assert.Equal(t, browser.stack, incomplete.Server)
				wantCommand = "gcx login my-stack --server " + browser.stack + " --oauth --config " + path
			}
			assert.Equal(t, wantCommand, incomplete.Recovery)

			inner := fail.ErrorToDetailedError(incomplete.Err)
			det := fail.ErrorToDetailedError(err)
			require.NotNil(t, inner)
			require.NotNil(t, det)
			assert.Equal(t, inner.Summary, det.Summary, "the failure keeps its own summary")
			assert.Equal(t, inner.ExitCode, det.ExitCode, "the failure keeps its own exit code")
			require.NotEmpty(t, det.Suggestions)
			assert.Contains(t, det.Suggestions[0], tc.wantRecovery)
			assert.True(t, strings.HasSuffix(det.Suggestions[0], wantCommand), det.Suggestions[0])
			assert.NotContains(t, strings.Join(det.Suggestions, "\n")+det.Details, "gcx signup")
		})
	}
}

// TestSignupOutputFailureIsNotAnUnfinishedSignup pins that an error from
// printing the result, after the connection is saved, is reported as itself.
func TestSignupOutputFailureIsNotAnUnfinishedSignup(t *testing.T) {
	signupEnvironment(t, "true")
	stubSignupBrowser(t, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")

	_, err := runSignup(t, "--config", path, "-o", "json", "--jq", ".contextName | tonumber")
	require.Error(t, err)
	var incomplete *internallogin.SignupIncompleteError
	assert.NotErrorAs(t, err, &incomplete)

	cfg, loadErr := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, loadErr)
	assert.NotNil(t, cfg.Contexts["default"], "the connection was saved before the output failed")
}

// TestSignupPassesTheManualRetryCommand pins the rerun that the remote
// session hint offers: a sign in to the same context and config file, never a
// second signup.
func TestSignupPassesTheManualRetryCommand(t *testing.T) {
	signupEnvironment(t, "true")
	browser := stubSignupBrowser(t, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")

	stderr, err := runSignup(t, "my stack", "--config", path)
	require.NoError(t, err, stderr)
	require.Equal(t, 1, browser.started())
	assert.Equal(t, "gcx login 'my stack' --cloud --oauth-manual --config "+path, browser.options[0].ManualCommand)
}

func TestSignupConflictSuggestionNamesSignup(t *testing.T) {
	t.Parallel()

	opts := &loginOpts{signup: true}
	opts.Config.Context = "other"
	err := opts.Validate([]string{"mine"})
	var det gcxerrors.DetailedError
	require.ErrorAs(t, err, &det)
	assert.Equal(t, []string{"Drop --context and use the positional form: gcx signup mine"}, det.Suggestions)
}

package login_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// launcherOAuthResult is what the consent page returns for a stack that the
// user picked (or created) in the browser.
func launcherOAuthResult() *auth.Result {
	return &auth.Result{
		Token:            "gat_test",
		RefreshToken:     "gar_test",
		ExpiresAt:        "2030-01-01T00:00:00Z",
		RefreshExpiresAt: "2030-06-01T00:00:00Z",
		APIEndpoint:      "https://mystack.grafana.net/api",
		InstanceEndpoint: "https://mystack.grafana.net",
	}
}

// agentModeOffForTest makes the optional Cloud step reachable: agent mode
// skips it, and the test suite may run under an agent.
func agentModeOffForTest(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

// TestRunPassesLauncherOptionsOnlyWithoutAStack pins what reaches the browser
// flow. Without a stack, the launcher follows the Cloud OAuth origin (so an
// environment override moves both together), signup opens the account
// creation page, and an interactive session gets the reopen shortcut. A stack
// login receives none of these.
func TestRunPassesLauncherOptionsOnlyWithoutAStack(t *testing.T) {
	agentModeOffForTest(t)
	usePlaintextCredentialStorage(t)

	tests := []struct {
		name       string
		inputs     login.Inputs
		wantServer string
		wantOpts   auth.Options
		wantNote   string
	}{
		{
			name:     "signup on a development portal",
			inputs:   login.Inputs{CloudSignup: true, Interactive: true, CloudOAuthURL: "https://grafana-dev.com/"},
			wantOpts: auth.Options{LaunchOrigin: "https://grafana-dev.com", Signup: true, ReopenOnEnter: true},
		},
		{
			name:     "sign in, non-interactive, production portal",
			inputs:   login.Inputs{UseCloudInstanceSelector: true, Yes: true, CloudOAuthURL: "https://grafana.com"},
			wantOpts: auth.Options{LaunchOrigin: "https://grafana.com"},
		},
		{
			name:     "no Cloud URL falls back to the production portal",
			inputs:   login.Inputs{UseCloudInstanceSelector: true, Yes: true},
			wantOpts: auth.Options{LaunchOrigin: "https://grafana.com"},
		},
		{
			// A lone API URL names the environment for both operations, as in
			// ResolveCloudEndpoints, so the launcher follows it too.
			name:     "API-only override moves the launcher",
			inputs:   login.Inputs{UseCloudInstanceSelector: true, Yes: true, CloudAPIURL: "https://grafana-dev.com"},
			wantOpts: auth.Options{LaunchOrigin: "https://grafana-dev.com"},
		},
		{
			// An API proxy cannot serve the launcher, so the login keeps the
			// production portal, as before the override moved the launcher.
			name:     "a Cloud URL that is no portal keeps the production launcher",
			inputs:   login.Inputs{UseCloudInstanceSelector: true, Yes: true, CloudAPIURL: "https://gcom-proxy.corp.example"},
			wantOpts: auth.Options{},
			wantNote: "gcom-proxy.corp.example is not a Grafana Cloud portal",
		},
		{
			name: "stack login",
			inputs: login.Inputs{
				Server: "https://mystack.grafana.net", Target: login.TargetCloud, UseOAuth: true,
				Interactive: true, Yes: true, CloudOAuthURL: "https://grafana-dev.com",
			},
			wantServer: "https://mystack.grafana.net",
			wantOpts:   auth.Options{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotServer string
			var got auth.Options
			var progress bytes.Buffer
			inputs := tc.inputs
			inputs.Writer = &progress
			opts := login.Options{
				Inputs: inputs,
				Hooks: login.Hooks{
					ConfigSource: configSource(t.TempDir()),
					ValidateFn:   noopValidate,
					NewAuthFlow: func(server string, ao auth.Options) login.AuthFlow {
						gotServer, got = server, ao
						return &stubAuthFlow{result: launcherOAuthResult()}
					},
				},
			}

			_, err := login.Run(context.Background(), &opts)
			require.NoError(t, err)
			assert.Equal(t, tc.wantServer, gotServer)
			assert.Equal(t, tc.wantOpts.LaunchOrigin, got.LaunchOrigin)
			assert.Equal(t, tc.wantOpts.Signup, got.Signup)
			assert.Equal(t, tc.wantOpts.ReopenOnEnter, got.ReopenOnEnter)
			if tc.wantNote != "" {
				assert.Contains(t, progress.String(), tc.wantNote)
			} else {
				assert.NotContains(t, progress.String(), "is not a Grafana Cloud portal")
			}
		})
	}
}

// TestRunCloudSignupSavesWithoutTheOptionalCloudStep covers the automatic
// finish: a signup saves the new stack connection as soon as the browser
// approves it, with no second grafana.com login in between. So does a
// non-interactive launcher login (--cloud --oauth from a script), which could
// not answer the optional prompt. An interactive launcher sign-in keeps
// offering that optional step.
func TestRunCloudSignupSavesWithoutTheOptionalCloudStep(t *testing.T) {
	agentModeOffForTest(t)
	usePlaintextCredentialStorage(t)

	newOpts := func(dir string, inputs login.Inputs) login.Options {
		return login.Options{
			Inputs: inputs,
			Hooks: login.Hooks{
				ConfigSource: configSource(dir),
				ValidateFn:   noopValidate,
				NewAuthFlow: func(string, auth.Options) login.AuthFlow {
					return &stubAuthFlow{result: launcherOAuthResult()}
				},
			},
			RetryState: login.RetryState{StagedContext: &config.Context{}},
		}
	}

	t.Run("signup", func(t *testing.T) {
		dir := t.TempDir()
		opts := newOpts(dir, login.Inputs{CloudSignup: true})
		result, err := login.Run(context.Background(), &opts)
		require.NoError(t, err)
		assert.Equal(t, "mystack", result.ContextName)
		assert.True(t, result.IsCloud)
		assert.False(t, result.HasCloudToken)

		cfg, err := config.Load(context.Background(), configSource(dir))
		require.NoError(t, err)
		ctx := cfg.Contexts["mystack"]
		require.NotNil(t, ctx)
		require.NotNil(t, ctx.Grafana)
		assert.Equal(t, "https://mystack.grafana.net", ctx.Grafana.Server)
		assert.Equal(t, "oauth", ctx.Grafana.AuthMethod)
		assert.Equal(t, "gat_test", ctx.Grafana.OAuthToken)
		assert.Equal(t, "mystack", cfg.CurrentContext)
	})

	// A person at a terminal gets the same signup: no optional Cloud step, and
	// no Cloud credential that the browser did not just issue.
	t.Run("interactive signup saves without the optional step", func(t *testing.T) {
		dir := t.TempDir()
		opts := newOpts(dir, login.Inputs{CloudSignup: true, Interactive: true})
		result, err := login.Run(context.Background(), &opts)
		require.NoError(t, err)
		assert.False(t, result.HasCloudToken)
	})

	t.Run("non-interactive sign in saves", func(t *testing.T) {
		dir := t.TempDir()
		opts := newOpts(dir, login.Inputs{UseCloudInstanceSelector: true})
		result, err := login.Run(context.Background(), &opts)
		require.NoError(t, err)
		assert.Equal(t, "mystack", result.ContextName)
	})

	t.Run("interactive sign in keeps the optional step", func(t *testing.T) {
		opts := newOpts(t.TempDir(), login.Inputs{UseCloudInstanceSelector: true, Interactive: true})
		_, err := login.Run(context.Background(), &opts)
		var need *login.ErrNeedInput
		require.ErrorAs(t, err, &need)
		assert.Equal(t, []string{"cloud-token"}, need.Fields)
		assert.True(t, need.Optional)
	})
}

// TestRunCloudSignupNeverAsksToSaveAnUnvalidatedConnection pins that a signup
// in a terminal reports a failed validation instead of asking "save anyway?".
// The person just created the account and cannot judge the failure; the stack
// exists either way, so gcx login connects it once it answers.
func TestRunCloudSignupNeverAsksToSaveAnUnvalidatedConnection(t *testing.T) {
	agentModeOffForTest(t)
	usePlaintextCredentialStorage(t)

	validationErr := &login.HealthCheckError{Server: "https://mystack.grafana.net", Status: 503, Cause: errors.New("unavailable")}
	// The sign in route is the contrast: the same failure asks the question.
	// Its Cloud token only takes the optional Cloud step out of the way.
	for _, inputs := range []login.Inputs{
		{CloudSignup: true, Interactive: true},
		{UseCloudInstanceSelector: true, Interactive: true, CloudToken: "glc_test"},
	} {
		opts := login.Options{
			Inputs: inputs,
			Hooks: login.Hooks{
				ConfigSource: configSource(t.TempDir()),
				ValidateFn: func(context.Context, login.Options, config.NamespacedRESTConfig) (string, error) {
					return "", validationErr
				},
				NewAuthFlow: func(string, auth.Options) login.AuthFlow {
					return &stubAuthFlow{result: launcherOAuthResult()}
				},
			},
			RetryState: login.RetryState{StagedContext: &config.Context{}},
		}
		_, err := login.Run(context.Background(), &opts)
		var clarify *login.ErrNeedClarification
		if inputs.CloudSignup {
			require.ErrorIs(t, err, validationErr)
			assert.NotErrorAs(t, err, &clarify)
			assert.Equal(t, "https://mystack.grafana.net", opts.Server, "the CLI reads the new stack URL for its recovery")
		} else {
			require.ErrorAs(t, err, &clarify)
			assert.Equal(t, "save-unvalidated", clarify.Field)
		}
	}
}

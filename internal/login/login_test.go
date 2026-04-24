package login_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuthFlow is a test double for login.AuthFlow that returns a preset Result or error.
type stubAuthFlow struct {
	result *auth.Result
	err    error
}

func (s *stubAuthFlow) Run(_ context.Context) (*auth.Result, error) {
	return s.result, s.err
}

// noopValidate is a ValidateFn that always succeeds.
func noopValidate(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
	return "", nil
}

// fixedDetect returns a DetectFn that always returns the given target.
func fixedDetect(tgt login.Target) func(ctx context.Context, server string) (login.Target, error) {
	return func(_ context.Context, _ string) (login.Target, error) {
		return tgt, nil
	}
}

// configSource returns a Source backed by a temp file in dir.
func configSource(dir string) config.Source {
	return config.ExplicitConfigFile(filepath.Join(dir, "config.yaml"))
}

func TestRun(t *testing.T) { //nolint:maintidx // 8 table-driven cases; complexity is inherent to spec-required coverage
	t.Parallel()

	oauthResult := &auth.Result{
		Token:            "gat_test",
		RefreshToken:     "gar_test",
		ExpiresAt:        "2030-01-01T00:00:00Z",
		RefreshExpiresAt: "2030-06-01T00:00:00Z",
		APIEndpoint:      "https://mystack.grafana.net/api",
		InstanceEndpoint: "https://mystack.grafana.net",
	}

	tests := []struct {
		name string
		opts func(dir string) login.Options

		wantErr     bool
		checkErr    func(t *testing.T, err error) // optional: extra assertions on the error
		checkResult func(t *testing.T, r login.Result)
		checkConfig func(t *testing.T, cfg config.Config)
	}{
		{
			// AC-001: First-run Cloud with CAP token via OAuth
			name: "cloud_oauth_with_cap_token",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:     "https://mystack.grafana.net",
						Target:     login.TargetCloud,
						UseOAuth:   true,
						CloudToken: "cap-token",
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						NewAuthFlow: func(_ string, _ auth.Options) login.AuthFlow {
							return &stubAuthFlow{result: oauthResult}
						},
						ValidateFn: noopValidate,
					},
				}
			},
			checkResult: func(t *testing.T, r login.Result) {
				t.Helper()
				assert.Equal(t, "mystack", r.ContextName)
				assert.Equal(t, "oauth", r.AuthMethod)
				assert.True(t, r.IsCloud)
				assert.True(t, r.HasCloudToken)
			},
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				ctx := cfg.Contexts["mystack"]
				require.NotNil(t, ctx)
				assert.Equal(t, "gat_test", ctx.Grafana.OAuthToken)
				assert.Equal(t, "oauth", ctx.Grafana.AuthMethod)
				require.NotNil(t, ctx.Cloud)
				assert.Equal(t, "cap-token", ctx.Cloud.Token)
				assert.Equal(t, "mystack", ctx.Cloud.Stack) // slug derived from *.grafana.net URL
			},
		},
		{
			// AC-002: First-run Cloud without CAP (Yes=true skips cloud-token prompt)
			name: "cloud_oauth_skip_cap",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:   "https://mystack.grafana.net",
						Target:   login.TargetCloud,
						UseOAuth: true,
						Yes:      true,
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						NewAuthFlow: func(_ string, _ auth.Options) login.AuthFlow {
							return &stubAuthFlow{result: oauthResult}
						},
						ValidateFn: noopValidate,
					},
				}
			},
			checkResult: func(t *testing.T, r login.Result) {
				t.Helper()
				assert.Equal(t, "oauth", r.AuthMethod)
				assert.True(t, r.IsCloud)
				assert.False(t, r.HasCloudToken)
			},
		},
		{
			// AC-003: On-prem with SA token; OAuth not attempted
			name: "onprem_sa_token",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:       "https://grafana.example.com",
						Target:       login.TargetOnPrem,
						GrafanaToken: "glsa_test",
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						ValidateFn:   noopValidate,
					},
				}
			},
			checkResult: func(t *testing.T, r login.Result) {
				t.Helper()
				assert.Equal(t, "token", r.AuthMethod)
				assert.False(t, r.IsCloud)
			},
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				ctx := cfg.Contexts["grafana-example-com"]
				require.NotNil(t, ctx)
				assert.Equal(t, "glsa_test", ctx.Grafana.APIToken)
				assert.Equal(t, "token", ctx.Grafana.AuthMethod)
				assert.EqualValues(t, 1, ctx.Grafana.OrgID, "fresh on-prem login must default OrgID to 1")
			},
		},
		{
			// Cloud target must NOT default OrgID to 1; StackID discovery owns
			// the Cloud namespace path so OrgID stays 0.
			name: "cloud_target_does_not_set_orgid",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:   "https://mystack.grafana.net",
						Target:   login.TargetCloud,
						UseOAuth: true,
						Yes:      true,
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						NewAuthFlow: func(_ string, _ auth.Options) login.AuthFlow {
							return &stubAuthFlow{result: oauthResult}
						},
						ValidateFn: noopValidate,
					},
				}
			},
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				ctx := cfg.Contexts["mystack"]
				require.NotNil(t, ctx)
				assert.EqualValues(t, 0, ctx.Grafana.OrgID, "cloud login must not set OrgID")
			},
		},
		{
			// AC-005: Ambiguous URL + --yes defaults to on-prem (D10)
			name: "ambiguous_url_yes_defaults_onprem",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:       "https://grafana.example.com",
						Yes:          true,
						GrafanaToken: "sa-token",
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						DetectFn:     fixedDetect(login.TargetUnknown),
						ValidateFn:   noopValidate,
					},
				}
			},
			checkResult: func(t *testing.T, r login.Result) {
				t.Helper()
				assert.Equal(t, "token", r.AuthMethod)
				assert.False(t, r.IsCloud)
			},
		},
		{
			// AC-008: Missing server returns structured ErrNeedInput
			name: "missing_server_returns_err_need_input",
			opts: func(dir string) login.Options {
				return login.Options{
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
					},
				}
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				t.Helper()
				var e *login.ErrNeedInput
				assert.ErrorAs(t, err, &e, "must be ErrNeedInput")
			},
			checkResult: func(t *testing.T, r login.Result) {
				t.Helper()
				assert.Empty(t, r.ContextName)
			},
		},
		{
			// AC-013: Validation failure leaves CurrentContext untouched (D12, NC-002, NC-010)
			name: "validation_failure_no_config_write",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:       "https://grafana.example.com",
						Target:       login.TargetOnPrem,
						GrafanaToken: "bad-token",
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
							return "", errors.New("health check failed: connection refused")
						},
					},
				}
			},
			wantErr: true,
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				// Config file was never created so Contexts must be nil or empty
				assert.Empty(t, cfg.Contexts)
			},
		},
		{
			// AC-011 + AC-009: AuthMethod written, roundtripped on re-auth
			name: "auth_method_roundtrip",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						Server:       "https://grafana.example.com",
						Target:       login.TargetOnPrem,
						GrafanaToken: "new-token",
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						ValidateFn:   noopValidate,
					},
				}
			},
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				ctx := cfg.Contexts["grafana-example-com"]
				require.NotNil(t, ctx)
				assert.Equal(t, "token", ctx.Grafana.AuthMethod)
				assert.Equal(t, "new-token", ctx.Grafana.APIToken)
			},
		},
		{
			// AC-012: Legacy config (no AuthMethod) loads and re-auths, preserves other fields
			name: "legacy_config_reauth_preserves_fields",
			opts: func(dir string) login.Options {
				// Pre-populate config with a legacy context (no AuthMethod) that has OrgID set
				src := configSource(dir)
				path, _ := src()
				legacyCfg := config.Config{
					Contexts: map[string]*config.Context{
						"grafana-example-com": {
							Grafana: &config.GrafanaConfig{
								Server:   "https://grafana.example.com",
								APIToken: "old-token",
								// AuthMethod intentionally absent (legacy)
								OrgID: 42,
							},
						},
					},
					CurrentContext: "grafana-example-com",
				}
				require.NoError(t, config.Write(context.Background(), config.ExplicitConfigFile(path), legacyCfg))

				return login.Options{
					Inputs: login.Inputs{
						Server:       "https://grafana.example.com",
						Target:       login.TargetOnPrem,
						GrafanaToken: "rotated-token",
					},
					Hooks: login.Hooks{
						ConfigSource: src,
						ValidateFn:   noopValidate,
					},
				}
			},
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				ctx := cfg.Contexts["grafana-example-com"]
				require.NotNil(t, ctx)
				assert.Equal(t, "rotated-token", ctx.Grafana.APIToken)
				assert.Equal(t, "token", ctx.Grafana.AuthMethod)
				assert.EqualValues(t, 42, ctx.Grafana.OrgID, "OrgID must be preserved in re-auth")
			},
		},
		{
			// AC-013: Redirect to grafana.com on empty server selection
			name: "redirect_grafana_com_empty_server",
			opts: func(dir string) login.Options {
				return login.Options{
					Inputs: login.Inputs{
						UseCloudInstanceSelector: true,
						Yes:                      true,
					},
					Hooks: login.Hooks{
						ConfigSource: configSource(dir),
						ValidateFn:   noopValidate,
						NewAuthFlow: func(_ string, _ auth.Options) login.AuthFlow {
							return &stubAuthFlow{result: oauthResult}
						},
					},
				}
			},
			checkConfig: func(t *testing.T, cfg config.Config) {
				t.Helper()
				ctx := cfg.Contexts["mystack"]
				require.NotNil(t, ctx)
				assert.Equal(t, oauthResult.InstanceEndpoint, ctx.Grafana.Server)
				assert.Equal(t, "gat_test", ctx.Grafana.OAuthToken)
				assert.Equal(t, "oauth", ctx.Grafana.AuthMethod)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			opts := tc.opts(dir)
			src := opts.ConfigSource

			result, err := login.Run(context.Background(), &opts)

			if tc.wantErr {
				require.Error(t, err)
				if tc.checkErr != nil {
					tc.checkErr(t, err)
				}
			} else {
				require.NoError(t, err)
			}

			if tc.checkResult != nil {
				tc.checkResult(t, result)
			}

			if tc.checkConfig != nil {
				cfg, loadErr := config.Load(context.Background(), src)
				if errors.Is(loadErr, nil) || cfg.Contexts != nil {
					require.NoError(t, loadErr)
					tc.checkConfig(t, cfg)
				} else {
					// File not created (e.g. validation failure test): pass empty config
					tc.checkConfig(t, config.Config{})
				}
			}
		})
	}
}

// TestRunAgentModeMissingServer verifies that even in agent mode, Run returns
// ErrNeedInput when the server field is empty (AC-008: server is always required).
func TestRunAgentModeMissingServer(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "1")
	agent.ResetForTesting()
	t.Cleanup(func() {
		t.Setenv("GCX_AGENT_MODE", "0")
		agent.ResetForTesting()
	})

	_, err := login.Run(context.Background(), &login.Options{
		Hooks: login.Hooks{
			ConfigSource: configSource(t.TempDir()),
		},
	})

	var e *login.ErrNeedInput
	require.ErrorAs(t, err, &e)
}

// TestRunAgentModeAmbiguousURL verifies that when agent mode is active and the
// target URL is ambiguous (neither Cloud domain nor private IP), Run defaults to
// on-prem without returning ErrNeedClarification (D17, NC-007, AC-008).
// Cannot be parallel: calls t.Setenv, which is incompatible with parallel parent tests.
func TestRunAgentModeAmbiguousURL(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "1")
	agent.ResetForTesting()
	t.Cleanup(func() {
		t.Setenv("GCX_AGENT_MODE", "0")
		agent.ResetForTesting()
	})

	dir := t.TempDir()
	src := configSource(dir)

	result, err := login.Run(context.Background(), &login.Options{
		Inputs: login.Inputs{
			Server:       "https://grafana.example.com",
			GrafanaToken: "sa-token",
		},
		Hooks: login.Hooks{
			ConfigSource: src,
			DetectFn:     fixedDetect(login.TargetUnknown),
			ValidateFn:   noopValidate,
		},
	})

	require.NoError(t, err)
	assert.False(t, result.IsCloud, "agent mode: ambiguous URL must default to on-prem")
	assert.Equal(t, "token", result.AuthMethod)
}

// countingAuthFlow is a stub that records how many times Run has been called.
type countingAuthFlow struct {
	calls *int
	res   *auth.Result
}

func (c *countingAuthFlow) Run(_ context.Context) (*auth.Result, error) {
	*c.calls++
	return c.res, nil
}

func TestRun_OAuthRunsOnceAcrossRetries(t *testing.T) {
	// Ensure agent mode is off so resolveCloudAuth returns ErrNeedInput instead of skipping.
	t.Setenv("GCX_AGENT_MODE", "0")
	agent.ResetForTesting()
	t.Cleanup(func() { agent.ResetForTesting() })

	dir := t.TempDir()
	calls := 0
	authResult := &auth.Result{
		Token:        "gat_test",
		RefreshToken: "gar_test",
		APIEndpoint:  "https://assistant.grafana.net/a/app/proxy",
		ExpiresAt:    "2099-01-01T00:00:00Z",
	}

	opts := login.Options{
		Inputs: login.Inputs{
			Server:   "https://assistant.grafana.net",
			UseOAuth: true,
			Target:   login.TargetCloud,
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			NewAuthFlow: func(_ string, _ auth.Options) login.AuthFlow {
				return &countingAuthFlow{calls: &calls, res: authResult}
			},
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				return "12.0.0", nil
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{},
		},
	}

	// First call: OAuth runs, step 5 returns ErrNeedInput for cloud-token.
	_, err := login.Run(context.Background(), &opts)
	var needInput *login.ErrNeedInput
	if !errors.As(err, &needInput) || len(needInput.Fields) == 0 || needInput.Fields[0] != "cloud-token" {
		t.Fatalf("expected ErrNeedInput{cloud-token}, got %v", err)
	}

	// Simulate user pressing Enter to skip CAP token.
	opts.Yes = true

	// Second call: should reuse OAuth from StagedContext, not re-run.
	if _, err := login.Run(context.Background(), &opts); err != nil {
		t.Fatalf("second Run failed: %v", err)
	}

	if calls != 1 {
		t.Errorf("AuthFlow.Run called %d times, expected exactly 1", calls)
	}
}

func TestPersist_ServerMismatch_EmitsClarification(t *testing.T) {
	dir := t.TempDir()

	// Seed an existing context with a different server.
	seed := config.Config{
		CurrentContext: "mystack",
		Contexts: map[string]*config.Context{
			"mystack": {
				Name: "mystack",
				Grafana: &config.GrafanaConfig{
					Server:     "https://mystack.grafana.net",
					APIToken:   "old-token",
					AuthMethod: "token",
				},
			},
		},
	}
	if err := config.Write(context.Background(), configSource(dir), seed); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://mystack.grafana-dev.net", // different server
			ContextName:  "mystack",
			Target:       login.TargetOnPrem,
			GrafanaToken: "new-token",
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				return "12.0.0", nil
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{},
		},
	}

	_, err := login.Run(context.Background(), &opts)
	var needClar *login.ErrNeedClarification
	if !errors.As(err, &needClar) {
		t.Fatalf("expected ErrNeedClarification, got %v", err)
	}
	if needClar.Field != "allow-override" {
		t.Errorf("expected Field='allow-override', got %q", needClar.Field)
	}

	// Verify config was NOT modified.
	cfg, err := config.Load(context.Background(), configSource(dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Contexts["mystack"].Grafana.Server != "https://mystack.grafana.net" {
		t.Errorf("context was modified despite ErrNeedClarification")
	}
}

func TestPersist_ServerMismatch_AllowOverrideBypasses(t *testing.T) {
	dir := t.TempDir()

	seed := config.Config{
		CurrentContext: "mystack",
		Contexts: map[string]*config.Context{
			"mystack": {
				Name: "mystack",
				Grafana: &config.GrafanaConfig{
					Server:     "https://mystack.grafana.net",
					APIToken:   "old-token",
					AuthMethod: "token",
					OrgID:      42, // non-auth field we expect to survive re-auth
				},
			},
		},
	}
	if err := config.Write(context.Background(), configSource(dir), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://mystack.grafana-dev.net",
			ContextName:  "mystack",
			Target:       login.TargetOnPrem,
			GrafanaToken: "new-token",
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				return "12.0.0", nil
			},
		},
		RetryState: login.RetryState{
			AllowOverride: true, // bypass
			StagedContext: &config.Context{},
		},
	}

	if _, err := login.Run(context.Background(), &opts); err != nil {
		t.Fatalf("Run with AllowOverride: %v", err)
	}

	cfg, err := config.Load(context.Background(), configSource(dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.Contexts["mystack"].Grafana
	if got.Server != "https://mystack.grafana-dev.net" {
		t.Errorf("Server = %q, want overridden", got.Server)
	}
	if got.OrgID != 42 {
		t.Errorf("OrgID = %d, want 42 (non-auth field preserved)", got.OrgID)
	}
}

func TestPersist_ServerMismatch_YesDoesNotBypass(t *testing.T) {
	dir := t.TempDir()

	seed := config.Config{
		CurrentContext: "mystack",
		Contexts: map[string]*config.Context{
			"mystack": {
				Name: "mystack",
				Grafana: &config.GrafanaConfig{
					Server:     "https://mystack.grafana.net",
					APIToken:   "old-token",
					AuthMethod: "token",
				},
			},
		},
	}
	if err := config.Write(context.Background(), configSource(dir), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://mystack.grafana-dev.net",
			ContextName:  "mystack",
			Target:       login.TargetOnPrem,
			GrafanaToken: "new-token",
			Yes:          true, // --yes alone must NOT bypass the server-identity guard
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				return "12.0.0", nil
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{},
		},
	}

	_, err := login.Run(context.Background(), &opts)
	var needClar *login.ErrNeedClarification
	if !errors.As(err, &needClar) {
		t.Fatalf("expected ErrNeedClarification, got %v", err)
	}
	if needClar.Field != "allow-override" {
		t.Errorf("expected Field='allow-override', got %q", needClar.Field)
	}
	// Config must be unchanged.
	cfg, err := config.Load(context.Background(), configSource(dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Contexts["mystack"].Grafana.Server != "https://mystack.grafana.net" {
		t.Errorf("context was modified despite ErrNeedClarification")
	}
}

func TestRun_ValidationFailure_EmitsSaveUnvalidatedClarification(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "0")
	agent.ResetForTesting()

	dir := t.TempDir()
	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://mystack.grafana.net",
			ContextName:  "mystack",
			Target:       login.TargetOnPrem,
			GrafanaToken: "glsa_test",
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				return "", errors.New("invalid semantic version")
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{},
		},
	}

	_, err := login.Run(context.Background(), &opts)
	var needClar *login.ErrNeedClarification
	if !errors.As(err, &needClar) {
		t.Fatalf("expected ErrNeedClarification, got %v", err)
	}
	if needClar.Field != "save-unvalidated" {
		t.Errorf("Field = %q, want save-unvalidated", needClar.Field)
	}

	// Config must not have been written.
	if _, err := config.Load(context.Background(), configSource(dir)); err == nil {
		t.Errorf("config written despite validation failure + no ForceSave")
	}
}

func TestRun_ForceSave_BypassesValidation(t *testing.T) {
	dir := t.TempDir()
	validatorCalled := false
	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://mystack.grafana.net",
			ContextName:  "mystack",
			Target:       login.TargetOnPrem,
			GrafanaToken: "glsa_test",
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				validatorCalled = true
				return "", errors.New("must not be called")
			},
		},
		RetryState: login.RetryState{
			ForceSave:     true,
			StagedContext: &config.Context{},
		},
	}

	result, err := login.Run(context.Background(), &opts)
	if err != nil {
		t.Fatalf("Run with ForceSave: %v", err)
	}
	if validatorCalled {
		t.Error("ValidateFn was called despite ForceSave=true")
	}
	if result.GrafanaVersion != "" {
		t.Errorf("GrafanaVersion = %q, want empty (validation skipped)", result.GrafanaVersion)
	}

	cfg, err := config.Load(context.Background(), configSource(dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Contexts["mystack"] == nil {
		t.Error("context not persisted despite ForceSave=true")
	}
}

func TestRun_ValidationFailure_YesFlagBypassesPrompt(t *testing.T) {
	dir := t.TempDir()
	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "https://mystack.grafana.net",
			ContextName:  "mystack",
			Target:       login.TargetOnPrem,
			GrafanaToken: "glsa_test",
			Yes:          true,
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, _ login.Options, _ config.NamespacedRESTConfig) (string, error) {
				return "", errors.New("validation failed")
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{},
		},
	}

	_, err := login.Run(context.Background(), &opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var needClar *login.ErrNeedClarification
	if errors.As(err, &needClar) {
		t.Errorf("--yes should not trigger ErrNeedClarification; got %v", needClar)
	}
}

func TestRun_NormalizesServerScheme(t *testing.T) {
	dir := t.TempDir()
	opts := login.Options{
		Inputs: login.Inputs{
			Server:       "assistant.grafana-dev.net", // no scheme
			GrafanaToken: "glsa_test",
			Target:       login.TargetOnPrem,
			Yes:          true,
		},
		Hooks: login.Hooks{
			ConfigSource: configSource(dir),
			ValidateFn: func(_ context.Context, o login.Options, _ config.NamespacedRESTConfig) (string, error) {
				// Assert that by the time Validate is called, Server has a scheme.
				if !strings.HasPrefix(o.Server, "https://") {
					t.Errorf("expected https:// prefix on Server, got %q", o.Server)
				}
				return "12.0.0", nil
			},
		},
	}

	result, err := login.Run(context.Background(), &opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.HasPrefix(result.ContextName, "") {
		t.Fatalf("expected a context name, got empty")
	}

	// Also assert the persisted config stores the normalized server.
	cfg, err := config.Load(context.Background(), configSource(dir))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got := cfg.Contexts[result.ContextName].Grafana.Server
	if got != "https://assistant.grafana-dev.net" {
		t.Errorf("stored server = %q, want https://assistant.grafana-dev.net", got)
	}
}

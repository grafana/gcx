package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scrubTargetKindEnv unsets the env vars that influence classification.
// Set-but-empty is not enough: the real env parsing uses LookupEnv, so an
// empty GRAFANA_SERVER would clear the config-file server instead.
func scrubTargetKindEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GRAFANA_SERVER", "GRAFANA_CLOUD_STACK", "GRAFANA_STACK_ID"} {
		t.Setenv(key, "") // register restoration of the real value
		require.NoError(t, os.Unsetenv(key))
	}
}

// testEnvOverride mirrors the env override every command load path applies
// (providers.envOverride / cloudEnvOverride): ensure a current context exists
// and parse GRAFANA_* env vars into it.
func testEnvOverride(cfg *config.Config) error {
	if cfg.CurrentContext == "" {
		cfg.CurrentContext = config.DefaultContextName
	}
	if !cfg.HasContext(cfg.CurrentContext) {
		cfg.SetContext(cfg.CurrentContext, true, config.Context{})
	}
	return config.ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
}

func TestCapturedTargetKind(t *testing.T) {
	cloudConfig := `
stacks:
  prod:
    grafana:
      server: https://mystack.grafana.net
contexts:
  dev:
    stack: prod
current-context: dev
`

	tests := []struct {
		name  string
		user  string // user-layer config content, "" for none
		local string // repo-local .gcx.yaml content, "" for none
		env   map[string]string
		want  string
	}{
		{
			name: "no config",
			want: "",
		},
		{
			name: "cloud server URL",
			user: cloudConfig,
			want: "cloud",
		},
		{
			name: "self-hosted server",
			user: `
stacks:
  onprem:
    grafana:
      server: https://grafana.internal.example.com
contexts:
  dev:
    stack: onprem
current-context: dev
`,
			want: "self-hosted",
		},
		{
			name: "explicit stack slug",
			user: `
stacks:
  prod:
    slug: mystack
contexts:
  dev:
    stack: prod
current-context: dev
`,
			want: "cloud",
		},
		{
			name: "stack id on custom domain",
			user: `
stacks:
  prod:
    grafana:
      server: https://grafana.example.com
      stack-id: 12345
contexts:
  dev:
    stack: prod
current-context: dev
`,
			want: "cloud",
		},
		{
			name: "no current context",
			user: `
stacks:
  prod:
    grafana:
      server: https://mystack.grafana.net
contexts:
  dev:
    stack: prod
`,
			want: "",
		},
		{
			name: "env-only GRAFANA_CLOUD_STACK",
			env:  map[string]string{"GRAFANA_CLOUD_STACK": "mystack"},
			want: "cloud",
		},
		{
			name: "env-only self-hosted GRAFANA_SERVER",
			env:  map[string]string{"GRAFANA_SERVER": "http://localhost:3000"},
			want: "self-hosted",
		},
		{
			name: "env-only cloud GRAFANA_SERVER",
			env:  map[string]string{"GRAFANA_SERVER": "https://mystack.grafana.net"},
			want: "cloud",
		},
		{
			name: "env GRAFANA_STACK_ID marks custom domain as cloud",
			env: map[string]string{
				"GRAFANA_SERVER":   "https://grafana.mycorp.com",
				"GRAFANA_STACK_ID": "12345",
			},
			want: "cloud",
		},
		{
			name: "legacy config cloud",
			user: `
contexts:
  dev:
    grafana:
      server: https://mystack.grafana.net
current-context: dev
`,
			want: "cloud",
		},
		{
			name: "legacy config cloud via cloud stack slug",
			user: `
contexts:
  dev:
    grafana:
      server: https://grafana.internal.example.com
    cloud:
      stack: mystack
current-context: dev
`,
			want: "cloud",
		},
		{
			name: "legacy config self-hosted",
			user: `
contexts:
  dev:
    grafana:
      server: http://localhost:3000
      token: abc
current-context: dev
`,
			want: "self-hosted",
		},
		{
			name: "local layer switches current context",
			user: cloudConfig,
			local: `
stacks:
  onprem:
    grafana:
      server: http://localhost:3000
contexts:
  local:
    stack: onprem
current-context: local
`,
			want: "self-hosted",
		},
		{
			name: "local context references user-layer stack",
			user: `
stacks:
  prod:
    grafana:
      server: https://mystack.grafana.net
contexts:
  dev:
    stack: prod
`,
			local: `
contexts:
  dev: {}
current-context: dev
`,
			want: "cloud",
		},
		{
			name: "legacy cloud-auth-only local layer keeps user-layer stack ref",
			user: `
stacks:
  prod:
    slug: mystack
contexts:
  dev:
    stack: prod
current-context: dev
`,
			local: `
contexts:
  dev:
    cloud:
      token: abc
`,
			want: "cloud",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userDir, workDir := isolatedLoaderEnv(t)
			scrubTargetKindEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.user != "" {
				writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), tt.user)
			}
			if tt.local != "" {
				writeLoaderConfig(t, filepath.Join(workDir, ".gcx.yaml"), tt.local)
			}

			_, err := config.LoadLayered(t.Context(), "", testEnvOverride)
			require.NoError(t, err)
			assert.Equal(t, tt.want, config.CapturedTargetKind())
		})
	}
}

func TestCapturedTargetKind_ContextOverride(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	scrubTargetKindEnv(t)

	// current-context says cloud; a --context style override selects the
	// self-hosted context and must win.
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), `
stacks:
  prod:
    slug: mystack
  onprem:
    grafana:
      server: http://localhost:3000
contexts:
  dev:
    stack: prod
  local:
    stack: onprem
current-context: dev
`)

	selectLocal := func(cfg *config.Config) error {
		cfg.CurrentContext = "local"
		return nil
	}
	_, err := config.LoadLayered(t.Context(), "", selectLocal, testEnvOverride)
	require.NoError(t, err)
	assert.Equal(t, "self-hosted", config.CapturedTargetKind())
}

func TestCapturedTargetKind_ExplicitConfigFile(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	scrubTargetKindEnv(t)

	// The discoverable user layer says cloud; the explicit file must bypass it.
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), `
stacks:
  prod:
    slug: mystack
contexts:
  dev:
    stack: prod
current-context: dev
`)

	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	writeLoaderConfig(t, explicit, `
stacks:
  onprem:
    grafana:
      server: http://localhost:3000
contexts:
  dev:
    stack: onprem
current-context: dev
`)

	_, err := config.LoadLayered(t.Context(), explicit, testEnvOverride)
	require.NoError(t, err)
	assert.Equal(t, "self-hosted", config.CapturedTargetKind())
}

func TestCapturedTargetKind_FailedOverrideStillRecordsTarget(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	scrubTargetKindEnv(t)

	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), `
stacks:
  onprem:
    grafana:
      server: http://localhost:3000
contexts:
  dev:
    stack: onprem
current-context: dev
`)

	// Seed a different kind first. Without this the assertion passes against an
	// implementation that captures nothing on the error path, because an earlier
	// test in this package leaves "self-hosted" in the process-wide value and the
	// stale reading is indistinguishable from a fresh one.
	config.CaptureTargetKind(config.TargetKindCloud)

	// Commands run context validation as an override (providers.LoadGrafanaConfig),
	// so a config that resolves but fails validation — a self-hosted stack with no
	// org-id, for instance — reaches this path with a fully merged config. The
	// target is known and must be recorded, otherwise those invocations look
	// identical to ones that had no config at all.
	failValidation := func(*config.Config) error {
		return errors.New("missing stacks.onprem.grafana.org-id")
	}

	_, err := config.LoadLayered(t.Context(), "", testEnvOverride, failValidation)
	require.Error(t, err)
	assert.Equal(t, "self-hosted", config.CapturedTargetKind())
}

func TestCapturedTargetKind_ContextSelectionFailureLeavesTargetUnreported(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	scrubTargetKindEnv(t)

	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), `
stacks:
  onprem:
    grafana:
      server: http://localhost:3000
contexts:
  dev:
    stack: onprem
current-context: dev
`)

	// Start from a genuinely unset value so the assertion below is about the
	// label being absent, not about some earlier value happening to survive.
	config.ClearCapturedTargetKind()

	// --context naming a context that does not exist fails selection without
	// changing CurrentContext, so the merged config still describes the
	// previously current self-hosted context. That context is not what the
	// invocation targeted and must not be reported as though it were.
	selectMissing := func(*config.Config) error {
		return config.ContextNotFound("does-not-exist", nil)
	}

	_, err := config.LoadLayered(t.Context(), "", selectMissing, testEnvOverride)
	require.ErrorIs(t, err, config.ErrContextNotFound)
	assert.Empty(t, config.CapturedTargetKind())
}

func TestCapturedTargetKind_CloudCredentialOnlyContextIsUnclassified(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	scrubTargetKindEnv(t)

	// Seeded with a kind, so passing requires the load to actively record
	// "unknown" rather than inherit an empty value from an earlier test.
	config.CaptureTargetKind(config.TargetKindCloud)

	// A cloud entry on its own is an org-wide GCOM credential: no stack slug, no
	// Grafana server, so it names no Grafana instance to classify. Pinned so the
	// empty value stays a decision rather than becoming an accident.
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), `
cloud:
  grafana-com:
    token: abc
contexts:
  dev:
    cloud: grafana-com
current-context: dev
`)

	_, err := config.LoadLayered(t.Context(), "", testEnvOverride)
	require.NoError(t, err)
	assert.Empty(t, config.CapturedTargetKind())
}

func TestCaptureTargetKindForServer(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{name: "cloud host", server: "https://mystack.grafana.net", want: "cloud"},
		{name: "self-hosted host", server: "http://localhost:3000", want: "self-hosted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.CaptureTargetKindForServer(tt.server)
			assert.Equal(t, tt.want, config.CapturedTargetKind())
		})
	}
}

func TestCaptureTargetKindForServer_EmptyServerKeepsCapturedKind(t *testing.T) {
	config.CaptureTargetKindForServer("https://mystack.grafana.net")
	config.CaptureTargetKindForServer("")
	assert.Equal(t, "cloud", config.CapturedTargetKind())
}

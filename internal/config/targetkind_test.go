package config_test

import (
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
	cloudConfig := "stacks:\n" +
		"  prod:\n" +
		"    grafana:\n" +
		"      server: https://mystack.grafana.net\n" +
		"contexts:\n" +
		"  dev:\n" +
		"    stack: prod\n" +
		"current-context: dev\n"

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
			user: "stacks:\n" +
				"  onprem:\n" +
				"    grafana:\n" +
				"      server: https://grafana.internal.example.com\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: onprem\n" +
				"current-context: dev\n",
			want: "self-hosted",
		},
		{
			name: "explicit stack slug",
			user: "stacks:\n" +
				"  prod:\n" +
				"    slug: mystack\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: prod\n" +
				"current-context: dev\n",
			want: "cloud",
		},
		{
			name: "stack id on custom domain",
			user: "stacks:\n" +
				"  prod:\n" +
				"    grafana:\n" +
				"      server: https://grafana.example.com\n" +
				"      stack-id: 12345\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: prod\n" +
				"current-context: dev\n",
			want: "cloud",
		},
		{
			name: "no current context",
			user: "stacks:\n" +
				"  prod:\n" +
				"    grafana:\n" +
				"      server: https://mystack.grafana.net\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: prod\n",
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
			user: "contexts:\n" +
				"  dev:\n" +
				"    grafana:\n" +
				"      server: https://mystack.grafana.net\n" +
				"current-context: dev\n",
			want: "cloud",
		},
		{
			name: "legacy config cloud via cloud stack slug",
			user: "contexts:\n" +
				"  dev:\n" +
				"    grafana:\n" +
				"      server: https://grafana.internal.example.com\n" +
				"    cloud:\n" +
				"      stack: mystack\n" +
				"current-context: dev\n",
			want: "cloud",
		},
		{
			name: "legacy config self-hosted",
			user: "contexts:\n" +
				"  dev:\n" +
				"    grafana:\n" +
				"      server: http://localhost:3000\n" +
				"      token: abc\n" +
				"current-context: dev\n",
			want: "self-hosted",
		},
		{
			name: "local layer switches current context",
			user: cloudConfig,
			local: "stacks:\n" +
				"  onprem:\n" +
				"    grafana:\n" +
				"      server: http://localhost:3000\n" +
				"contexts:\n" +
				"  local:\n" +
				"    stack: onprem\n" +
				"current-context: local\n",
			want: "self-hosted",
		},
		{
			name: "local context references user-layer stack",
			user: "stacks:\n" +
				"  prod:\n" +
				"    grafana:\n" +
				"      server: https://mystack.grafana.net\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: prod\n",
			local: "contexts:\n" +
				"  dev: {}\n" +
				"current-context: dev\n",
			want: "cloud",
		},
		{
			name: "legacy cloud-auth-only local layer keeps user-layer stack ref",
			user: "stacks:\n" +
				"  prod:\n" +
				"    slug: mystack\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: prod\n" +
				"current-context: dev\n",
			local: "contexts:\n" +
				"  dev:\n" +
				"    cloud:\n" +
				"      token: abc\n",
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
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"stacks:\n"+
			"  prod:\n"+
			"    slug: mystack\n"+
			"  onprem:\n"+
			"    grafana:\n"+
			"      server: http://localhost:3000\n"+
			"contexts:\n"+
			"  dev:\n"+
			"    stack: prod\n"+
			"  local:\n"+
			"    stack: onprem\n"+
			"current-context: dev\n")

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
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"stacks:\n  prod:\n    slug: mystack\ncontexts:\n  dev:\n    stack: prod\ncurrent-context: dev\n")

	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	writeLoaderConfig(t, explicit,
		"stacks:\n  onprem:\n    grafana:\n      server: http://localhost:3000\ncontexts:\n  dev:\n    stack: onprem\ncurrent-context: dev\n")

	_, err := config.LoadLayered(t.Context(), explicit, testEnvOverride)
	require.NoError(t, err)
	assert.Equal(t, "self-hosted", config.CapturedTargetKind())
}

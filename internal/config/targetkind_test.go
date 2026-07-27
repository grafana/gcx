package config_test

import (
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestLoadTargetKind(t *testing.T) {
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
			name: "self-managed server",
			user: "stacks:\n" +
				"  onprem:\n" +
				"    grafana:\n" +
				"      server: https://grafana.internal.example.com\n" +
				"contexts:\n" +
				"  dev:\n" +
				"    stack: onprem\n" +
				"current-context: dev\n",
			want: "self-managed",
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
			name: "malformed config",
			user: "{{{ not yaml",
			want: "",
		},
		{
			name: "env-only GRAFANA_CLOUD_STACK",
			env:  map[string]string{"GRAFANA_CLOUD_STACK": "mystack"},
			want: "cloud",
		},
		{
			name: "env-only self-managed GRAFANA_SERVER",
			env:  map[string]string{"GRAFANA_SERVER": "http://localhost:3000"},
			want: "self-managed",
		},
		{
			name: "env-only cloud GRAFANA_SERVER",
			env:  map[string]string{"GRAFANA_SERVER": "https://mystack.grafana.net"},
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
			name: "legacy config self-managed",
			user: "contexts:\n" +
				"  dev:\n" +
				"    grafana:\n" +
				"      server: http://localhost:3000\n" +
				"      token: abc\n" +
				"current-context: dev\n",
			want: "self-managed",
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
			want: "self-managed",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userDir, workDir := isolatedLoaderEnv(t)
			t.Setenv("GRAFANA_CLOUD_STACK", "")
			t.Setenv("GRAFANA_SERVER", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.user != "" {
				writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), tt.user)
			}
			if tt.local != "" {
				writeLoaderConfig(t, filepath.Join(workDir, ".gcx.yaml"), tt.local)
			}

			assert.Equal(t, tt.want, config.LoadTargetKind(t.Context()))
		})
	}
}

func TestLoadTargetKind_ExplicitConfigFile(t *testing.T) {
	userDir, _ := isolatedLoaderEnv(t)
	t.Setenv("GRAFANA_CLOUD_STACK", "")
	t.Setenv("GRAFANA_SERVER", "")

	// The discoverable user layer says cloud; the explicit file must bypass it.
	writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"),
		"stacks:\n  prod:\n    slug: mystack\ncontexts:\n  dev:\n    stack: prod\ncurrent-context: dev\n")

	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	writeLoaderConfig(t, explicit,
		"stacks:\n  onprem:\n    grafana:\n      server: http://localhost:3000\ncontexts:\n  dev:\n    stack: onprem\ncurrent-context: dev\n")
	t.Setenv("GCX_CONFIG", explicit)

	assert.Equal(t, "self-managed", config.LoadTargetKind(t.Context()))
}

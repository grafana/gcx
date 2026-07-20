package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveContextPath(t *testing.T) {
	withStack := config.Config{
		CurrentContext: "dev",
		Contexts: map[string]*config.Context{
			"dev": {Stack: "dev-stack"},
		},
		Stacks: map[string]*config.StackConfig{
			"dev-stack": {},
		},
	}

	testCases := []struct {
		name    string
		cfg     config.Config
		path    string
		want    string
		wantErr string
	}{
		{
			name: "bare datasources path resolves under current context",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "datasources.prometheus",
			want: "contexts.dev.datasources.prometheus",
		},
		{
			name: "bare stack ref resolves under current context",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "stack",
			want: "contexts.dev.stack",
		},
		{
			name: "bare cloud ref resolves under current context",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "cloud",
			want: "contexts.dev.cloud",
		},
		{
			name: "bare grafana path resolves under current context's stack",
			cfg:  withStack,
			path: "grafana.tls.insecure-skip-verify",
			want: "stacks.dev-stack.grafana.tls.insecure-skip-verify",
		},
		{
			name: "bare providers path resolves under current context's stack",
			cfg:  withStack,
			path: "providers.slo.token",
			want: "stacks.dev-stack.providers.slo.token",
		},
		{
			name: "bare slug resolves under current context's stack",
			cfg:  withStack,
			path: "slug",
			want: "stacks.dev-stack.slug",
		},
		{
			name: "contexts prefix is left alone",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "contexts.other.datasources.loki",
			want: "contexts.other.datasources.loki",
		},
		{
			name: "stacks prefix is left alone",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "stacks.other.grafana.server",
			want: "stacks.other.grafana.server",
		},
		{
			name: "qualified cloud entry path is left alone",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "cloud.grafana-com.token",
			want: "cloud.grafana-com.token",
		},
		{
			name: "current-context is left alone",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "current-context",
			want: "current-context",
		},
		{
			name:    "bare cloud entry field errors with a hint",
			cfg:     config.Config{CurrentContext: "dev"},
			path:    "cloud.token",
			wantErr: "cloud credentials now live in named entries",
		},
		{
			name:    "legacy default datasource path errors with the new path",
			cfg:     config.Config{CurrentContext: "dev"},
			path:    "default-prometheus-datasource",
			wantErr: "use datasources.prometheus",
		},
		{
			name:    "bare path with no current context errors",
			cfg:     config.Config{},
			path:    "datasources.prometheus",
			wantErr: "no current context set",
		},
		{
			name:    "bare grafana path with no stack ref errors",
			cfg:     config.Config{CurrentContext: "dev", Contexts: map[string]*config.Context{"dev": {}}},
			path:    "grafana.server",
			wantErr: "current context references no stack",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := config.ResolveContextPath(tc.cfg, tc.path)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

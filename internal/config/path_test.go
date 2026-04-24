package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveContextPath(t *testing.T) {
	testCases := []struct {
		name    string
		cfg     config.Config
		path    string
		want    string
		wantErr string
	}{
		{
			name: "bare path resolves under current context",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "cloud.token",
			want: "contexts.dev.cloud.token",
		},
		{
			name: "nested bare path resolves under current context",
			cfg:  config.Config{CurrentContext: "prod"},
			path: "grafana.tls.insecure-skip-verify",
			want: "contexts.prod.grafana.tls.insecure-skip-verify",
		},
		{
			name: "contexts prefix is left alone",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "contexts.other.cloud.token",
			want: "contexts.other.cloud.token",
		},
		{
			name: "current-context is left alone",
			cfg:  config.Config{CurrentContext: "dev"},
			path: "current-context",
			want: "current-context",
		},
		{
			name:    "bare path with no current context errors",
			cfg:     config.Config{},
			path:    "cloud.token",
			wantErr: "no current context set",
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

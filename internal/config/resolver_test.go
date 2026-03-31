package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDefaultDatasourceUID(t *testing.T) {
	testCases := []struct {
		name     string
		ctx      config.Context
		kind     string
		expected string
	}{
		{
			name: "new datasources key takes precedence over legacy field (prometheus)",
			ctx: config.Context{
				Datasources:                 map[string]string{"prometheus": "new-uid"},
				DefaultPrometheusDatasource: "legacy-uid",
			},
			kind:     "prometheus",
			expected: "new-uid",
		},
		{
			name: "legacy prometheus fallback when no datasources entry",
			ctx: config.Context{
				DefaultPrometheusDatasource: "legacy-uid",
			},
			kind:     "prometheus",
			expected: "legacy-uid",
		},
		{
			name: "new datasources key takes precedence over legacy field (loki)",
			ctx: config.Context{
				Datasources:           map[string]string{"loki": "new-loki-uid"},
				DefaultLokiDatasource: "legacy-loki-uid",
			},
			kind:     "loki",
			expected: "new-loki-uid",
		},
		{
			name: "legacy loki fallback when no datasources entry",
			ctx: config.Context{
				DefaultLokiDatasource: "legacy-loki-uid",
			},
			kind:     "loki",
			expected: "legacy-loki-uid",
		},
		{
			name: "new datasources key takes precedence over legacy field (pyroscope)",
			ctx: config.Context{
				Datasources:                map[string]string{"pyroscope": "new-pyro-uid"},
				DefaultPyroscopeDatasource: "legacy-pyro-uid",
			},
			kind:     "pyroscope",
			expected: "new-pyro-uid",
		},
		{
			name: "legacy pyroscope fallback when no datasources entry",
			ctx: config.Context{
				DefaultPyroscopeDatasource: "legacy-pyro-uid",
			},
			kind:     "pyroscope",
			expected: "legacy-pyro-uid",
		},
		{
			name:     "returns empty string when neither datasources entry nor legacy field set (loki)",
			ctx:      config.Context{},
			kind:     "loki",
			expected: "",
		},
		{
			name:     "returns empty string for unknown kind",
			ctx:      config.Context{},
			kind:     "tempo",
			expected: "",
		},
		{
			name: "datasources map with different kind does not match",
			ctx: config.Context{
				Datasources:           map[string]string{"prometheus": "prom-uid"},
				DefaultLokiDatasource: "loki-uid",
			},
			kind:     "loki",
			expected: "loki-uid",
		},
		{
			name: "empty datasources entry falls through to legacy field",
			ctx: config.Context{
				Datasources:                 map[string]string{"prometheus": ""},
				DefaultPrometheusDatasource: "legacy-uid",
			},
			kind:     "prometheus",
			expected: "legacy-uid",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			req.Equal(tc.expected, config.DefaultDatasourceUID(tc.ctx, tc.kind))
		})
	}
}

func TestSetValue_DatasourcesSection(t *testing.T) {
	testCases := []struct {
		name           string
		input          config.Config
		path           string
		value          string
		expectedOutput config.Config
	}{
		{
			name:  "set datasources.prometheus in new context",
			input: config.Config{},
			path:  "contexts.myctx.datasources.prometheus",
			value: "prom-uid-123",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{"prometheus": "prom-uid-123"},
					},
				},
			},
		},
		{
			name: "set datasources.loki alongside existing datasources entry",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{"prometheus": "prom-uid"},
					},
				},
			},
			path:  "contexts.myctx.datasources.loki",
			value: "loki-uid",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{
							"prometheus": "prom-uid",
							"loki":       "loki-uid",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			err := config.SetValue(&tc.input, tc.path, tc.value)
			req.NoError(err)
			req.Equal(tc.expectedOutput, tc.input)
		})
	}
}

func TestUnsetValue_DatasourcesSection(t *testing.T) {
	testCases := []struct {
		name           string
		input          config.Config
		path           string
		expectedOutput config.Config
	}{
		{
			name: "unset datasources.loki removes the loki key",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{
							"prometheus": "prom-uid",
							"loki":       "loki-uid",
						},
					},
				},
			},
			path: "contexts.myctx.datasources.loki",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{
							"prometheus": "prom-uid",
						},
					},
				},
			},
		},
		{
			name: "unset datasources.prometheus removes the prometheus key",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{"prometheus": "prom-uid-123"},
					},
				},
			},
			path: "contexts.myctx.datasources.prometheus",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"myctx": {
						Datasources: map[string]string{},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			err := config.UnsetValue(&tc.input, tc.path)
			req.NoError(err)
			req.Equal(tc.expectedOutput, tc.input)
		})
	}
}

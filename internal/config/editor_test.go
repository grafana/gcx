package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

func Test_SetValue(t *testing.T) {
	testCases := []struct {
		name           string
		input          config.Config
		path           string
		value          string
		expectedOutput config.Config
	}{
		{
			name:           "string",
			input:          config.Config{},
			path:           "current-context",
			value:          "ctx-name",
			expectedOutput: config.Config{CurrentContext: "ctx-name"},
		},
		{
			name:  "string in new context",
			input: config.Config{},
			path:  "contexts.new.grafana.server",
			value: "url",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"new": {
						Grafana: &config.GrafanaConfig{Server: "url"},
					},
				},
			},
		},
		{
			name: "string in existing context",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "url"},
					},
				},
			},
			path:  "contexts.existing.grafana.server",
			value: "new-url",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "new-url"},
					},
				},
			},
		},
		{
			name:  "boolean in new context",
			input: config.Config{},
			path:  "contexts.new.grafana.tls.insecure-skip-verify",
			value: "true",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"new": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{Insecure: true}},
					},
				},
			},
		},
		{
			name:  "bytes in new context",
			input: config.Config{},
			path:  "contexts.new.grafana.tls.cert-data",
			value: "foo bar baz",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"new": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{CertData: []byte("foo bar baz")}},
					},
				},
			},
		},
		{
			name:  "int64 in new context",
			input: config.Config{},
			path:  "contexts.new.grafana.org-id",
			value: "1",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"new": {
						Grafana: &config.GrafanaConfig{OrgID: 1},
					},
				},
			},
		},
		{
			name:  "provider key in new context",
			input: config.Config{},
			path:  "contexts.default.providers.slo.token",
			value: "my-token",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "my-token"},
						},
					},
				},
			},
		},
		{
			name: "provider key with existing provider",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "old-token"},
						},
					},
				},
			},
			path:  "contexts.default.providers.slo.url",
			value: "https://slo.example.com",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "old-token", "url": "https://slo.example.com"},
						},
					},
				},
			},
		},
		{
			name: "provider key with null provider map",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": nil,
						},
					},
				},
			},
			path:  "contexts.default.providers.slo.token",
			value: "my-token",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "my-token"},
						},
					},
				},
			},
		},
		{
			name:  "cloud token in new context",
			input: config.Config{},
			path:  "contexts.dev.cloud.token",
			value: "my-token",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"dev": {
						Cloud: &config.CloudConfig{Token: "my-token"},
					},
				},
			},
		},
		{
			name:  "cloud stack in new context",
			input: config.Config{},
			path:  "contexts.dev.cloud.stack",
			value: "mystack",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"dev": {
						Cloud: &config.CloudConfig{Stack: "mystack"},
					},
				},
			},
		},
		{
			name:  "cloud api-url in new context",
			input: config.Config{},
			path:  "contexts.dev.cloud.api-url",
			value: "grafana-dev.com",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"dev": {
						Cloud: &config.CloudConfig{APIUrl: "grafana-dev.com"},
					},
				},
			},
		},
		{
			name: "provider key creates new provider alongside existing",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token"},
						},
					},
				},
			},
			path:  "contexts.default.providers.oncall.token",
			value: "oncall-token",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo":    {"token": "slo-token"},
							"oncall": {"token": "oncall-token"},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := require.New(t)

			err := config.SetValue(&testCase.input, testCase.path, testCase.value)
			req.NoError(err)

			req.Equal(testCase.expectedOutput, testCase.input)
		})
	}
}

func Test_SetValue_withUnknownField(t *testing.T) {
	req := require.New(t)
	input := config.Config{}

	err := config.SetValue(&input, "unknown", "value")
	req.Error(err)
}

func Test_UnsetValue(t *testing.T) {
	testCases := []struct {
		name           string
		input          config.Config
		path           string
		expectedOutput config.Config
	}{
		{
			name:           "string",
			input:          config.Config{CurrentContext: "ctx-name"},
			path:           "current-context",
			expectedOutput: config.Config{},
		},
		{
			name: "map entry",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"dev": {
						Grafana: &config.GrafanaConfig{Server: "dev-url"},
					},
					"prod": {
						Grafana: &config.GrafanaConfig{Server: "prod-url"},
					},
				},
			},
			path: "contexts.prod",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"dev": {
						Grafana: &config.GrafanaConfig{Server: "dev-url"},
					},
				},
			},
		},
		{
			name: "string in context",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "url", User: "user"},
					},
				},
			},
			path: "contexts.existing.grafana.user",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "url"},
					},
				},
			},
		},
		{
			name: "boolean in new context",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"existing": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{Insecure: true}},
					},
				},
			},
			path: "contexts.existing.grafana.tls.insecure-skip-verify",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"existing": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{Insecure: false}},
					},
				},
			},
		},
		{
			name: "unset provider key",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token", "url": "https://slo.example.com"},
						},
					},
				},
			},
			path: "contexts.default.providers.slo.url",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token"},
						},
					},
				},
			},
		},
		{
			name: "unset entire provider",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo":    {"token": "slo-token"},
							"oncall": {"token": "oncall-token"},
						},
					},
				},
			},
			path: "contexts.default.providers.slo",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"oncall": {"token": "oncall-token"},
						},
					},
				},
			},
		},
		{
			name: "unset provider key with null provider map is no-op",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": nil,
						},
					},
				},
			},
			path: "contexts.default.providers.slo.token",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": nil,
						},
					},
				},
			},
		},
		{
			name: "unset non-existent nested provider key is no-op",
			input: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token"},
						},
					},
				},
			},
			path: "contexts.default.providers.oncall.token",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token"},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := require.New(t)

			err := config.UnsetValue(&testCase.input, testCase.path)
			req.NoError(err)

			req.Equal(testCase.expectedOutput, testCase.input)
		})
	}
}

func Test_UnsetValue_withUnknownField(t *testing.T) {
	req := require.New(t)
	input := config.Config{}

	err := config.UnsetValue(&input, "unknown")
	req.Error(err)
}

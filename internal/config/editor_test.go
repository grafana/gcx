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
			name:  "string in new stack",
			input: config.Config{},
			path:  "stacks.new.grafana.server",
			value: "url",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"new": {
						Grafana: &config.GrafanaConfig{Server: "url"},
					},
				},
			},
		},
		{
			name: "string in existing stack",
			input: config.Config{
				Stacks: map[string]*config.StackConfig{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "url"},
					},
				},
			},
			path:  "stacks.existing.grafana.server",
			value: "new-url",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "new-url"},
					},
				},
			},
		},
		{
			name:  "boolean in new stack",
			input: config.Config{},
			path:  "stacks.new.grafana.tls.insecure-skip-verify",
			value: "true",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"new": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{Insecure: true}},
					},
				},
			},
		},
		{
			name:  "bytes in new stack",
			input: config.Config{},
			path:  "stacks.new.grafana.tls.cert-data",
			value: "foo bar baz",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"new": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{CertData: []byte("foo bar baz")}},
					},
				},
			},
		},
		{
			name:  "int64 in new stack",
			input: config.Config{},
			path:  "stacks.new.grafana.org-id",
			value: "1",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"new": {
						Grafana: &config.GrafanaConfig{OrgID: 1},
					},
				},
			},
		},
		{
			name:  "provider key in new stack",
			input: config.Config{},
			path:  "stacks.default.providers.slo.token",
			value: "my-token",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "old-token"},
						},
					},
				},
			},
			path:  "stacks.default.providers.slo.url",
			value: "https://slo.example.com",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": nil,
						},
					},
				},
			},
			path:  "stacks.default.providers.slo.token",
			value: "my-token",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "my-token"},
						},
					},
				},
			},
		},
		{
			name:  "token in new cloud entry",
			input: config.Config{},
			path:  "cloud.dev.token",
			value: "my-token",
			expectedOutput: config.Config{
				Cloud: map[string]*config.CloudEntry{
					"dev": {Token: "my-token"},
				},
			},
		},
		{
			name:  "slug in new stack",
			input: config.Config{},
			path:  "stacks.dev.slug",
			value: "mystack",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"dev": {Slug: "mystack"},
				},
			},
		},
		{
			name:  "stack ref in new context",
			input: config.Config{},
			path:  "contexts.dev.stack",
			value: "dev",
			expectedOutput: config.Config{
				Contexts: map[string]*config.Context{
					"dev": {Stack: "dev"},
				},
			},
		},
		{
			name:  "api-url in new cloud entry",
			input: config.Config{},
			path:  "cloud.dev.api-url",
			value: "grafana-dev.com",
			expectedOutput: config.Config{
				Cloud: map[string]*config.CloudEntry{
					"dev": {APIUrl: "grafana-dev.com"},
				},
			},
		},
		{
			name: "provider key creates new provider alongside existing",
			input: config.Config{
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token"},
						},
					},
				},
			},
			path:  "stacks.default.providers.oncall.token",
			value: "oncall-token",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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
				Stacks: map[string]*config.StackConfig{
					"dev": {
						Grafana: &config.GrafanaConfig{Server: "dev-url"},
					},
					"prod": {
						Grafana: &config.GrafanaConfig{Server: "prod-url"},
					},
				},
			},
			path: "stacks.prod",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"dev": {
						Grafana: &config.GrafanaConfig{Server: "dev-url"},
					},
				},
			},
		},
		{
			name: "string in stack",
			input: config.Config{
				Stacks: map[string]*config.StackConfig{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "url", User: "user"},
					},
				},
			},
			path: "stacks.existing.grafana.user",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"existing": {
						Grafana: &config.GrafanaConfig{Server: "url"},
					},
				},
			},
		},
		{
			name: "boolean in stack",
			input: config.Config{
				Stacks: map[string]*config.StackConfig{
					"existing": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{Insecure: true}},
					},
				},
			},
			path: "stacks.existing.grafana.tls.insecure-skip-verify",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
					"existing": {
						Grafana: &config.GrafanaConfig{TLS: &config.TLS{Insecure: false}},
					},
				},
			},
		},
		{
			name: "unset provider key",
			input: config.Config{
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token", "url": "https://slo.example.com"},
						},
					},
				},
			},
			path: "stacks.default.providers.slo.url",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo":    {"token": "slo-token"},
							"oncall": {"token": "oncall-token"},
						},
					},
				},
			},
			path: "stacks.default.providers.slo",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": nil,
						},
					},
				},
			},
			path: "stacks.default.providers.slo.token",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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
				Stacks: map[string]*config.StackConfig{
					"default": {
						Providers: map[string]map[string]string{
							"slo": {"token": "slo-token"},
						},
					},
				},
			},
			path: "stacks.default.providers.oncall.token",
			expectedOutput: config.Config{
				Stacks: map[string]*config.StackConfig{
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

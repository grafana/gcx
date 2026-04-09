package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

func TestConfig_HasContext(t *testing.T) {
	req := require.New(t)

	cfg := config.Config{
		Contexts: map[string]*config.Context{
			"dev": {
				Grafana: &config.GrafanaConfig{Server: "dev-server"},
			},
		},
		CurrentContext: "dev",
	}

	req.True(cfg.HasContext("dev"))
	req.False(cfg.HasContext("prod"))
}

func TestGrafanaConfig_IsEmpty(t *testing.T) {
	req := require.New(t)

	req.True(config.GrafanaConfig{}.IsEmpty())
	req.False(config.GrafanaConfig{TLS: &config.TLS{Insecure: true}}.IsEmpty())
	req.False(config.GrafanaConfig{Server: "value"}.IsEmpty())
}

func TestGrafanaConfig_Validate_AllowsDiscoveredStackID(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-12345",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	req.NoError(cfg.Validate("ctx"))
}

func TestGrafanaConfig_Validate_AllowsDiscoveredStackIDAndSuppliedStackID(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-12345",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{
		Server:  server.URL,
		StackID: 12345,
	}
	req.NoError(cfg.Validate("ctx"))
}

func TestGrafanaConfig_Validate_AllowsOrgId(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-12345",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{
		Server: server.URL,
		OrgID:  1,
	}
	req.NoError(cfg.Validate("ctx"))
}

func TestGrafanaConfig_Validate_AllowsOrgIdWhenDiscoveryFails(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{
		Server: server.URL,
		OrgID:  1,
	}
	req.NoError(cfg.Validate("ctx"))
}

func TestGrafanaConfig_Validate_MismatchedStackID(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-12345",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{
		Server:  server.URL,
		StackID: 54321,
	}

	err := cfg.Validate("ctx")
	req.Error(err)
	req.ErrorContains(err, "mismatched")
	req.ErrorContains(err, "contexts.ctx.grafana.stack-id")
}

func TestGrafanaConfig_Validate_MissingStackWhenBootdataUnavailable(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	err := cfg.Validate("ctx")
	req.Error(err)
	req.ErrorContains(err, "missing")
	req.ErrorContains(err, "contexts.ctx.grafana.org-id")
	req.ErrorContains(err, "contexts.ctx.grafana.stack-id")
}

func TestGrafanaConfig_Validate_BootdataUnavailableAndSuppliedStackId(t *testing.T) {
	req := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL, StackID: 5431}

	req.NoError(cfg.Validate("ctx"))
}

func TestContext_WithProviders(t *testing.T) {
	testCases := []struct {
		name     string
		ctx      config.Context
		expected map[string]map[string]string
	}{
		{
			name: "single provider with single key",
			ctx: config.Context{
				Name: "test",
				Providers: map[string]map[string]string{
					"slo": {"token": "slo-token"},
				},
			},
			expected: map[string]map[string]string{
				"slo": {"token": "slo-token"},
			},
		},
		{
			name: "multiple providers with multiple keys",
			ctx: config.Context{
				Name: "test",
				Providers: map[string]map[string]string{
					"slo":    {"token": "slo-token", "url": "https://slo.example.com"},
					"oncall": {"token": "oncall-token"},
				},
			},
			expected: map[string]map[string]string{
				"slo":    {"token": "slo-token", "url": "https://slo.example.com"},
				"oncall": {"token": "oncall-token"},
			},
		},
		{
			name: "nil providers",
			ctx: config.Context{
				Name: "test",
			},
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			req.Equal(tc.expected, tc.ctx.Providers)
		})
	}
}

func TestMinify(t *testing.T) {
	req := require.New(t)

	cfg := config.Config{
		Contexts: map[string]*config.Context{
			"dev": {
				Grafana: &config.GrafanaConfig{
					Server: "dev-server",
				},
			},
			"prod": {
				Grafana: &config.GrafanaConfig{
					Server: "prod-server",
				},
			},
		},
		CurrentContext: "dev",
	}

	minified, err := config.Minify(cfg)
	req.NoError(err)

	req.Equal(config.Config{
		Contexts: map[string]*config.Context{
			"dev": {
				Grafana: &config.GrafanaConfig{
					Server: "dev-server",
				},
			},
		},
		CurrentContext: "dev",
	}, minified)
}

func TestMinify_withNoCurrentContext(t *testing.T) {
	req := require.New(t)

	cfg := config.Config{
		Contexts: map[string]*config.Context{
			"dev": {
				Grafana: &config.GrafanaConfig{
					Server: "dev-server",
				},
			},
			"prod": {
				Grafana: &config.GrafanaConfig{
					Server: "prod-server",
				},
			},
		},
		CurrentContext: "",
	}

	_, err := config.Minify(cfg)
	req.Error(err)
	req.ErrorContains(err, "current-context must be defined")
}

func TestContext_ResolveStackSlug(t *testing.T) {
	testCases := []struct {
		name     string
		ctx      config.Context
		expected string
	}{
		{
			name: "explicit cloud.stack takes precedence over grafana.server derivation",
			ctx: config.Context{
				Cloud:   &config.CloudConfig{Stack: "explicit"},
				Grafana: &config.GrafanaConfig{Server: "https://derived.grafana.net"},
			},
			expected: "explicit",
		},
		{
			name: "derive slug from grafana.net subdomain when no cloud.stack",
			ctx: config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana.net"},
			},
			expected: "mystack",
		},
		{
			name: "non-grafana.net server returns empty string",
			ctx: config.Context{
				Grafana: &config.GrafanaConfig{Server: "https://grafana.mycompany.com"},
			},
			expected: "",
		},
		{
			name:     "no grafana config returns empty string",
			ctx:      config.Context{},
			expected: "",
		},
		{
			name: "empty cloud.stack falls back to grafana.server derivation",
			ctx: config.Context{
				Cloud:   &config.CloudConfig{Stack: ""},
				Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana.net"},
			},
			expected: "mystack",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			req.Equal(tc.expected, tc.ctx.ResolveStackSlug())
		})
	}
}

func TestContext_ResolveGCOMURL(t *testing.T) {
	testCases := []struct {
		name     string
		ctx      config.Context
		expected string
	}{
		{
			name:     "no cloud config returns default grafana.com URL",
			ctx:      config.Context{},
			expected: "https://grafana.com",
		},
		{
			name: "empty cloud.api-url returns default grafana.com URL",
			ctx: config.Context{
				Cloud: &config.CloudConfig{},
			},
			expected: "https://grafana.com",
		},
		{
			name: "custom cloud.api-url is prefixed with https://",
			ctx: config.Context{
				Cloud: &config.CloudConfig{APIUrl: "grafana-dev.com"},
			},
			expected: "https://grafana-dev.com",
		},
		{
			name: "cloud.api-url with existing https:// scheme is not double-prefixed",
			ctx: config.Context{
				Cloud: &config.CloudConfig{APIUrl: "https://grafana-dev.com"},
			},
			expected: "https://grafana-dev.com",
		},
		{
			name: "cloud.api-url with http:// scheme is preserved",
			ctx: config.Context{
				Cloud: &config.CloudConfig{APIUrl: "http://localhost:3000"},
			},
			expected: "http://localhost:3000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			req.Equal(tc.expected, tc.ctx.ResolveGCOMURL())
		})
	}
}

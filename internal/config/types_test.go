package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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

func TestContextNameFromServerURL(t *testing.T) {
	testCases := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "grafana.net URL returns stack slug",
			url:      "https://mystack.grafana.net",
			expected: "mystack",
		},
		{
			name:     "grafana-dev.net URL returns stack slug with -dev suffix",
			url:      "https://mystack.grafana-dev.net",
			expected: "mystack-dev",
		},
		{
			name:     "grafana-ops.net URL returns stack slug with -ops suffix",
			url:      "https://mystack.grafana-ops.net",
			expected: "mystack-ops",
		},
		{
			name:     "regional grafana.net URL returns first component",
			url:      "https://mystack.us.grafana.net",
			expected: "mystack",
		},
		{
			name:     "non-grafana URL returns hyphenated hostname",
			url:      "https://grafana.mycompany.com",
			expected: "grafana-mycompany-com",
		},
		{
			name:     "localhost URL returns hostname",
			url:      "http://localhost:3000",
			expected: "localhost",
		},
		{
			name:     "empty string returns default",
			url:      "",
			expected: "default",
		},
		{
			name:     "unparseable URL returns default",
			url:      "://invalid",
			expected: "default",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			req.Equal(tc.expected, config.ContextNameFromServerURL(tc.url))
		})
	}
}

func TestContextNameFromServerURL_DotsToHyphens(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"cloud prod (unchanged)", "https://mystack.grafana.net", "mystack"},
		{"cloud dev suffix", "https://mystack.grafana-dev.net", "mystack-dev"},
		{"custom domain", "https://grafana.example.com", "grafana-example-com"},
		{"deep custom", "https://grafana.internal.acme.corp", "grafana-internal-acme-corp"},
		{"ipv4 address", "https://10.0.0.5", "10-0-0-5"},
		{"localhost clean", "http://localhost:3000", "localhost"},
		{"bare hostname no dots", "https://grafana", "grafana"},
		{"invalid url returns default", ":not-a-url", config.DefaultContextName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := config.ContextNameFromServerURL(tc.url)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
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

func TestGrafanaConfig_InferredAuthMethod(t *testing.T) {
	testCases := []struct {
		name     string
		cfg      config.GrafanaConfig
		expected string
	}{
		{
			name:     "stored AuthMethod returned verbatim",
			cfg:      config.GrafanaConfig{AuthMethod: "oauth", OAuthToken: "gat_x"},
			expected: "oauth",
		},
		{
			name:     "OAuthToken set infers oauth",
			cfg:      config.GrafanaConfig{OAuthToken: "gat_x"},
			expected: "oauth",
		},
		{
			name:     "APIToken set infers token",
			cfg:      config.GrafanaConfig{APIToken: "glsa_x"},
			expected: "token",
		},
		{
			name:     "OAuthToken takes priority over APIToken",
			cfg:      config.GrafanaConfig{OAuthToken: "gat_x", APIToken: "glsa_x"},
			expected: "oauth",
		},
		{
			name:     "User set infers basic",
			cfg:      config.GrafanaConfig{User: "admin"},
			expected: "basic",
		},
		{
			name:     "Password set infers basic",
			cfg:      config.GrafanaConfig{Password: "secret"},
			expected: "basic",
		},
		{
			name:     "no credentials returns unknown",
			cfg:      config.GrafanaConfig{Server: "https://grafana.example.com"},
			expected: "unknown",
		},
		{
			name:     "empty config returns unknown",
			cfg:      config.GrafanaConfig{},
			expected: "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			req.Equal(tc.expected, tc.cfg.InferredAuthMethod())
		})
	}
}

func TestStackSlugFromServerURL_AppendsEnvSuffix(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantSlug string
		wantOK   bool
	}{
		{"prod bare", "https://mystack.grafana.net", "mystack", true},
		{"prod regional", "https://mystack.us.grafana.net", "mystack", true},
		{"dev appends -dev", "https://mystack.grafana-dev.net", "mystack-dev", true},
		{"ops appends -ops", "https://mystack.grafana-ops.net", "mystack-ops", true},
		{"custom domain no match", "https://grafana.example.com", "", false},
		{"empty slug rejected", "https://.grafana-dev.net", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slug, ok := config.StackSlugFromServerURL(tc.url)
			if slug != tc.wantSlug || ok != tc.wantOK {
				t.Fatalf("got (%q, %v), want (%q, %v)", slug, ok, tc.wantSlug, tc.wantOK)
			}
		})
	}
}

func TestGrafanaConfig_AuthMethod_Roundtrip(t *testing.T) {
	t.Run("auth-method field serializes and deserializes via YAML", func(t *testing.T) {
		req := require.New(t)

		original := config.GrafanaConfig{
			Server:     "https://mystack.grafana.net",
			AuthMethod: "oauth",
		}

		data, err := yaml.Marshal(original)
		req.NoError(err)
		req.Contains(string(data), "auth-method: oauth")

		var loaded config.GrafanaConfig
		req.NoError(yaml.Unmarshal(data, &loaded))
		req.Equal("oauth", loaded.AuthMethod)
	})

	t.Run("legacy YAML without auth-method deserializes with empty AuthMethod", func(t *testing.T) {
		req := require.New(t)

		legacyYAML := []byte("server: https://mystack.grafana.net\ntoken: glsa_abc\n")
		var cfg config.GrafanaConfig
		req.NoError(yaml.Unmarshal(legacyYAML, &cfg))
		req.Empty(cfg.AuthMethod)
	})

	t.Run("omitempty keeps auth-method out of YAML when empty", func(t *testing.T) {
		req := require.New(t)

		cfg := config.GrafanaConfig{Server: "https://mystack.grafana.net"}
		data, err := yaml.Marshal(cfg)
		req.NoError(err)
		req.NotContains(string(data), "auth-method")
	})

	t.Run("auth-method field serializes and deserializes via JSON", func(t *testing.T) {
		req := require.New(t)

		original := config.GrafanaConfig{
			Server:     "https://mystack.grafana.net",
			AuthMethod: "token",
		}

		data, err := json.Marshal(original)
		req.NoError(err)
		req.Contains(string(data), `"auth-method":"token"`)

		var loaded config.GrafanaConfig
		req.NoError(json.Unmarshal(data, &loaded))
		req.Equal("token", loaded.AuthMethod)
	})
}

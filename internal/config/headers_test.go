package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/secrets"
)

// Custom headers carry authentication (e.g. a gateway header for Grafana
// hosted behind a reverse proxy), so malformed ones must fail closed and
// their values must be redacted in `config view` unless --raw.

func TestGrafanaConfig_Validate_RejectsInvalidHeaders(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"empty header name", map[string]string{"": "value"}},
		{"CRLF in header name", map[string]string{"X-A\r\nB": "value"}},
		{"CRLF in header value", map[string]string{"X-A": "value\r\nInjected"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := config.GrafanaConfig{
				Server:   "https://grafana.example.com",
				OrgID:    1,
				APIToken: "glsa_test",
				Headers:  tc.headers,
			}
			if err := g.Validate(t.Context(), "test"); err == nil {
				t.Fatalf("expected validation error for headers %v", tc.headers)
			}
		})
	}
}

// A custom Authorization header would silently conflict with the configured
// auth: it overrides the token/basic credential in token and basic modes, and
// is itself overridden by the OAuth refresh transport. Reject it so auth is
// unambiguous — dedicated auth fields win.
func TestGrafanaConfig_Validate_RejectsAuthorizationHeader(t *testing.T) {
	for _, name := range []string{"Authorization", "authorization", "AUTHORIZATION"} {
		t.Run(name, func(t *testing.T) {
			g := config.GrafanaConfig{
				Server:   "https://grafana.example.com",
				OrgID:    1,
				APIToken: "glsa_test",
				Headers:  map[string]string{name: "Bearer custom"},
			}
			if err := g.Validate(t.Context(), "test"); err == nil {
				t.Fatalf("expected validation error for %q header", name)
			}
		})
	}
}

func TestConfigView_RedactsHeaderValues(t *testing.T) {
	g := config.GrafanaConfig{
		Server:   "https://grafana.example.com",
		APIToken: "glsa_test",
		Headers:  map[string]string{"X-Auth-Token": "secret-value", "X-Empty": ""},
	}
	if err := secrets.Redact(&g); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := g.Headers["X-Auth-Token"], "**REDACTED**"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if got := g.Headers["X-Empty"]; got != "" {
		t.Errorf("empty value should stay empty, got %q", got)
	}
}

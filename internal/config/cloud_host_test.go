package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
)

func TestIsGrafanaCloudHost(t *testing.T) {
	cases := []struct {
		name string
		host string
		want bool
	}{
		// Stack URL suffixes (*.net)
		{"prod stack", "mystack.grafana.net", true},
		{"dev stack", "mystack.grafana-dev.net", true},
		{"ops stack", "mystack.grafana-ops.net", true},
		// Root domain suffixes (*.com)
		{"prod com domain", "mystack.grafana.com", true},
		{"dev com domain", "mystack.grafana-dev.com", true},
		{"ops com domain", "mystack.grafana-ops.com", true},
		// Not-a-subdomain: bare domain names must not match
		{"bare grafana.net — not a subdomain", "grafana.net", false},
		{"bare grafana.com — not a subdomain", "grafana.com", false},
		{"bare grafana-dev.net — not a subdomain", "grafana-dev.net", false},
		// Unrelated domains
		{"example.com", "example.com", false},
		{"internal host", "grafana.mycompany.com", false},
		{"lookalike domain", "not-grafana.net", false},
		// Empty string
		{"empty string", "", false},
		// Case sensitivity: caller is responsible for lowercasing
		{"uppercase does not match", "MYSTACK.GRAFANA.NET", false},
		{"mixed case does not match", "MyStack.Grafana.Net", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := config.IsGrafanaCloudHost(tc.host)
			if got != tc.want {
				t.Fatalf("IsGrafanaCloudHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestGCOMPortalServerURL(t *testing.T) {
	cases := []struct {
		name       string
		serverURL  string
		wantSuffix string
		wantOK     bool
	}{
		// Portal roots: each maps to the stack suffix of its own environment.
		{"prod portal", "https://grafana.com", ".grafana.net", true},
		{"dev portal", "https://grafana-dev.com", ".grafana-dev.net", true},
		{"ops portal", "https://grafana-ops.com", ".grafana-ops.net", true},
		{"portal with a path", "https://grafana.com/orgs/example", ".grafana.net", true},
		{"portal with a port", "https://grafana.com:443", ".grafana.net", true},
		{"portal in uppercase", "https://GRAFANA.COM", ".grafana.net", true},
		{"portal over http", "http://grafana.com", ".grafana.net", true},
		// Stack URLs are the correct input and must pass through.
		{"prod stack", "https://mystack.grafana.net", "", false},
		{"ops stack", "https://mystack.grafana-ops.net", "", false},
		{"regional stack", "https://mystack.us.grafana.net", "", false},
		// A subdomain of a portal root is not a portal root.
		{"portal subdomain", "https://help.grafana.com", "", false},
		// Everything else.
		{"custom cloud domain", "https://mystack.cloud.example.grafana.com", "", false},
		{"on-premises host", "https://grafana.example.com", "", false},
		{"localhost", "http://localhost:3000", "", false},
		{"empty string", "", "", false},
		{"no scheme", "grafana.com", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			suffix, ok := config.GCOMPortalServerURL(tc.serverURL)
			if ok != tc.wantOK {
				t.Fatalf("GCOMPortalServerURL(%q) ok = %v, want %v", tc.serverURL, ok, tc.wantOK)
			}
			if suffix != tc.wantSuffix {
				t.Fatalf("GCOMPortalServerURL(%q) suffix = %q, want %q", tc.serverURL, suffix, tc.wantSuffix)
			}
		})
	}
}

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

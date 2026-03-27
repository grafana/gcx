package auth_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/auth"
)

func TestValidateEndpointURL_AcceptsTrustedDomains(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"grafana.net", "https://mystack.grafana.net"},
		{"grafana-dev.net", "https://mystack.grafana-dev.net"},
		{"grafana-ops.net", "https://mystack.grafana-ops.net"},
		{"localhost", "http://127.0.0.1:3000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := auth.ValidateEndpointURL(tt.endpoint); err != nil {
				t.Fatalf("expected %q to be accepted, got error: %v", tt.endpoint, err)
			}
		})
	}
}

func TestValidateEndpointURL_RejectsUntrustedDomains(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"random domain", "https://evil.example.com"},
		{"http non-local", "http://mystack.grafana.net"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := auth.ValidateEndpointURL(tt.endpoint); err == nil {
				t.Fatalf("expected %q to be rejected", tt.endpoint)
			}
		})
	}
}

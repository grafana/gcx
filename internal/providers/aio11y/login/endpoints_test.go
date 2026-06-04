package login_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y/login"
)

func TestSigilEndpointFromOTLP(t *testing.T) {
	tests := []struct {
		name    string
		otlp    string
		want    string
		wantErr bool
	}{
		{
			name: "prod eu region",
			otlp: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp",
			want: "https://sigil-prod-eu-west-2.grafana.net",
		},
		{
			name: "prod us region",
			otlp: "https://otlp-gateway-prod-us-east-0.grafana.net/otlp",
			want: "https://sigil-prod-us-east-0.grafana.net",
		},
		{
			name: "no path suffix still derives host",
			otlp: "https://otlp-gateway-prod-eu-west-2.grafana.net",
			want: "https://sigil-prod-eu-west-2.grafana.net",
		},
		{
			name:    "empty",
			otlp:    "",
			wantErr: true,
		},
		{
			name:    "not a gateway host",
			otlp:    "https://example.com/otlp",
			wantErr: true,
		},
		{
			name:    "relative url",
			otlp:    "/otlp",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := login.SigilEndpointFromOTLP(tt.otlp)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

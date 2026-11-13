package auth_test

import (
	"testing"

	"github.com/grafana/gcx/internal/auth"
)

func TestValidateGCOMURLHostNormalization(t *testing.T) {
	for _, tc := range []struct {
		url     string
		wantErr bool
	}{
		{"https://GRAFANA.COM", false},
		{"https://Grafana-Dev.COM", false},
		{"http://LocalHost:3000", false},
		{"https://GRAFANA.COM.attacker.com", true},
		{"http://GRAFANA.COM", true},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if err := auth.ValidateGCOMURL(tc.url); (err != nil) != tc.wantErr {
				t.Fatalf("validateGCOMURL(%q) = %v, want error: %v", tc.url, err, tc.wantErr)
			}
		})
	}
}

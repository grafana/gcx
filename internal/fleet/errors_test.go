package fleet_test

import (
	"testing"

	"github.com/grafana/gcx/internal/fleet"
)

// Grafana answers with a different body for each cause, and it does not
// capitalize the messages in the same way. A resource that is absent must not
// match.
func TestIsPluginMissingBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "plugin is not installed",
			body: `{"message":"Plugin not found","traceID":""}`,
			want: true,
		},
		{
			name: "plugin has no route for the path",
			body: `{"message":"plugin route match not found"}`,
			want: true,
		},
		{
			name: "plugin is installed but not enabled",
			body: `{"message":"Plugin is not enabled"}`,
			want: true,
		},
		{
			name: "an absent pipeline is not a missing plugin",
			body: `{"code":"not_found","message":"pipeline not found"}`,
			want: false,
		},
		{
			name: "an absent collector is not a missing plugin",
			body: `{"code":"not_found","message":"collector not found"}`,
			want: false,
		},
		{
			name: "an empty body is not a missing plugin",
			body: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fleet.IsPluginMissingBody(tt.body); got != tt.want {
				t.Fatalf("IsPluginMissingBody(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

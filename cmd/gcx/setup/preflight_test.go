package setup_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup"
)

// The Fleet Management row must name the exact cause, because a user cannot
// act on "unhealthy" alone.
func TestCollectorAppRow(t *testing.T) {
	tests := []struct {
		name         string
		installed    bool
		enabled      bool
		actionsKnown bool
		canRead      bool
		canAdmin     bool
		wantEnabled  bool
		wantHealth   string
		wantDetails  string
	}{
		{
			name:        "plugin absent",
			wantHealth:  "unhealthy",
			wantDetails: "the grafana-collector-app plugin is not installed",
		},
		{
			name:        "plugin installed but disabled",
			installed:   true,
			wantHealth:  "unhealthy",
			wantDetails: "the grafana-collector-app plugin is installed but not enabled",
		},
		{
			name:         "both route actions missing",
			installed:    true,
			enabled:      true,
			actionsKnown: true,
			wantEnabled:  true,
			wantHealth:   "unhealthy",
			wantDetails:  "your login has neither the grafana-collector-app:read nor the grafana-collector-app:admin action",
		},
		{
			name:         "admin route action missing",
			installed:    true,
			enabled:      true,
			actionsKnown: true,
			canRead:      true,
			wantEnabled:  true,
			wantHealth:   "degraded",
			wantDetails:  "limited access; some commands, including read-only commands, need the grafana-collector-app:admin action",
		},
		{
			name:         "named read route action missing",
			installed:    true,
			enabled:      true,
			actionsKnown: true,
			canAdmin:     true,
			wantEnabled:  true,
			wantHealth:   "degraded",
			wantDetails:  "limited access; named read routes need the grafana-collector-app:read action",
		},
		{
			name:        "actions endpoint unavailable",
			installed:   true,
			enabled:     true,
			wantEnabled: true,
			wantHealth:  "unknown",
			wantDetails: "plugin enabled; route permissions unknown",
		},
		{
			name:         "both actions present",
			installed:    true,
			enabled:      true,
			actionsKnown: true,
			canRead:      true,
			canAdmin:     true,
			wantEnabled:  true,
			wantHealth:   "healthy",
			wantDetails:  "read and admin routes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := setup.CollectorRowForTest(tt.installed, tt.enabled, tt.actionsKnown, tt.canRead, tt.canAdmin)
			product, enabled, health, details := setup.StatusRowFieldsForTest(row)

			if product != "fleet-management" {
				t.Fatalf("product = %q, want fleet-management", product)
			}
			if enabled != tt.wantEnabled {
				t.Fatalf("enabled = %v, want %v", enabled, tt.wantEnabled)
			}
			if health != tt.wantHealth {
				t.Fatalf("health = %q, want %q", health, tt.wantHealth)
			}
			if details != tt.wantDetails {
				t.Fatalf("details = %q, want %q", details, tt.wantDetails)
			}
		})
	}
}

// An unknown plugin state must not read as "not installed": a 403 says nothing
// about the plugin.
func TestUnknownPluginRow(t *testing.T) {
	row := setup.UnknownPluginRowForTest(http.StatusForbidden)
	product, enabled, health, details := setup.StatusRowFieldsForTest(row)

	if product != "fleet-management" {
		t.Fatalf("product = %q, want fleet-management", product)
	}
	if enabled {
		t.Fatal("enabled = true, want false")
	}
	if health != "unknown" {
		t.Fatalf("health = %q, want unknown", health)
	}
	if !strings.Contains(details, "HTTP 403") {
		t.Fatalf("details = %q, want the status in the text", details)
	}
}

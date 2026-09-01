package setup_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			name:         "read action missing",
			installed:    true,
			enabled:      true,
			actionsKnown: true,
			wantEnabled:  true,
			wantHealth:   "unhealthy",
			wantDetails:  "your login is missing the grafana-collector-app:read action",
		},
		{
			name:         "admin action missing means read only",
			installed:    true,
			enabled:      true,
			actionsKnown: true,
			canRead:      true,
			wantEnabled:  true,
			wantHealth:   "degraded",
			wantDetails:  "read only; write commands need the grafana-collector-app:admin action",
		},
		{
			name:        "actions endpoint unavailable",
			installed:   true,
			enabled:     true,
			wantEnabled: true,
			wantHealth:  "healthy",
			wantDetails: "plugin enabled; permissions unknown",
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
			wantDetails:  "read and write",
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

// The preflight must treat a 404 from the plugin settings route as "not
// installed", not as a request failure.
func TestCheckCollectorApp(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantHealth  string
		wantDetails string
	}{
		{
			name: "plugin absent",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					w.WriteHeader(http.StatusNotFound)
				case "/api/access-control/user/actions":
					writeJSON(w, map[string]bool{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantHealth:  "unhealthy",
			wantDetails: "the grafana-collector-app plugin is not installed",
		},
		{
			name: "plugin enabled with both actions",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					writeJSON(w, map[string]any{"id": "grafana-collector-app", "enabled": true})
				case "/api/access-control/user/actions":
					writeJSON(w, map[string]bool{
						"grafana-collector-app:read":  true,
						"grafana-collector-app:admin": true,
					})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantHealth:  "healthy",
			wantDetails: "read and write",
		},
		{
			name: "plugin settings forbidden leaves the plugin state unknown",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					w.WriteHeader(http.StatusForbidden)
				case "/api/access-control/user/actions":
					writeJSON(w, map[string]bool{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantHealth:  "unknown",
			wantDetails: "HTTP 403 from /api/plugins/grafana-collector-app/settings; the plugin state is unknown",
		},
		{
			name: "plugin settings server error leaves the plugin state unknown",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					w.WriteHeader(http.StatusInternalServerError)
				case "/api/access-control/user/actions":
					writeJSON(w, map[string]bool{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantHealth:  "unknown",
			wantDetails: "HTTP 500 from /api/plugins/grafana-collector-app/settings; the plugin state is unknown",
		},
		{
			name: "actions endpoint forbidden leaves permissions unknown",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					writeJSON(w, map[string]any{"id": "grafana-collector-app", "enabled": true})
				default:
					w.WriteHeader(http.StatusForbidden)
				}
			},
			wantHealth:  "healthy",
			wantDetails: "plugin enabled; permissions unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			row, err := setup.CheckCollectorAppForTest(context.Background(), server.URL)
			if err != nil {
				t.Fatalf("CheckCollectorAppForTest() = %v", err)
			}
			_, _, health, details := setup.StatusRowFieldsForTest(row)
			if health != tt.wantHealth {
				t.Fatalf("health = %q, want %q", health, tt.wantHealth)
			}
			if details != tt.wantDetails {
				t.Fatalf("details = %q, want %q", details, tt.wantDetails)
			}
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

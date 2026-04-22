//nolint:testpackage // white-box testing: probeTarget is unexported and must be tested from within the package
package login

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsLocalHostname(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		// Loopback (AC-003)
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"foo.localhost", true},
		// RFC 1918 private IPv4
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"192.169.1.1", false},
		// IPv6 ULA
		{"fd00::1", true},
		// Public IPv6 — not local
		{"fe80::1", false},
		{"2001:db8::1", false},
		// NC-009: enterprise-intranet suffixes are NOT local
		{"myhost.local", false},
		{"grafana.internal", false},
		{"host.corp", false},
		{"host.lan", false},
		// Public hostnames
		{"mystack.grafana.net", false},
		{"example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := isLocalHostname(tt.host)
			if got != tt.want {
				t.Errorf("isLocalHostname(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestDetectTargetDomainRouting(t *testing.T) {
	client := &http.Client{}
	tests := []struct {
		name   string
		server string
		want   Target
	}{
		// AC-014: Cloud domain match → Cloud (no probe)
		{"grafana.net", "https://mystack.grafana.net", TargetCloud},
		{"grafana-dev.net", "https://mystack.grafana-dev.net", TargetCloud},
		{"grafana-ops.net", "https://mystack.grafana-ops.net", TargetCloud},
		// AC-003: local hostname → OnPrem (no probe)
		{"localhost port", "http://localhost:3000", TargetOnPrem},
		{"loopback IP", "http://127.0.0.1:3000", TargetOnPrem},
		{"private IP 10.x", "http://10.0.0.1:3000", TargetOnPrem},
		{"private IP 192.168.x", "http://192.168.1.10:3000", TargetOnPrem},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectTarget(context.Background(), tt.server, client)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectTarget(%q) = %v, want %v", tt.server, got, tt.want)
			}
		})
	}
}

func TestProbeTarget(t *testing.T) {
	type settingsResp struct {
		BuildInfo struct {
			GrafanaURL string `json:"grafanaUrl"`
		} `json:"buildInfo"`
	}

	// AC-018: Cloud markers in probe response → Cloud
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp settingsResp
		resp.BuildInfo.GrafanaURL = "https://grafana.com"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer cloudSrv.Close()

	// FR-006c: valid probe + no Cloud markers → OnPrem
	onpremSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resp settingsResp
		resp.BuildInfo.GrafanaURL = "https://mycompany.example.com"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer onpremSrv.Close()

	// non-200 probe → Unknown
	notFoundSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFoundSrv.Close()

	tests := []struct {
		name   string
		server string
		want   Target
	}{
		{"cloud markers", cloudSrv.URL, TargetCloud},
		{"onprem no markers", onpremSrv.URL, TargetOnPrem},
		{"non-200", notFoundSrv.URL, TargetUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := probeTarget(context.Background(), tt.server, &http.Client{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("probeTarget(%q) = %v, want %v", tt.server, got, tt.want)
			}
		})
	}
}

func TestProbeTargetTimeout(t *testing.T) {
	// AC-019: probe timeout → TargetUnknown
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer slowSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	got, err := probeTarget(ctx, slowSrv.URL, &http.Client{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != TargetUnknown {
		t.Errorf("expected TargetUnknown on timeout, got %v", got)
	}
}

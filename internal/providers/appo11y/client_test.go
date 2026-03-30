package appo11y_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/appo11y"
	"github.com/grafana/gcx/internal/providers/appo11y/overrides"
	"github.com/grafana/gcx/internal/providers/appo11y/settings"
	k8srest "k8s.io/client-go/rest"
)

const (
	testOverridesPath = "/api/plugin-proxy/grafana-app-observability-app/overrides"
	testSettingsPath  = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"
)

// newTestClient creates a Client pointed at the given test server URL.
func newTestClient(t *testing.T, serverURL string) *appo11y.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{}
	cfg.Config = k8srest.Config{Host: serverURL}
	client, err := appo11y.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestGetOverrides(t *testing.T) {
	cfg := overrides.MetricsGeneratorConfig{
		CostAttribution: map[string]any{"env": "prod"},
		MetricsGenerator: &overrides.MetricsGenerator{
			DisableCollection:  false,
			CollectionInterval: "60s",
			Processor: &overrides.Processor{
				ServiceGraphs: &overrides.ServiceGraphs{Dimensions: []string{"service.name"}},
				SpanMetrics:   &overrides.SpanMetrics{Dimensions: []string{"http.method"}},
			},
		},
	}

	tests := []struct {
		name           string
		responseStatus int
		responseBody   any
		responseETag   string
		wantErr        string
		wantETag       string
	}{
		{
			name:           "success with etag",
			responseStatus: http.StatusOK,
			responseBody:   cfg,
			responseETag:   `"abc123"`,
			wantETag:       `"abc123"`,
		},
		{
			name:           "success without etag",
			responseStatus: http.StatusOK,
			responseBody:   cfg,
			responseETag:   "",
			wantETag:       "",
		},
		{
			name:           "404 plugin not installed",
			responseStatus: http.StatusNotFound,
			responseBody:   map[string]string{"message": "not found"},
			wantErr:        "Grafana App Observability plugin is not installed or not enabled",
		},
		{
			name:           "500 server error",
			responseStatus: http.StatusInternalServerError,
			responseBody:   map[string]string{"error": "internal error"},
			wantErr:        "request failed with status 500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != testOverridesPath {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				if tc.responseETag != "" {
					w.Header().Set("ETag", tc.responseETag)
				}
				w.WriteHeader(tc.responseStatus)
				_ = json.NewEncoder(w).Encode(tc.responseBody)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			got, err := client.GetOverrides(context.Background())

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if errStr := err.Error(); len(errStr) == 0 || !strings.Contains(errStr, tc.wantErr) {
					t.Errorf("error %q does not contain %q", errStr, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ETag() != tc.wantETag {
				t.Errorf("ETag = %q, want %q", got.ETag(), tc.wantETag)
			}
		})
	}
}

func TestUpdateOverrides(t *testing.T) {
	cfg := &overrides.MetricsGeneratorConfig{
		MetricsGenerator: &overrides.MetricsGenerator{
			DisableCollection: true,
		},
	}

	tests := []struct {
		name           string
		etag           string
		responseStatus int
		wantIfMatch    string
		wantNoIfMatch  bool
		wantErr        string
	}{
		{
			name:           "with etag sends If-Match header",
			etag:           `"etag-value"`,
			responseStatus: http.StatusOK,
			wantIfMatch:    `"etag-value"`,
		},
		{
			name:           "empty etag omits If-Match header",
			etag:           "",
			responseStatus: http.StatusOK,
			wantNoIfMatch:  true,
		},
		{
			name:           "412 concurrent modification",
			etag:           `"stale"`,
			responseStatus: http.StatusPreconditionFailed,
			wantErr:        "concurrent modification conflict",
		},
		{
			name:           "404 plugin not installed",
			etag:           "",
			responseStatus: http.StatusNotFound,
			wantErr:        "Grafana App Observability plugin is not installed or not enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != testOverridesPath {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}

				gotIfMatch := r.Header.Get("If-Match")
				if tc.wantIfMatch != "" && gotIfMatch != tc.wantIfMatch {
					t.Errorf("If-Match = %q, want %q", gotIfMatch, tc.wantIfMatch)
				}
				if tc.wantNoIfMatch && gotIfMatch != "" {
					t.Errorf("expected no If-Match header, got %q", gotIfMatch)
				}

				w.WriteHeader(tc.responseStatus)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			err := client.UpdateOverrides(context.Background(), cfg, tc.etag)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetSettings(t *testing.T) {
	s := settings.PluginSettings{
		JSONData: settings.PluginJSONData{
			DefaultLogQueryMode:       "loki",
			LogsQueryWithNamespace:    "namespace_query",
			LogsQueryWithoutNamespace: "no_namespace_query",
			MetricsMode:               "classic",
		},
	}

	tests := []struct {
		name           string
		responseStatus int
		responseBody   any
		wantErr        string
	}{
		{
			name:           "success",
			responseStatus: http.StatusOK,
			responseBody:   s,
		},
		{
			name:           "404 plugin not installed",
			responseStatus: http.StatusNotFound,
			responseBody:   map[string]string{"message": "not found"},
			wantErr:        "Grafana App Observability plugin is not installed or not enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != testSettingsPath {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tc.responseStatus)
				_ = json.NewEncoder(w).Encode(tc.responseBody)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			got, err := client.GetSettings(context.Background())

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.JSONData.DefaultLogQueryMode != s.JSONData.DefaultLogQueryMode {
				t.Errorf("DefaultLogQueryMode = %q, want %q", got.JSONData.DefaultLogQueryMode, s.JSONData.DefaultLogQueryMode)
			}
		})
	}
}

func TestUpdateSettings(t *testing.T) {
	s := &settings.PluginSettings{
		JSONData: settings.PluginJSONData{MetricsMode: "otel"},
	}

	tests := []struct {
		name           string
		responseStatus int
		wantErr        string
	}{
		{
			name:           "success",
			responseStatus: http.StatusOK,
		},
		{
			name:           "no If-Match header sent",
			responseStatus: http.StatusOK,
		},
		{
			name:           "404 plugin not installed",
			responseStatus: http.StatusNotFound,
			wantErr:        "Grafana App Observability plugin is not installed or not enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != testSettingsPath {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
					t.Errorf("settings update must not send If-Match header, got %q", ifMatch)
				}
				w.WriteHeader(tc.responseStatus)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			err := client.UpdateSettings(context.Background(), s)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

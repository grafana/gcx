package grafana_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/grafana"
)

func TestFetchAnonymousSettings(t *testing.T) {
	t.Parallel()

	validSettings := grafana.FrontendSettings{
		BuildInfo: grafana.BuildInfo{
			GrafanaURL: "https://example.grafana.net",
		},
	}

	validBody, err := json.Marshal(validSettings)
	if err != nil {
		t.Fatalf("failed to marshal test settings: %v", err)
	}

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantErr   bool
		wantURL   string // non-empty: check BuildInfo.GrafanaURL
		cancelCtx bool
	}{
		{
			name: "success path returns populated settings",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/frontend/settings" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(validBody)
			},
			wantErr: false,
			wantURL: "https://example.grafana.net",
		},
		{
			name: "404 returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			},
			wantErr: true,
		},
		{
			name: "malformed JSON body returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{not valid json`))
			},
			wantErr: true,
		},
		{
			name: "cancelled context returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Serve a valid response; the context will already be cancelled.
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(validBody)
			},
			wantErr:   true,
			cancelCtx: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			ctx := context.Background()
			if tc.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // cancel immediately before the call
			}

			got, err := grafana.FetchAnonymousSettings(ctx, srv.URL, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (settings=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil settings, got nil")
			}
			if tc.wantURL != "" && got.BuildInfo.GrafanaURL != tc.wantURL {
				t.Errorf("BuildInfo.GrafanaURL = %q, want %q", got.BuildInfo.GrafanaURL, tc.wantURL)
			}
		})
	}
}

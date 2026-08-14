package irm_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
)

func TestSyncPluginRequest(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"message":"Sync request processed successfully"}`) //nolint:errcheck
	}))

	if _, err := client.SyncPlugin(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("got method %q, want POST", gotMethod)
	}
	wantPath := irm.BasePath + "/plugin/sync"
	if gotPath != wantPath {
		t.Errorf("got path %q, want %q", gotPath, wantPath)
	}
}

// TestSyncPluginResponses covers the answers that the endpoint can send.
// Nobody verified the real shape, so the client accepts every 2xx status code,
// tolerates an empty body, and fails only on an explicit in-band error field.
func TestSyncPluginResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		body        string
		wantErr     string
		wantMessage string
	}{
		{
			name:        "200 with a JSON body",
			status:      http.StatusOK,
			body:        `{"message":"Sync request processed successfully","status":"success"}`,
			wantMessage: "Sync request processed successfully",
		},
		{
			name:   "200 with an empty body",
			status: http.StatusOK,
		},
		{
			name:   "202 with an empty body",
			status: http.StatusAccepted,
		},
		{
			name:   "204 with no content",
			status: http.StatusNoContent,
		},
		{
			name:        "200 with an unknown status token",
			status:      http.StatusOK,
			body:        `{"message":"sync queued","status":"queued"}`,
			wantMessage: "sync queued",
		},
		{
			name:    "200 with an in-band error field",
			status:  http.StatusOK,
			body:    `{"error":"sync is already running"}`,
			wantErr: "sync is already running",
		},
		{
			name:    "403 with a backend message",
			status:  http.StatusForbidden,
			body:    `{"detail":"permission denied"}`,
			wantErr: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.body != "" {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body) //nolint:errcheck
			}))

			got, err := client.SyncPlugin(context.Background())
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error that contains %q, got none", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected an error that contains %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("got message %q, want %q", got.Message, tt.wantMessage)
			}
		})
	}
}

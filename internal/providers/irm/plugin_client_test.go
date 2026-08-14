package irm_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
)

func TestSyncPlugin(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"message":"Sync request processed successfully","status":"success"}`) //nolint:errcheck
	}))

	got, err := client.SyncPlugin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("got method %q, want POST", gotMethod)
	}
	wantPath := irm.BasePath + "/plugin/sync"
	if gotPath != wantPath {
		t.Errorf("got path %q, want %q", gotPath, wantPath)
	}
	if got.Status != "success" || got.Message != "Sync request processed successfully" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// TestSyncPluginInBandFailure covers a backend that reports failure on a 200
// response, the way the incident query endpoint does.
func TestSyncPluginInBandFailure(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"message":"sync is already running","status":"error"}`) //nolint:errcheck
	}))

	_, err := client.SyncPlugin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sync is already running") {
		t.Errorf("expected the in-band failure message, got %v", err)
	}
}

func TestSyncPluginHTTPFailure(t *testing.T) {
	t.Parallel()

	client := newTestOnCallClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"detail":"permission denied"}`) //nolint:errcheck
	}))

	_, err := client.SyncPlugin(context.Background())
	if err == nil {
		t.Fatal("expected an error on a 403 response")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected the backend message in the error, got %v", err)
	}
}

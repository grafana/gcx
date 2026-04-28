package search_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/dashboards/search"
	"k8s.io/client-go/rest"
)

// stubLoader is a test double for search.GrafanaConfigLoader.
type stubLoader struct {
	cfg config.NamespacedRESTConfig
}

func (s *stubLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return s.cfg, nil
}

// testServerResponse is the JSON structure returned by the mock search server.
// Mirrors wireSearchResponse so tests can construct responses without accessing
// unexported types.
type testServerResponse struct {
	Hits      []testServerHit `json:"hits"`
	MaxScore  float64         `json:"maxScore"`
	QueryCost int64           `json:"queryCost"`
	TotalHits int64           `json:"totalHits"`
}

type testServerHit struct {
	Resource string   `json:"resource"`
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Folder   string   `json:"folder"`
	Tags     []string `json:"tags"`
}

// newTestServer starts a test HTTP server that handles exactly one GET /…/search
// request. The handler writes the given response as JSON.
// The caller is responsible for calling server.Close().
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *stubLoader) {
	t.Helper()
	srv := httptest.NewServer(handler)
	loader := &stubLoader{
		cfg: config.NamespacedRESTConfig{
			Config:    rest.Config{Host: srv.URL},
			Namespace: "stacks-test",
		},
	}
	return srv, loader
}

// runSearchCommand executes the search cobra command with the given args and
// returns stdout as a string, plus any error.
func runSearchCommand(t *testing.T, loader search.GrafanaConfigLoader, args ...string) (string, error) {
	t.Helper()
	cmd := search.Commands(loader)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// writeJSONResponse encodes v as JSON to w with 200 OK.
func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSearch_ServerSideTypeFilter verifies that the client sends type=dashboard and
// that all returned hits are rendered (server is responsible for filtering folders).
func TestSearch_ServerSideTypeFilter(t *testing.T) {
	var capturedQuery string
	resp := testServerResponse{
		Hits: []testServerHit{
			{Resource: "dashboards", Name: "dash-1", Title: "My Dashboard", Folder: "", Tags: []string{"prod"}},
			{Resource: "dashboards", Name: "dash-2", Title: "Another Dashboard", Folder: "my-folder", Tags: nil},
		},
	}

	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		writeJSONResponse(w, resp)
	})
	defer srv.Close()

	// Use -o table explicitly so agent-mode JSON default doesn't interfere.
	output, err := runSearchCommand(t, loader, "test", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// type=dashboard must be sent to the server.
	if !strings.Contains(capturedQuery, "type=dashboard") {
		t.Errorf("expected 'type=dashboard' in query string, got: %s", capturedQuery)
	}

	// Both dashboard hits must be present.
	if !strings.Contains(output, "dash-1") {
		t.Errorf("expected 'dash-1' in output:\n%s", output)
	}
	if !strings.Contains(output, "dash-2") {
		t.Errorf("expected 'dash-2' in output:\n%s", output)
	}
}

// TestSearch_YAMLEnvelopeShape verifies the K8s envelope structure in YAML output.
func TestSearch_YAMLEnvelopeShape(t *testing.T) {
	resp := testServerResponse{
		Hits: []testServerHit{
			{Resource: "dashboards", Name: "dash-uid", Title: "Envelope Test", Folder: "folder-uid", Tags: []string{"tag1"}},
		},
	}

	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, resp)
	})
	defer srv.Close()

	output, err := runSearchCommand(t, loader, "test", "-o", "yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// K8s envelope kind / apiVersion.
	if !strings.Contains(output, "kind: DashboardSearchResultList") {
		t.Errorf("expected 'kind: DashboardSearchResultList' in YAML:\n%s", output)
	}
	if !strings.Contains(output, "apiVersion: dashboard.grafana.app/v0alpha1") {
		t.Errorf("expected apiVersion in YAML:\n%s", output)
	}

	// Item envelope.
	if !strings.Contains(output, "kind: DashboardHit") {
		t.Errorf("expected 'kind: DashboardHit' in YAML:\n%s", output)
	}

	// metadata.name and spec fields.
	if !strings.Contains(output, "name: dash-uid") {
		t.Errorf("expected 'name: dash-uid' in YAML:\n%s", output)
	}
	if !strings.Contains(output, "title: Envelope Test") {
		t.Errorf("expected title in spec:\n%s", output)
	}
	if !strings.Contains(output, "folder: folder-uid") {
		t.Errorf("expected folder in spec:\n%s", output)
	}
	if !strings.Contains(output, "tag1") {
		t.Errorf("expected tags in spec:\n%s", output)
	}
}

// TestSearch_TypeDashboardQueryParam verifies that type=dashboard is always sent
// so the server filters folders server-side.
func TestSearch_TypeDashboardQueryParam(t *testing.T) {
	var capturedQuery string

	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		writeJSONResponse(w, testServerResponse{})
	})
	defer srv.Close()

	_, _ = runSearchCommand(t, loader, "test")

	if !strings.Contains(capturedQuery, "type=dashboard") {
		t.Errorf("query string must contain 'type=dashboard', got: %s", capturedQuery)
	}
}

// TestSearch_URLContainsV0Alpha1Path verifies the search path contains the
// literal API path.
func TestSearch_URLContainsV0Alpha1Path(t *testing.T) {
	var capturedPath string

	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		writeJSONResponse(w, testServerResponse{})
	})
	defer srv.Close()

	_, _ = runSearchCommand(t, loader, "test")

	want := "/apis/dashboard.grafana.app/v0alpha1/namespaces/"
	if !strings.Contains(capturedPath, want) {
		t.Errorf("expected path to contain %q, got: %s", want, capturedPath)
	}
}

// TestSearch_RepeatedFolderAndTagFlags verifies that repeated --folder and --tag
// flags encode as multiple query parameters.
func TestSearch_RepeatedFolderAndTagFlags(t *testing.T) {
	var capturedQuery string

	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		writeJSONResponse(w, testServerResponse{})
	})
	defer srv.Close()

	_, err := runSearchCommand(t, loader,
		"test",
		"--folder", "folder-a",
		"--folder", "folder-b",
		"--tag", "tag1",
		"--tag", "tag2",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both folder values must appear.
	if !strings.Contains(capturedQuery, "folder=folder-a") {
		t.Errorf("expected 'folder=folder-a' in query: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "folder=folder-b") {
		t.Errorf("expected 'folder=folder-b' in query: %s", capturedQuery)
	}

	// Both tag values must appear.
	if !strings.Contains(capturedQuery, "tag=tag1") {
		t.Errorf("expected 'tag=tag1' in query: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "tag=tag2") {
		t.Errorf("expected 'tag=tag2' in query: %s", capturedQuery)
	}
}

// TestSearch_EmptyQueryWithFilter verifies that an empty positional query
// combined with at least one filter does not cause a parse error.
func TestSearch_EmptyQueryWithFilter(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "folder filter only",
			args: []string{"--folder", "some-folder-uid"},
		},
		{
			name: "tag filter only",
			args: []string{"--tag", "production"},
		},
		{
			name: "both folder and tag",
			args: []string{"--folder", "f1", "--tag", "t1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSONResponse(w, testServerResponse{})
			})
			defer srv.Close()

			_, err := runSearchCommand(t, loader, tt.args...)
			if err != nil {
				t.Errorf("expected no error with args %v, got: %v", tt.args, err)
			}
		})
	}
}

// TestSearch_EmptyQueryNoFilter verifies that an empty query without any filter
// returns an error (prevents unbounded searches).
func TestSearch_EmptyQueryNoFilter(t *testing.T) {
	loader := &stubLoader{
		cfg: config.NamespacedRESTConfig{
			Config:    rest.Config{Host: "http://localhost:9999"},
			Namespace: "test",
		},
	}

	_, err := runSearchCommand(t, loader)
	if err == nil {
		t.Error("expected error for empty query without filter, got nil")
	}
}

// TestSearch_APIVersionRejected verifies that --api-version is rejected with
// a non-zero exit and a clear message.
func TestSearch_APIVersionRejected(t *testing.T) {
	loader := &stubLoader{
		cfg: config.NamespacedRESTConfig{
			Config:    rest.Config{Host: "http://localhost:9999"},
			Namespace: "test",
		},
	}

	_, err := runSearchCommand(t, loader, "test", "--api-version", "v2")
	if err == nil {
		t.Fatal("expected error when --api-version is supplied, got nil")
	}

	if !strings.Contains(err.Error(), "--api-version") {
		t.Errorf("error message should mention '--api-version', got: %v", err)
	}
	if !strings.Contains(err.Error(), "v0alpha1") {
		t.Errorf("error message should mention 'v0alpha1', got: %v", err)
	}
}

// TestSearch_QueryParamEncoding verifies that --limit and --sort flags are
// forwarded to the search request as the correct query parameters.
func TestSearch_QueryParamEncoding(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantParam string
	}{
		{
			name:      "limit flag",
			args:      []string{"test", "--limit", "5"},
			wantParam: "limit=5",
		},
		{
			name:      "sort flag",
			args:      []string{"test", "--sort", "name_sort"},
			wantParam: "sort=name_sort",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedQuery string

			srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				capturedQuery = r.URL.RawQuery
				writeJSONResponse(w, testServerResponse{})
			})
			defer srv.Close()

			_, err := runSearchCommand(t, loader, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(capturedQuery, tt.wantParam) {
				t.Errorf("expected %q in query string, got: %s", tt.wantParam, capturedQuery)
			}
		})
	}
}

// TestSearch_HTTP500ErrorPath verifies that a 500 response from the search
// server causes the command to exit with a non-zero error that includes both
// the HTTP status and the response body in the message.
func TestSearch_HTTP500ErrorPath(t *testing.T) {
	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backend exploded", http.StatusInternalServerError)
	})
	defer srv.Close()

	_, err := runSearchCommand(t, loader, "test")
	if err == nil {
		t.Fatal("expected error for HTTP 500 response, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "500") {
		t.Errorf("error message should contain '500', got: %s", msg)
	}
	if !strings.Contains(msg, "backend exploded") {
		t.Errorf("error message should contain response body 'backend exploded', got: %s", msg)
	}
}

// TestSearch_MalformedJSONResponse verifies that a 200 response with invalid
// JSON body causes the command to exit with a non-zero error mentioning the
// decode failure.
func TestSearch_MalformedJSONResponse(t *testing.T) {
	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json"))
	})
	defer srv.Close()

	_, err := runSearchCommand(t, loader, "test")
	if err == nil {
		t.Fatal("expected error for malformed JSON response, got nil")
	}

	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error message should contain 'decode', got: %s", err.Error())
	}
}

// TestSearch_TableOutput verifies that the default table output contains
// expected columns.
func TestSearch_TableOutput(t *testing.T) {
	resp := testServerResponse{
		Hits: []testServerHit{
			{Resource: "dashboards", Name: "d1", Title: "Dashboard One", Folder: "my-folder", Tags: []string{"a", "b"}},
		},
	}

	srv, loader := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, resp)
	})
	defer srv.Close()

	// Use -o table explicitly so agent-mode JSON default doesn't interfere.
	output, err := runSearchCommand(t, loader, "test", "-o", "table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantStrings := []string{"NAME", "TITLE", "FOLDER", "TAGS", "AGE", "d1", "Dashboard One", "my-folder", "a", "b"}
	for _, want := range wantStrings {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in table output:\n%s", want, output)
		}
	}

	// AGE must render as "-" since search hits have no timestamp.
	if !strings.Contains(output, "-") {
		t.Errorf("expected AGE to render as '-':\n%s", output)
	}
}

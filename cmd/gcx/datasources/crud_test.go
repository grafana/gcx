package datasources_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type crudCalls struct {
	create, update, del int
}

// newCRUDServer returns a fake Grafana that serves the legacy /api/datasources
// REST API backed by an in-memory store. GET /apis returns 404 so the dual-mode
// client selects the legacy REST transport.
func newCRUDServer(t *testing.T, store map[string]map[string]any, calls *crudCalls) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
	applySecure := func(ds map[string]any) {
		if sjd, ok := ds["secureJsonData"].(map[string]any); ok && len(sjd) > 0 {
			fields := map[string]any{}
			for k := range sjd {
				fields[k] = true
			}
			ds["secureJsonFields"] = fields
		}
		delete(ds, "secureJsonData") // never returned on read
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/apis" || path == "/bootdata":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && path == "/api/datasources":
			calls.create++
			var ds map[string]any
			if err := json.NewDecoder(r.Body).Decode(&ds); err != nil {
				t.Errorf("decode create body: %v", err)
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			uid, _ := ds["uid"].(string)
			if uid == "" {
				uid = "generated-uid"
				ds["uid"] = uid
			}
			applySecure(ds)
			store[uid] = ds
			writeJSON(w, map[string]any{"datasource": ds, "id": 1, "message": "created"})
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/datasources/uid/"):
			calls.update++
			uid := strings.TrimPrefix(path, "/api/datasources/uid/")
			var ds map[string]any
			if err := json.NewDecoder(r.Body).Decode(&ds); err != nil {
				t.Errorf("decode update body: %v", err)
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			ds["uid"] = uid
			applySecure(ds)
			store[uid] = ds
			writeJSON(w, map[string]any{"datasource": ds, "id": 1, "message": "updated"})
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/datasources/uid/"):
			calls.del++
			uid := strings.TrimPrefix(path, "/api/datasources/uid/")
			if _, ok := store[uid]; !ok {
				http.Error(w, `{"message":"Data source not found"}`, http.StatusNotFound)
				return
			}
			delete(store, uid)
			writeJSON(w, map[string]any{"id": 1, "message": "Data source deleted"})
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/health"):
			writeJSON(w, map[string]any{"status": "OK", "message": "OK"})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/datasources/uid/"):
			uid := strings.TrimPrefix(path, "/api/datasources/uid/")
			ds, ok := store[uid]
			if !ok {
				http.Error(w, `{"message":"Data source not found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, ds)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
}

func executeWithStdin(t *testing.T, stdin string, args []string) (string, error) {
	t.Helper()
	root := helperRoot(datasources.Command())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
	}
	return stdout.String(), err
}

const mockManifest = `{"apiVersion":"grafana-mock-datasource.datasource.grafana.app/v0alpha1",` +
	`"kind":"DataSource","metadata":{"name":"gcx-test"},` +
	`"spec":{"type":"grafana-mock-datasource","title":"gcx test","access":"proxy","url":"https://example.test/"},` +
	`"secure":{"apiToken":{"create":"super-secret"}}}`

func TestCreateThenGetRoundTrip(t *testing.T) {
	store := map[string]map[string]any{}
	calls := &crudCalls{}
	server := newCRUDServer(t, store, calls)
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	stdout, err := executeWithStdin(t, mockManifest,
		[]string{"datasources", "create", "--config", cfg, "-f", "-", "-o", "yaml"})
	require.NoError(t, err)
	assert.Equal(t, 1, calls.create)
	assert.Contains(t, stdout, "gcx test")
	// Secret value must never appear in output.
	assert.NotContains(t, stdout, "super-secret")
	// The resolved secret was sent (recorded as a secure field on the server).
	fields, _ := store["gcx-test"]["secureJsonFields"].(map[string]any)
	assert.Equal(t, true, fields["apiToken"])

	// Round-trip: get -o yaml emits an apply-ready manifest (no secret value).
	out, err := executeDatasourceCommand(t,
		[]string{"datasources", "get", "--config", cfg, "gcx-test", "-o", "yaml"})
	require.NoError(t, err)
	assert.Contains(t, out, "url: https://example.test/")
	assert.Contains(t, out, "apiToken")
	assert.NotContains(t, out, "super-secret")
}

func TestCreateDryRunDoesNotWrite(t *testing.T) {
	store := map[string]map[string]any{}
	calls := &crudCalls{}
	server := newCRUDServer(t, store, calls)
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	stdout, err := executeWithStdin(t, mockManifest,
		[]string{"datasources", "create", "--config", cfg, "-f", "-", "--dry-run", "-o", "yaml"})
	require.NoError(t, err)
	assert.Equal(t, 0, calls.create, "dry-run must not POST")
	assert.NotContains(t, stdout, "super-secret")
	assert.Contains(t, stdout, "<redacted>")
}

func TestDeleteBatchPartialFailure(t *testing.T) {
	store := map[string]map[string]any{
		"exists": {"uid": "exists", "name": "exists", "type": "grafana-mock-datasource"},
	}
	calls := &crudCalls{}
	server := newCRUDServer(t, store, calls)
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	stdout, err := executeDatasourceCommand(t,
		[]string{"datasources", "delete", "--config", cfg, "exists", "missing", "--yes", "-o", "json"})

	// Partial failure → exit code 4.
	var pf *gcxerrors.PartialFailureError
	require.ErrorAs(t, err, &pf)
	assert.Equal(t, 2, calls.del, "delete is attempted per UID")
	assert.NotContains(t, store, "exists", "the existing datasource is deleted")
	assert.Contains(t, stdout, "deleted")
	assert.Contains(t, stdout, "failed")
}

// TestGetTextOutput exercises the default `datasources get` text format,
// which now renders the human detail view from the same DataSourceManifest
// shape used by -o yaml/json (Pattern 13: format-agnostic data acquisition,
// codec controls display only).
func TestGetTextOutput(t *testing.T) {
	store := map[string]map[string]any{}
	calls := &crudCalls{}
	server := newCRUDServer(t, store, calls)
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	// Create the datasource via the manifest path so the server-side store is
	// populated identically to the round-trip test.
	_, err := executeWithStdin(t, mockManifest,
		[]string{"datasources", "create", "--config", cfg, "-f", "-", "-o", "yaml"})
	require.NoError(t, err)
	require.Equal(t, 1, calls.create)

	// Default -o text uses the manifest-backed table codec; assert the human
	// detail view renders the expected FIELD/VALUE rows and never leaks secrets.
	out, err := executeDatasourceCommand(t,
		[]string{"datasources", "get", "--config", cfg, "gcx-test"})
	require.NoError(t, err)

	assert.Contains(t, out, "FIELD")
	assert.Contains(t, out, "VALUE")
	assert.Contains(t, out, "UID")
	assert.Contains(t, out, "gcx-test")
	assert.Contains(t, out, "Name")
	assert.Contains(t, out, "gcx test")
	assert.Contains(t, out, "Type")
	assert.Contains(t, out, "grafana-mock-datasource")
	assert.Contains(t, out, "URL")
	assert.Contains(t, out, "https://example.test/")
	assert.Contains(t, out, "Access")
	assert.Contains(t, out, "proxy")
	assert.Contains(t, out, "BasicAuth")
	assert.Contains(t, out, "WithCredentials")

	// Secret value must never appear in any format.
	assert.NotContains(t, out, "super-secret")
}

func TestHealthHealthy(t *testing.T) {
	store := map[string]map[string]any{
		"ok": {"uid": "ok", "name": "ok", "type": "grafana-mock-datasource"},
	}
	server := newCRUDServer(t, store, &crudCalls{})
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	stdout, err := executeDatasourceCommand(t,
		[]string{"datasources", "health", "--config", cfg, "ok", "-o", "json"})
	require.NoError(t, err)
	assert.Contains(t, strings.ToUpper(stdout), "OK")
}

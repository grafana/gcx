package faro //nolint:testpackage // Drives the unexported list command through the loader seam.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runEmptyAppsList runs `apps list` with the given arguments against a server
// that answers an empty app list, and returns stdout and the run error.
func runEmptyAppsList(t *testing.T, args ...string) (string, error) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(basePath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`[]`))
		assert.NoError(t, err)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cmd := newListCommand(&fakeConfigLoader{grafanaURL: server.URL, faroAPIURL: server.URL})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), err
}

// TestAppsList_JSONPromotedFieldOnEmptyList pins the promoted keys of
// adapter.TypedObject, which embeds metav1.TypeMeta with an inline json tag.
// encoding/json writes kind and apiVersion into the parent object, so both are
// top-level keys. The type walk once read the name of the embedded type as the
// key, so the command rejected "kind" on an empty list.
func TestAppsList_JSONPromotedFieldOnEmptyList(t *testing.T) {
	stdout, err := runEmptyAppsList(t, "--json", "kind")

	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout: %s", stdout)
	assert.Empty(t, got)
}

// TestAppsList_JSONListNamesPromotedFields pins that discovery advertises the
// same keys that selection accepts. It once advertised "TypeMeta", which
// encoding/json never writes as a key.
func TestAppsList_JSONListNamesPromotedFields(t *testing.T) {
	stdout, err := runEmptyAppsList(t, "--json", "list")

	require.NoError(t, err)
	assert.Contains(t, stdout, "kind")
	assert.Contains(t, stdout, "apiVersion")
	assert.NotContains(t, stdout, "TypeMeta")
}

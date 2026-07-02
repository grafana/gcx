package datasources_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPluginsServer serves the Grafana plugins listing used for datasource type
// discovery and `schemas get` type validation.
func newPluginsServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins":
			assert.Equal(t, "datasource", r.URL.Query().Get("type"))
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode([]map[string]any{
				{"id": "prometheus", "name": "Prometheus", "category": "tsdb", "type": "datasource"},
				{"id": "loki", "name": "Loki", "category": "logging", "type": "datasource"},
			}); err != nil {
				t.Errorf("encode plugins response: %v", err)
			}
			return
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
}

func TestSchemasList(t *testing.T) {
	server := newPluginsServer(t)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{
		"datasources", "schemas", "list", "--config", configFile, "-o", "json",
	})
	require.NoError(t, err)

	var result struct {
		Types []map[string]any `json:"types"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Len(t, result.Types, 2)
	// Sorted by plugin id: loki before prometheus.
	assert.Equal(t, "loki", result.Types[0]["id"])
	assert.Equal(t, "prometheus", result.Types[1]["id"])
}

func TestSchemasGet_KnownTypeSucceeds(t *testing.T) {
	server := newPluginsServer(t)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{
		"datasources", "schemas", "get", "--type", "prometheus", "--config", configFile, "-o", "json",
	})
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &schema))
	assert.Contains(t, schema, "properties")
}

func TestSchemasGet_UnknownTypeFails(t *testing.T) {
	server := newPluginsServer(t)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	_, err := executeDatasourceCommand(t, []string{
		"datasources", "schemas", "get", "--type", "does-not-exist", "--config", configFile,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown datasource plugin type")
}

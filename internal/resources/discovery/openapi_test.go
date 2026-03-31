package discovery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// sampleOpenAPIIndex returns an OpenAPI v3 index with one group/version.
func sampleOpenAPIIndex(baseURL string) map[string]any {
	return map[string]any{
		"paths": map[string]any{
			"apis/alerting.test.app/v1": map[string]any{
				"serverRelativeURL": baseURL + "/openapi/v3/apis/alerting.test.app/v1?hash=TEST_HASH_123",
			},
		},
	}
}

// sampleOpenAPIDoc returns a minimal OpenAPI v3 document with a typed resource schema.
func sampleOpenAPIDoc() map[string]any {
	return map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"com.test.AlertRule": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"apiVersion": map[string]any{"type": "string"},
						"kind":       map[string]any{"type": "string"},
						"metadata":   map[string]any{"type": "object"},
						"spec": map[string]any{
							"allOf": []any{
								map[string]any{"$ref": "#/components/schemas/com.test.AlertRuleSpec"},
							},
							"description": "The spec of the AlertRule",
						},
					},
					"x-kubernetes-group-version-kind": []any{
						map[string]any{
							"group":   "alerting.test.app",
							"kind":    "AlertRule",
							"version": "v1",
						},
					},
				},
				"com.test.AlertRuleSpec": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string"},
						"annotations": map[string]any{"type": "object"},
						"for":         map[string]any{"type": "string"},
					},
				},
				// A list type — should not appear in results.
				"com.test.AlertRuleList": map[string]any{
					"type": "object",
					"x-kubernetes-group-version-kind": []any{
						map[string]any{
							"group":   "alerting.test.app",
							"kind":    "AlertRuleList",
							"version": "v1",
						},
					},
				},
			},
		},
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/openapi/v3", func(w http.ResponseWriter, r *http.Request) {
		// We need to use the actual server URL in the index, but we don't have
		// it yet. The handler captures it from the request.
		idx := sampleOpenAPIIndex("")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	})

	mux.HandleFunc("/openapi/v3/apis/alerting.test.app/v1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleOpenAPIDoc())
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Re-register the index handler with the correct server URL.
	mux.HandleFunc("/openapi/v3-with-url", func(w http.ResponseWriter, r *http.Request) {})
	// Actually, we need the serverRelativeURL to be relative, not absolute.
	// The fetcher prepends baseURL. So just use relative paths.
	return srv
}

func TestSchemaFetcher_FetchSpecSchemas(t *testing.T) {
	srv := newTestServer(t)

	// Override cache dir to temp directory.
	cacheDir := t.TempDir()
	t.Setenv("GCX_OPENAPI_CACHE_DIR", cacheDir)

	cfg := &rest.Config{Host: srv.URL}
	fetcher, err := discovery.NewSchemaFetcher(cfg)
	require.NoError(t, err)

	descs := resources.Descriptors{
		{
			GroupVersion: schema.GroupVersion{Group: "alerting.test.app", Version: "v1"},
			Kind:         "AlertRule",
			Plural:       "alertrules",
			Singular:     "alertrule",
		},
	}

	schemas, err := fetcher.FetchSpecSchemas(context.Background(), descs)
	require.NoError(t, err)

	// AlertRule should have a resolved spec schema.
	key := "alerting.test.app/v1/AlertRule"
	specSchema, ok := schemas[key]
	require.True(t, ok, "expected schema for AlertRule")

	props, ok := specSchema["properties"].(map[string]any)
	require.True(t, ok, "expected properties in spec schema")
	assert.Contains(t, props, "title")
	assert.Contains(t, props, "annotations")
	assert.Contains(t, props, "for")
}

func TestSchemaFetcher_UnknownGVSkipped(t *testing.T) {
	srv := newTestServer(t)

	t.Setenv("GCX_OPENAPI_CACHE_DIR", t.TempDir())

	cfg := &rest.Config{Host: srv.URL}
	fetcher, err := discovery.NewSchemaFetcher(cfg)
	require.NoError(t, err)

	// Request a GV that doesn't exist in the OpenAPI index.
	descs := resources.Descriptors{
		{
			GroupVersion: schema.GroupVersion{Group: "slo.ext.grafana.app", Version: "v1alpha1"},
			Kind:         "SLO",
			Plural:       "slos",
		},
	}

	schemas, err := fetcher.FetchSpecSchemas(context.Background(), descs)
	require.NoError(t, err)
	assert.Empty(t, schemas, "unknown GV should produce no schemas")
}

func TestDiskCache_GetSet(t *testing.T) {
	dir := t.TempDir()

	// Write a cache entry.
	err := os.MkdirAll(dir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "abc123.json"), []byte(`{"test": true}`), 0o600)
	require.NoError(t, err)

	// Read it back.
	data, err := os.ReadFile(filepath.Join(dir, "abc123.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"test": true}`, string(data))

	// Miss.
	_, err = os.ReadFile(filepath.Join(dir, "nonexistent.json"))
	assert.Error(t, err)
}

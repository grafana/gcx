package faro_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

func newTestAdapter(t *testing.T, server *httptest.Server, namespace string) adapter.ResourceAdapter {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: namespace,
	}
	factory := faro.NewFactoryFromConfig(cfg)
	a, err := factory(t.Context())
	require.NoError(t, err)
	return a
}

func TestResourceAdapter_Descriptor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")
	desc := a.Descriptor()

	assert.Equal(t, "faro.ext.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v1alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "FaroApp", desc.Kind)
	assert.Equal(t, "app", desc.Singular)
	assert.Equal(t, "apps", desc.Plural)
}

func TestResourceAdapter_NoAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")
	assert.Empty(t, a.Aliases(), "adapter aliases should be empty")
}

func TestResourceAdapter_List(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		handler       http.HandlerFunc
		wantLen       int
		wantErr       bool
		wantAPIVer    string
		wantKind      string
		wantNamespace string
	}{
		{
			name:      "returns resources with correct GVK and namespace",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []map[string]any{
					{"id": 42, "name": "my-web-app"},
					{"id": 43, "name": "other-app"},
				})
			},
			wantLen:       2,
			wantAPIVer:    faro.APIVersion,
			wantKind:      faro.Kind,
			wantNamespace: "stack-123",
		},
		{
			name:      "returns empty list",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []map[string]any{})
			},
			wantLen: 0,
		},
		{
			name:      "propagates client error",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			a := newTestAdapter(t, server, tt.namespace)
			result, err := a.List(t.Context(), metav1.ListOptions{})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result.Items, tt.wantLen)

			if tt.wantLen > 0 {
				item := result.Items[0]
				assert.Equal(t, tt.wantAPIVer, item.GetAPIVersion())
				assert.Equal(t, tt.wantKind, item.GetKind())
				assert.Equal(t, tt.wantNamespace, item.GetNamespace())
			}
		})
	}
}

func TestResourceAdapter_Get(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		handler  http.HandlerFunc
		wantName string
		wantErr  bool
	}{
		{
			name: "returns resource with correct name",
			id:   "my-web-app-42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/42")
				writeJSON(w, map[string]any{"id": 42, "name": "my-web-app"})
			},
			wantName: "my-web-app-42",
		},
		{
			name: "propagates not found error",
			id:   "missing-999",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			a := newTestAdapter(t, server, "stack-123")
			result, err := a.Get(t.Context(), tt.id, metav1.GetOptions{})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, result.GetName())
			assert.Equal(t, faro.APIVersion, result.GetAPIVersion())
			assert.Equal(t, faro.Kind, result.GetKind())
		})
	}
}

func TestResourceAdapter_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/42")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")
	err := a.Delete(t.Context(), "my-web-app-42", metav1.DeleteOptions{})
	require.NoError(t, err)
}

func TestResourceAdapter_ListPopulatesMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 42, "name": "my-web-app", "appKey": "abc"},
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "meta-ns")
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	item := result.Items[0]
	assert.Equal(t, "my-web-app-42", item.GetName())
	assert.Equal(t, "meta-ns", item.GetNamespace())
	assert.Equal(t, faro.APIVersion, item.GetAPIVersion())
	assert.Equal(t, faro.Kind, item.GetKind())

	spec, found, err := unstructured.NestedMap(item.Object, "spec")
	require.NoError(t, err)
	require.True(t, found, "spec field should be present")
	assert.Equal(t, "my-web-app", spec["name"])
}

func TestResourceAdapter_RoundTrip(t *testing.T) {
	// Verify that FaroApp -> unstructured -> back preserves fields.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":                 42,
			"name":               "my-web-app",
			"appKey":             "abc-key",
			"collectEndpointURL": "https://collect.example.com",
			"corsOrigins":        []map[string]any{{"url": "https://example.com"}},
			"extraLogLabels":     []map[string]string{{"key": "team", "value": "frontend"}},
			"settings": map[string]any{
				"geolocationEnabled": true,
				"geolocationLevel":   "country",
			},
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-rt")

	obj, err := a.Get(t.Context(), "my-web-app-42", metav1.GetOptions{})
	require.NoError(t, err)

	// Verify the unstructured spec contains expected fields.
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, "my-web-app", spec["name"])
	assert.Equal(t, "abc-key", spec["appKey"])
	assert.Equal(t, "https://collect.example.com", spec["collectEndpointURL"])

	// Verify metadata.
	assert.Equal(t, "my-web-app-42", obj.GetName())
	assert.Equal(t, "stack-rt", obj.GetNamespace())
	assert.Equal(t, faro.APIVersion, obj.GetAPIVersion())
	assert.Equal(t, faro.Kind, obj.GetKind())
}

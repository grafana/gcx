package definitions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers/slo/definitions"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

// newTestAdapter creates a ResourceAdapter backed by a test HTTP server.
func newTestAdapter(t *testing.T, server *httptest.Server, namespace string) adapter.ResourceAdapter {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: namespace,
	}

	factory := definitions.NewFactoryFromConfig(cfg)
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

	assert.Equal(t, "slo.ext.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v1alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "SLO", desc.Kind)
	assert.Equal(t, "slo", desc.Singular)
	assert.Equal(t, "slos", desc.Plural)
}

func TestResourceAdapter_NoAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")
	assert.Empty(t, a.Aliases(), "adapter aliases should be empty (provider-prefixed aliases removed)")
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
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				writeJSON(w, definitions.SLOListResponse{
					SLOs: []definitions.Slo{
						{UUID: "uuid-1", Name: "SLO 1"},
						{UUID: "uuid-2", Name: "SLO 2"},
					},
				})
			},
			wantLen:       2,
			wantAPIVer:    definitions.APIVersion,
			wantKind:      definitions.Kind,
			wantNamespace: "stack-123",
		},
		{
			name:      "returns empty list",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, definitions.SLOListResponse{})
			},
			wantLen: 0,
		},
		{
			name:      "propagates client error",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, definitions.ErrorResponse{Code: 500, Error: "internal error"})
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
		name      string
		namespace string
		uuid      string
		handler   http.HandlerFunc
		wantErr   bool
		wantName  string
	}{
		{
			name:      "returns resource with correct name",
			namespace: "stack-123",
			uuid:      "abc-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/abc-123", r.URL.Path)
				writeJSON(w, definitions.Slo{UUID: "abc-123", Name: "My SLO"})
			},
			wantName: "abc-123",
		},
		{
			name:      "propagates not found error",
			namespace: "stack-123",
			uuid:      "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, definitions.ErrorResponse{Code: 404, Error: "not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			a := newTestAdapter(t, server, tt.namespace)
			result, err := a.Get(t.Context(), tt.uuid, metav1.GetOptions{})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, result.GetName())
			assert.Equal(t, definitions.APIVersion, result.GetAPIVersion())
			assert.Equal(t, definitions.Kind, result.GetKind())
		})
	}
}

func TestResourceAdapter_Create(t *testing.T) {
	createdUUID := "new-uuid-456"

	// The Create handler serves POST and the following GET.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var slo definitions.Slo
			if err := json.NewDecoder(r.Body).Decode(&slo); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			assert.Equal(t, "New SLO", slo.Name)
			w.WriteHeader(http.StatusAccepted)
			writeJSON(w, definitions.SLOCreateResponse{UUID: createdUUID})
		case http.MethodGet:
			assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/"+createdUUID, r.URL.Path)
			writeJSON(w, definitions.Slo{UUID: createdUUID, Name: "New SLO"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")

	// Build the input resource via ToResource to ensure valid GVK envelope.
	inputSLO := definitions.Slo{
		Name:        "New SLO",
		Description: "A test SLO",
		Query: definitions.Query{
			Type:     "freeform",
			Freeform: &definitions.FreeformQuery{Query: "up"},
		},
		Objectives: []definitions.Objective{{Value: 0.99, Window: "30d"}},
	}
	res, err := definitions.ToResource(inputSLO, "stack-123")
	require.NoError(t, err)
	obj := res.ToUnstructured()

	result, err := a.Create(t.Context(), &obj, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, createdUUID, result.GetName())
	assert.Equal(t, definitions.APIVersion, result.GetAPIVersion())
	assert.Equal(t, definitions.Kind, result.GetKind())
}

func TestResourceAdapter_Update(t *testing.T) {
	targetUUID := "existing-uuid-789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/"+targetUUID, r.URL.Path)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/"+targetUUID, r.URL.Path)
			writeJSON(w, definitions.Slo{UUID: targetUUID, Name: "Updated SLO"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")

	inputSLO := definitions.Slo{
		UUID:        targetUUID,
		Name:        "Updated SLO",
		Description: "An updated SLO",
		Query: definitions.Query{
			Type:     "freeform",
			Freeform: &definitions.FreeformQuery{Query: "up"},
		},
		Objectives: []definitions.Objective{{Value: 0.99, Window: "30d"}},
	}
	res, err := definitions.ToResource(inputSLO, "stack-123")
	require.NoError(t, err)
	obj := res.ToUnstructured()

	result, err := a.Update(t.Context(), &obj, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, targetUUID, result.GetName())
}

func TestResourceAdapter_Delete(t *testing.T) {
	tests := []struct {
		name    string
		uuid    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "deletes by name",
			uuid: "del-uuid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugins/grafana-slo-app/resources/v1/slo/del-uuid", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "propagates error",
			uuid: "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, definitions.ErrorResponse{Code: 404, Error: "not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			a := newTestAdapter(t, server, "stack-123")
			err := a.Delete(t.Context(), tt.uuid, metav1.DeleteOptions{})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestResourceAdapter_RoundTrip(t *testing.T) {
	originalSLO := definitions.Slo{
		UUID:        "rt-uuid-001",
		Name:        "Round-trip SLO",
		Description: "Tests full marshal/unmarshal cycle",
		Query: definitions.Query{
			Type: "ratio",
			Ratio: &definitions.RatioQuery{
				SuccessMetric: definitions.MetricDef{PrometheusMetric: "http_ok"},
				TotalMetric:   definitions.MetricDef{PrometheusMetric: "http_total"},
			},
		},
		Objectives: []definitions.Objective{{Value: 0.999, Window: "30d"}},
		Labels:     []definitions.Label{{Key: "team", Value: "platform"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, originalSLO)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-rt")

	// Get returns an unstructured.Unstructured.
	obj, err := a.Get(t.Context(), originalSLO.UUID, metav1.GetOptions{})
	require.NoError(t, err)

	// Convert back to Resource and then FromResource.
	res, err := resources.FromUnstructured(obj)
	require.NoError(t, err)

	restored, err := definitions.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, originalSLO.UUID, restored.UUID)
	assert.Equal(t, originalSLO.Name, restored.Name)
	assert.Equal(t, originalSLO.Description, restored.Description)
	assert.Equal(t, originalSLO.Query.Type, restored.Query.Type)
	require.NotNil(t, restored.Query.Ratio)
	assert.Equal(t, originalSLO.Query.Ratio.SuccessMetric.PrometheusMetric, restored.Query.Ratio.SuccessMetric.PrometheusMetric)
	assert.Equal(t, originalSLO.Query.Ratio.TotalMetric.PrometheusMetric, restored.Query.Ratio.TotalMetric.PrometheusMetric)
}

func TestResourceAdapter_ListPopulatesMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, definitions.SLOListResponse{
			SLOs: []definitions.Slo{
				{UUID: "meta-uuid", Name: "Metadata SLO"},
			},
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "meta-ns")
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	item := result.Items[0]
	assert.Equal(t, "meta-uuid", item.GetName())
	assert.Equal(t, "meta-ns", item.GetNamespace())
	assert.Equal(t, definitions.APIVersion, item.GetAPIVersion())
	assert.Equal(t, definitions.Kind, item.GetKind())

	// Verify spec is populated.
	spec, found, err := unstructured.NestedMap(item.Object, "spec")
	require.NoError(t, err)
	require.True(t, found, "spec field should be present")
	assert.Equal(t, "Metadata SLO", spec["name"])
}

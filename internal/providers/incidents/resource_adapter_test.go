package incidents_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers/incidents"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
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
	factory := incidents.NewFactoryFromConfig(cfg)
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

	assert.Equal(t, "incident.ext.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v1alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "Incident", desc.Kind)
	assert.Equal(t, "incident", desc.Singular)
	assert.Equal(t, "incidents", desc.Plural)
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
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"incidents": []map[string]any{
						{"incidentID": "inc-1", "title": "Outage 1", "status": "active"},
						{"incidentID": "inc-2", "title": "Outage 2", "status": "resolved"},
					},
					"cursor": map[string]any{"hasMore": false},
					"query":  map[string]any{},
				})
			},
			wantLen:       2,
			wantAPIVer:    incidents.APIVersion,
			wantKind:      incidents.Kind,
			wantNamespace: "stack-123",
		},
		{
			name:      "returns empty list",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"incidents": []map[string]any{},
					"cursor":    map[string]any{"hasMore": false},
					"query":     map[string]any{},
				})
			},
			wantLen: 0,
		},
		{
			name:      "propagates client error",
			namespace: "stack-123",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "internal error"})
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
			id:   "inc-123",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "IncidentsService.GetIncident")
				writeJSON(w, map[string]any{
					"incident": map[string]any{"incidentID": "inc-123", "title": "My Incident", "status": "active"},
				})
			},
			wantName: "inc-123",
		},
		{
			name: "propagates not found error",
			id:   "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
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
			assert.Equal(t, incidents.APIVersion, result.GetAPIVersion())
			assert.Equal(t, incidents.Kind, result.GetKind())
		})
	}
}

func TestResourceAdapter_Create(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "IncidentsService.CreateIncident")
		writeJSON(w, map[string]any{
			"incident": map[string]any{"incidentID": "new-456", "title": "New Incident", "status": "active"},
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")

	inputInc := incidents.Incident{
		Title:  "New Incident",
		Status: "active",
	}
	res, err := incidents.ToResource(inputInc, "stack-123")
	require.NoError(t, err)
	obj := res.ToUnstructured()

	result, err := a.Create(t.Context(), &obj, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "new-456", result.GetName())
	assert.Equal(t, incidents.APIVersion, result.GetAPIVersion())
	assert.Equal(t, incidents.Kind, result.GetKind())
}

func TestResourceAdapter_Update(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "IncidentsService.UpdateStatus")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "inc-789", body["incidentID"])
		writeJSON(w, map[string]any{
			"incident": map[string]any{"incidentID": "inc-789", "title": "Updated", "status": "resolved"},
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")

	inputInc := incidents.Incident{
		IncidentID: "inc-789",
		Title:      "Updated",
		Status:     "resolved",
	}
	res, err := incidents.ToResource(inputInc, "stack-123")
	require.NoError(t, err)
	obj := res.ToUnstructured()

	result, err := a.Update(t.Context(), &obj, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, "inc-789", result.GetName())
}

func TestResourceAdapter_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-123")
	err := a.Delete(t.Context(), "inc-1", metav1.DeleteOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestResourceAdapter_RoundTrip(t *testing.T) {
	originalInc := incidents.Incident{
		IncidentID:   "rt-inc-001",
		Title:        "Round-trip Incident",
		Status:       "active",
		Severity:     "critical",
		Description:  "Tests full marshal/unmarshal cycle",
		IncidentType: "default",
		Labels: []incidents.IncidentLabel{
			{Key: "team", Label: "platform"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"incident": originalInc,
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "stack-rt")

	obj, err := a.Get(t.Context(), originalInc.IncidentID, metav1.GetOptions{})
	require.NoError(t, err)

	// Convert back to Resource and then FromResource.
	res, err := resources.FromUnstructured(obj)
	require.NoError(t, err)

	restored, err := incidents.FromResource(res)
	require.NoError(t, err)

	assert.Equal(t, originalInc.IncidentID, restored.IncidentID)
	assert.Equal(t, originalInc.Title, restored.Title)
	assert.Equal(t, originalInc.Status, restored.Status)
	assert.Equal(t, originalInc.Severity, restored.Severity)
	assert.Equal(t, originalInc.Description, restored.Description)
}

func TestResourceAdapter_ListPopulatesMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"incidents": []map[string]any{
				{"incidentID": "meta-inc", "title": "Metadata Inc", "status": "active"},
			},
			"cursor": map[string]any{"hasMore": false},
			"query":  map[string]any{},
		})
	}))
	defer server.Close()

	a := newTestAdapter(t, server, "meta-ns")
	result, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	item := result.Items[0]
	assert.Equal(t, "meta-inc", item.GetName())
	assert.Equal(t, "meta-ns", item.GetNamespace())
	assert.Equal(t, incidents.APIVersion, item.GetAPIVersion())
	assert.Equal(t, incidents.Kind, item.GetKind())

	spec, found, err := unstructured.NestedMap(item.Object, "spec")
	require.NoError(t, err)
	require.True(t, found, "spec field should be present")
	assert.Equal(t, "Metadata Inc", spec["title"])
}

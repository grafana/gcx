package definitions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

// newTestAdapter creates a ResourceAdapter backed by a test HTTP server, via
// the same declarative definitions.SloResource + adapter.NewProvider path
// used by provider.go — the pipeline the `gcx resources` command surface
// resolves through (AC-001, AC-019).
func newTestAdapter(t *testing.T, server *httptest.Server, namespace string) adapter.ResourceAdapter {
	t.Helper()

	loadDeps := func(context.Context) (adapter.ClientDeps, error) {
		return adapter.ClientDeps{
			HTTP:      server.Client(),
			BaseURL:   server.URL,
			Namespace: namespace,
		}, nil
	}

	p := adapter.NewProvider("slo", "test", loadDeps, definitions.SloResource())
	regs := p.TypedRegistrations()
	require.Len(t, regs, 1)

	a, err := regs[0].Factory(t.Context())
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
			wantAPIVer:    "slo.ext.grafana.app/v1alpha1",
			wantKind:      "SLO",
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
				writeJSON(w, providers.ErrorResponse{Error: "internal error"})
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
				writeJSON(w, providers.ErrorResponse{Error: "not found"})
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
			assert.Equal(t, "slo.ext.grafana.app/v1alpha1", result.GetAPIVersion())
			assert.Equal(t, "SLO", result.GetKind())
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
	obj, err := definitions.SloResource().TypedCRUD(nil, "stack-123").ToUnstructured(inputSLO)
	require.NoError(t, err)

	result, err := a.Create(t.Context(), &obj, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, createdUUID, result.GetName())
	assert.Equal(t, "slo.ext.grafana.app/v1alpha1", result.GetAPIVersion())
	assert.Equal(t, "SLO", result.GetKind())
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
	obj, err := definitions.SloResource().TypedCRUD(nil, "stack-123").ToUnstructured(inputSLO)
	require.NoError(t, err)

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
				writeJSON(w, providers.ErrorResponse{Error: "not found"})
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

	restored, err := definitions.SloResource().TypedCRUD(nil, "stack-rt").FromUnstructured(obj)
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
	assert.Equal(t, "slo.ext.grafana.app/v1alpha1", item.GetAPIVersion())
	assert.Equal(t, "SLO", item.GetKind())

	// Verify spec is populated.
	spec, found, err := unstructured.NestedMap(item.Object, "spec")
	require.NoError(t, err)
	require.True(t, found, "spec field should be present")
	assert.Equal(t, "Metadata SLO", spec["name"])
}

// TestSloResource_RegistrationDerivesSchemaAndExample covers AC-002: the SLO
// definition type's registration carries a non-nil derived schema and a
// derived example, without SloExample()/SloSchema() hand-written manifests.
func TestSloResource_RegistrationDerivesSchemaAndExample(t *testing.T) {
	loadDeps := func(context.Context) (adapter.ClientDeps, error) { return adapter.ClientDeps{}, nil }
	p := adapter.NewProvider("slo", "test", loadDeps, definitions.SloResource())
	regs := p.TypedRegistrations()
	require.Len(t, regs, 1)

	reg := regs[0]
	assert.NotNil(t, reg.Schema, "schema must be auto-derived from Slo, not hand-threaded")
	require.NotNil(t, reg.Example, "example must be derived from SloResource.Example")
	assert.Contains(t, string(reg.Example), "HTTP Availability")
	var example unstructured.Unstructured
	require.NoError(t, json.Unmarshal(reg.Example, &example))
	assert.Equal(t, "my-slo", example.GetName())
	spec, found, err := unstructured.NestedMap(example.Object, "spec")
	require.NoError(t, err)
	require.True(t, found)
	assert.NotContains(t, spec, "uuid")
	assert.NotContains(t, spec, "readOnly")

	a, err := reg.Factory(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, a.Schema(), "AsAdapter's Schema() must never be nil (FR-016)")
	assert.Equal(t, reg.Example, a.Example())
}

// TestSloResource_SharedByBothFrontDoors covers AC-001/AC-019: the same
// definitions.SloResource declaration (and therefore the same NewClient /
// capability-seam construction) backs both provider.go's `gcx slo` command
// tree (via providers.BoundResource.Load) and the `gcx resources` pipeline (via
// adapter.NewProvider's TypedRegistrations()) — this test drives both call
// sites against the same test server and asserts field-for-field equivalent
// data.
func TestSloResource_SharedByBothFrontDoors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-slo-app/resources/v1/slo":
			writeJSON(w, definitions.SLOListResponse{
				SLOs: []definitions.Slo{{UUID: "shared-uuid", Name: "Shared SLO"}},
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	// Front door 1: gcx resources, via SloResource's adapter.NewProvider
	// registration/capability-seam path.
	a := newTestAdapter(t, server, "shared-ns")
	viaResources, err := a.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, viaResources.Items, 1)

	// Front door 2: gcx slo definitions, through the shared resource loader.
	loader := stubGrafanaConfigLoader{host: server.URL, namespace: "shared-ns"}
	crud, _, err := providers.BindGrafanaResource(loader, definitions.SloResource()).Load(t.Context())
	require.NoError(t, err)
	viaCommands, err := crud.List(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, viaCommands, 1)

	assert.Equal(t, viaResources.Items[0].GetName(), viaCommands[0].Spec.UUID)
	assert.Equal(t, "Shared SLO", viaCommands[0].Spec.Name)
	spec, found, err := unstructured.NestedMap(viaResources.Items[0].Object, "spec")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "Shared SLO", spec["name"])
	commandObj, err := crud.ToUnstructured(viaCommands[0].Spec)
	require.NoError(t, err)
	assert.Equal(t, viaResources.Items[0].Object, commandObj.Object)
	assert.NotContains(t, spec, "uuid")
	assert.NotContains(t, spec, "readOnly")
	assert.NotEmpty(t, crud.Example)
}

// stubGrafanaConfigLoader implements providers.GrafanaConfigLoader for
// TestSloResource_SharedByBothFrontDoors.
type stubGrafanaConfigLoader struct {
	host      string
	namespace string
}

func (l stubGrafanaConfigLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: l.host},
		Namespace: l.namespace,
	}, nil
}

func minimalSlo() definitions.Slo {
	return definitions.Slo{
		UUID:        "test-uuid-123",
		Name:        "My SLO",
		Description: "A test SLO",
		Query: definitions.Query{
			Type: "freeform",
			Freeform: &definitions.FreeformQuery{
				Query: "sum(rate(http_requests_total{status=~\"2..\"}[5m])) / sum(rate(http_requests_total[5m]))",
			},
		},
		Objectives: []definitions.Objective{
			{Value: 0.999, Window: "30d"},
		},
	}
}

func fullSlo() definitions.Slo {
	return definitions.Slo{
		UUID:        "full-uuid-456",
		Name:        "Full SLO",
		Description: "A fully populated SLO",
		Query: definitions.Query{
			Type: "ratio",
			Ratio: &definitions.RatioQuery{
				SuccessMetric: definitions.MetricDef{
					PrometheusMetric: "http_requests_total{status=~\"2..\"}",
					Type:             "counter",
				},
				TotalMetric: definitions.MetricDef{
					PrometheusMetric: "http_requests_total",
					Type:             "counter",
				},
				GroupByLabels: []string{"service"},
			},
		},
		Objectives: []definitions.Objective{
			{Value: 0.999, Window: "30d"},
			{Value: 0.99, Window: "7d"},
		},
		Labels: []definitions.Label{
			{Key: "team", Value: "platform"},
		},
		Alerting: &definitions.Alerting{
			Labels: []definitions.Label{{Key: "severity", Value: "critical"}},
			FastBurn: &definitions.AlertingRule{
				Annotations: []definitions.Label{{Key: "runbook", Value: "https://example.com"}},
				Enrichments: []definitions.Enrichment{{Type: "assistantInvestigation"}},
			},
		},
		DestinationDatasource: &definitions.DestinationDatasource{UID: "prom-uid"},
		Folder:                &definitions.Folder{UID: "folder-uid"},
		SearchExpression:      "team:platform",
	}
}

func TestSloResource_Conversion(t *testing.T) {
	tests := []struct {
		name  string
		value definitions.Slo
	}{
		{name: "minimal", value: minimalSlo()},
		{name: "full", value: fullSlo()},
		{name: "Ratio", value: definitions.Slo{
			UUID:        "ratio-uuid",
			Name:        "Ratio SLO",
			Description: "An SLO with ratio query",
			Query: definitions.Query{
				Type: "ratio",
				Ratio: &definitions.RatioQuery{
					SuccessMetric: definitions.MetricDef{
						PrometheusMetric: "http_requests_total{status=~\"2..\"}",
						Type:             "counter",
					},
					TotalMetric: definitions.MetricDef{
						PrometheusMetric: "http_requests_total",
					},
					GroupByLabels: []string{"service", "namespace"},
				},
			},
			Objectives: []definitions.Objective{
				{Value: 0.995, Window: "28d"},
			},
		}},
		{name: "Threshold", value: definitions.Slo{
			UUID:        "threshold-uuid",
			Name:        "Threshold SLO",
			Description: "An SLO with threshold query",
			Query: definitions.Query{
				Type: "threshold",
				Threshold: &definitions.ThresholdQuery{
					ThresholdExpression: "sum(rate(http_requests_total[5m]))",
					Threshold: definitions.Threshold{
						Value:    100.0,
						Operator: "gt",
					},
					GroupByLabels: []string{"pod"},
				},
			},
			Objectives: []definitions.Objective{
				{Value: 0.99, Window: "7d"},
			},
		}},
		{name: "AlertingEnrichments", value: func() definitions.Slo {
			original := minimalSlo()
			original.Alerting = &definitions.Alerting{
				FastBurn: &definitions.AlertingRule{
					Annotations: []definitions.Label{{Key: "name", Value: "SLO Burn Rate Very High"}},
					Enrichments: []definitions.Enrichment{{Type: "assistantInvestigation"}},
				},
				SlowBurn: &definitions.AlertingRule{
					Annotations: []definitions.Label{{Key: "name", Value: "SLO Burn Rate High"}},
					Enrichments: []definitions.Enrichment{{Type: "assistantInvestigation"}},
				},
			}
			return original
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crud := definitions.SloResource().TypedCRUD(nil, "stack-123")
			tt.value.ReadOnly = &definitions.ReadOnly{CreationTimestamp: 1234567890, Status: &definitions.Status{Type: "Ok"}, Provenance: "api"}
			obj, err := crud.ToUnstructured(tt.value)
			require.NoError(t, err)
			assert.Equal(t, "slo.ext.grafana.app/v1alpha1", obj.GetAPIVersion())
			assert.Equal(t, "SLO", obj.GetKind())
			assert.Equal(t, tt.value.UUID, obj.GetName())
			assert.Equal(t, "stack-123", obj.GetNamespace())
			spec, found, err := unstructured.NestedMap(obj.Object, "spec")
			require.NoError(t, err)
			require.True(t, found)
			assert.NotContains(t, spec, "uuid")
			assert.NotContains(t, spec, "readOnly")
			restored, err := crud.FromUnstructured(&obj)
			require.NoError(t, err)
			tt.value.ReadOnly = nil
			assert.Equal(t, tt.value, *restored)
		})
	}
}

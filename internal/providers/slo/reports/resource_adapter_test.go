package reports_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReportResource(t *testing.T) {
	for _, operation := range []string{"list", "get", "missing", "create", "update", "delete", "dry-run"} {
		t.Run(operation, func(t *testing.T) {
			writes, reads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					reads++
					if r.URL.Path == "/api/plugins/grafana-slo-app/resources/v1/report/missing" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if operation == "list" {
						_, _ = w.Write([]byte(`{"reports":[{"uuid":"r1","name":"Weekly"},{"uuid":"r2","name":"Monthly"}]}`))
						return
					}
					_, _ = w.Write([]byte(`{"uuid":"r1","name":"Weekly"}`))
				case http.MethodPost, http.MethodPut:
					writes++
					var item reports.Report
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&item))
					assert.Equal(t, "Weekly", item.Name)
					w.WriteHeader(http.StatusAccepted)
					if r.Method == http.MethodPost {
						_, _ = w.Write([]byte(`{"uuid":"created","message":"accepted"}`))
					}
				case http.MethodDelete:
					writes++
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			p := adapter.NewProvider("slo", "SLO", func(context.Context) (adapter.ClientDeps, error) {
				return adapter.ClientDeps{HTTP: server.Client(), BaseURL: server.URL, Namespace: "stack"}, nil
			}, reports.ReportResource())
			reg := p.TypedRegistrations()[0]
			require.NotEmpty(t, reg.Schema)
			require.NotEmpty(t, reg.Example)
			a, err := reg.Factory(t.Context())
			require.NoError(t, err)
			obj := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "r1"}, "spec": map[string]any{"name": "Weekly"}}}
			var result *unstructured.Unstructured
			switch operation {
			case "list":
				list, err := a.List(t.Context(), metav1.ListOptions{Limit: 1})
				require.NoError(t, err)
				require.Len(t, list.Items, 1)
				result = &list.Items[0]
			case "get":
				result, err = a.Get(t.Context(), "r1", metav1.GetOptions{})
			case "missing":
				_, err = a.Get(t.Context(), "missing", metav1.GetOptions{})
				require.True(t, apierrors.IsNotFound(err), "%v", err)
				return
			case "create":
				result, err = a.Create(t.Context(), obj, metav1.CreateOptions{})
			case "update":
				result, err = a.Update(t.Context(), obj, metav1.UpdateOptions{})
			case "delete":
				err = a.Delete(t.Context(), "r1", metav1.DeleteOptions{})
			case "dry-run":
				_, err = a.Create(t.Context(), obj, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
				require.ErrorIs(t, err, adapter.ErrDryRunUnverified)
				assert.Zero(t, writes)
				return
			}
			require.NoError(t, err)
			if operation == "create" || operation == "update" || operation == "delete" {
				assert.Equal(t, 1, writes)
				assert.Zero(t, reads, "accepted writes must not require an immediate read-back")
			}
			if result != nil {
				assert.Equal(t, "Report", result.GetKind())
				assert.Equal(t, "stack", result.GetNamespace())
				spec, _, err := unstructured.NestedMap(result.Object, "spec")
				require.NoError(t, err)
				assert.NotContains(t, spec, "uuid")
				if operation == "create" {
					assert.Equal(t, "created", result.GetName())
				}
			}
		})
	}
}

func minimalReport() reports.Report {
	return reports.Report{
		UUID:        "test-uuid-123",
		Name:        "My Report",
		Description: "A test report",
		TimeSpan:    "calendarMonth",
		ReportDefinition: reports.ReportDefinition{
			Slos: []reports.ReportSlo{
				{SloUUID: "slo-uuid-1"},
			},
		},
	}
}

func fullReport() reports.Report {
	return reports.Report{
		UUID:        "full-uuid-456",
		Name:        "Full Report",
		Description: "A fully populated report",
		TimeSpan:    "weeklySundayToSunday",
		Labels: []reports.Label{
			{Key: "team", Value: "platform"},
		},
		ReportDefinition: reports.ReportDefinition{
			Slos: []reports.ReportSlo{
				{SloUUID: "slo-uuid-1"},
				{SloUUID: "slo-uuid-2"},
				{SloUUID: "slo-uuid-3"},
			},
		},
	}
}

func TestReportResource_Conversion(t *testing.T) {
	tests := []struct {
		name  string
		value reports.Report
	}{
		{name: "minimal", value: minimalReport()},
		{name: "full", value: fullReport()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crud := reports.ReportResource().TypedCRUD(nil, "stack-123")
			obj, err := crud.ToUnstructured(tt.value)
			require.NoError(t, err)
			assert.Equal(t, "slo.ext.grafana.app/v1alpha1", obj.GetAPIVersion())
			assert.Equal(t, "Report", obj.GetKind())
			assert.Equal(t, tt.value.UUID, obj.GetName())
			assert.Equal(t, "stack-123", obj.GetNamespace())
			spec, found, err := unstructured.NestedMap(obj.Object, "spec")
			require.NoError(t, err)
			require.True(t, found)
			assert.NotContains(t, spec, "uuid")
			restored, err := crud.FromUnstructured(&obj)
			require.NoError(t, err)
			assert.Equal(t, tt.value, *restored)
		})
	}
}

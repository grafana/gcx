package fleet //nolint:testpackage // Tests unexported Fleet resource contracts.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFleetExamplesMatchLiveCreateRequirements(t *testing.T) {
	var pipeline map[string]any
	require.NoError(t, json.Unmarshal(pipelineExample(), &pipeline))
	pipelineSpec, ok := pipeline["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "my_pipeline", pipelineSpec["name"])
	assert.Regexp(t, `^[A-Za-z_][A-Za-z0-9_]*$`, pipelineSpec["name"])

	var collector map[string]any
	require.NoError(t, json.Unmarshal(collectorExample(), &collector))
	collectorSpec, ok := collector["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "my-collector-id", collectorSpec["id"])
	assert.Equal(t, "COLLECTOR_TYPE_ALLOY", collectorSpec["collector_type"])
}

func TestCollectorSchemaExposesID(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(collectorSchema(), &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	spec, ok := properties["spec"].(map[string]any)
	require.True(t, ok)
	specProperties, ok := spec["properties"].(map[string]any)
	require.True(t, ok)
	id, ok := specProperties["id"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", id["type"])
}

func TestCollectorSchemaExposesHealthFields(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(collectorSchema(), &schema))

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	spec, ok := properties["spec"].(map[string]any)
	require.True(t, ok)
	specProperties, ok := spec["properties"].(map[string]any)
	require.True(t, ok)

	collectorType, ok := specProperties["collector_type"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"COLLECTOR_TYPE_UNSPECIFIED", "COLLECTOR_TYPE_ALLOY", "COLLECTOR_TYPE_OTEL"}, collectorType["enum"])
	var example map[string]any
	require.NoError(t, json.Unmarshal(collectorExample(), &example))
	exampleSpec, ok := example["spec"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, collectorType["enum"], exampleSpec["collector_type"])
	if required, ok := spec["required"].([]any); ok {
		assert.NotContains(t, required, "collector_type")
	}

	localAttributes, ok := specProperties["local_attributes"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, localAttributes["readOnly"])
	additionalProperties, ok := localAttributes["additionalProperties"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", additionalProperties["type"])

	for _, name := range []string{"created_at", "updated_at", "marked_inactive_at"} {
		field, ok := specProperties[name].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", field["type"])
		assert.Equal(t, "date-time", field["format"])
		assert.Equal(t, true, field["readOnly"])
	}
}

func TestResolveCollectorUsesArbitraryStringIDFirst(t *testing.T) {
	const collectorID = "collector-prod-eu-a"
	var requestedIDs []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathGetCollector:
			var request map[string]string
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requestedIDs = append(requestedIDs, request["id"])
			if request["id"] != collectorID {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeContractJSON(t, w, map[string]any{"id": collectorID, "name": "renamed"})
		case pathListCollectors:
			t.Error("resolver listed collectors after an exact ID match")
			writeContractJSON(t, w, map[string]any{"collectors": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	collector, err := resolveCollector(context.Background(), NewClient(context.Background(), server.URL, server.Client()), collectorID)
	require.NoError(t, err)
	assert.Equal(t, collectorID, collector.ID)
	assert.Equal(t, []string{collectorID}, requestedIDs)
}

func TestResolveCollectorFallbacks(t *testing.T) {
	const stringID = "collector-prod-eu-a"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathGetCollector:
			var request map[string]string
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if request["id"] == "202" {
				writeContractJSON(t, w, map[string]any{"id": "202", "name": "legacy"})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case pathListCollectors:
			writeContractJSON(t, w, map[string]any{
				"collectors": []map[string]any{{"id": stringID, "name": "renamed"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(context.Background(), server.URL, server.Client())

	tests := []struct {
		name   string
		ref    string
		wantID string
	}{
		{name: "collector name", ref: "renamed", wantID: stringID},
		{name: "rendered string ID", ref: "renamed-" + stringID, wantID: stringID},
		{name: "legacy numeric composite", ref: "legacy-202", wantID: "202"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := resolveCollector(context.Background(), client, tt.ref)
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, collector.ID)
		})
	}
}

// TestAdapterGetMissingReturnsNotFound pins the contract that the push
// pipeline relies on: a missing resource must surface as a Kubernetes NotFound,
// so that push creates it rather than failing.
func TestAdapterGetMissingReturnsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fleetProxyPrefix + pathListPipelines:
			writeContractJSON(t, w, map[string]any{"pipelines": []any{}})
		case fleetProxyPrefix + pathListCollectors:
			writeContractJSON(t, w, map[string]any{"collectors": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	loader := &fakeRESTLoader{url: server.URL}

	tests := []struct {
		name    string
		factory adapter.Factory
		ref     string
	}{
		{name: "pipeline without ID", factory: NewPipelineAdapterFactory(loader), ref: "my-pipeline"},
		{name: "pipeline from another stack", factory: NewPipelineAdapterFactory(loader), ref: "my-pipeline-99999"},
		{name: "collector", factory: NewCollectorAdapterFactory(loader), ref: "my-collector"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := tt.factory(context.Background())
			require.NoError(t, err)

			_, err = a.Get(context.Background(), tt.ref, metav1.GetOptions{})
			require.Error(t, err)
			assert.True(t, apierrors.IsNotFound(err), "want NotFound, got %v", err)
		})
	}
}

func writeContractJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(value))
}

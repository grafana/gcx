package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, server *httptest.Server) *fleet.Client {
	t.Helper()
	return fleet.NewClient(context.Background(), server.URL, nil)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func TestClient_ListPipelines(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns pipelines",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/pipeline.v1.PipelineService/ListPipelines")
				writeJSON(w, map[string]any{
					"pipelines": []map[string]any{
						{"id": "p-1", "name": "pipeline-1", "enabled": true},
						{"id": "p-2", "name": "pipeline-2", "enabled": false},
					},
				})
			},
			wantLen: 2,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			pipelines, err := client.ListPipelines(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, pipelines, tt.wantLen)
		})
	}
}

func TestClient_GetPipeline(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "returns pipeline by ID",
			id:   "p-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/pipeline.v1.PipelineService/GetPipeline")

				var body map[string]string
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "p-1", body["id"])

				writeJSON(w, map[string]any{
					"id": "p-1", "name": "pipeline-1", "enabled": true,
				})
			},
			wantID: "p-1",
		},
		{
			name: "returns error on not found",
			id:   "p-missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
			errMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			pipeline, err := client.GetPipeline(context.Background(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pipeline)
			assert.Equal(t, tt.wantID, pipeline.ID)
		})
	}
}

func TestClient_CreatePipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pipeline.v1.PipelineService/CreatePipeline")

		var body map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		var p fleet.Pipeline
		assert.NoError(t, json.Unmarshal(body["pipeline"], &p))
		assert.Equal(t, "new-pipeline", p.Name)
		assert.Equal(t, "contents here", p.Contents)

		writeJSON(w, map[string]any{
			"id": "p-created", "name": "new-pipeline", "enabled": true, "contents": "contents here",
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	created, err := client.CreatePipeline(context.Background(), fleet.Pipeline{
		Name:     "new-pipeline",
		Contents: "contents here",
		Enabled:  new(true),
	})

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "p-created", created.ID)
	assert.Equal(t, "new-pipeline", created.Name)
}

func TestClient_UpdatePipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pipeline.v1.PipelineService/UpdatePipeline")

		var body map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		var p fleet.Pipeline
		assert.NoError(t, json.Unmarshal(body["pipeline"], &p))
		assert.Equal(t, "p-1", p.ID)
		assert.Equal(t, "updated-pipeline", p.Name)

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdatePipeline(context.Background(), "p-1", fleet.Pipeline{
		Name:     "updated-pipeline",
		Contents: "updated contents",
	})

	require.NoError(t, err)
}

// TestClient_ListPipelines_PreservesConfigType is a regression test for the list
// path, which feeds the wide table: configType must survive the list decode, or
// OTel pipelines stay indistinguishable from Alloy ones in gcx output.
func TestClient_ListPipelines_PreservesConfigType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"pipelines": []map[string]any{
				{"id": "otel-1", "name": "otel-pipeline", "configType": "CONFIG_TYPE_OTEL"},
				{"id": "alloy-1", "name": "alloy-pipeline"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	pipelines, err := client.ListPipelines(context.Background())
	require.NoError(t, err)
	require.Len(t, pipelines, 2)
	assert.Equal(t, "CONFIG_TYPE_OTEL", pipelines[0].ConfigType, "configType must survive the list decode")
	assert.Empty(t, pipelines[1].ConfigType, "a pipeline without configType decodes to the empty Alloy default")
}

// TestClient_GetPipeline_PreservesConfigType is a regression test for the OTel
// pipeline bug: a Fleet API response carrying "configType":"CONFIG_TYPE_OTEL"
// must not lose that field on decode, otherwise valid OTel YAML is later treated
// as Alloy River.
func TestClient_GetPipeline_PreservesConfigType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"id":         "otel-1",
			"name":       "macos_endpoint_otel",
			"enabled":    true,
			"configType": "CONFIG_TYPE_OTEL",
			"contents":   "receivers: {}",
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	p, err := client.GetPipeline(context.Background(), "otel-1")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "CONFIG_TYPE_OTEL", p.ConfigType, "configType must survive API decode")
}

// TestClient_CreatePipeline_SendsConfigType is a regression test: a new OTel
// pipeline must send "configType":"CONFIG_TYPE_OTEL", otherwise Fleet validates
// the OTel YAML as Alloy River and rejects the request.
func TestClient_CreatePipeline_SendsConfigType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		var p fleet.Pipeline
		assert.NoError(t, json.Unmarshal(body["pipeline"], &p))
		assert.Equal(t, "CONFIG_TYPE_OTEL", p.ConfigType, "configType must be sent on create")

		writeJSON(w, map[string]any{
			"id":         "otel-1",
			"name":       "macos_endpoint_otel",
			"configType": "CONFIG_TYPE_OTEL",
			"contents":   "receivers: {}",
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	created, err := client.CreatePipeline(context.Background(), fleet.Pipeline{
		Name:       "macos_endpoint_otel",
		Contents:   "receivers: {}",
		ConfigType: "CONFIG_TYPE_OTEL",
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "CONFIG_TYPE_OTEL", created.ConfigType, "configType must survive the create response decode")
}

// TestClient_UpdatePipeline_SendsConfigType is a regression test: an OTel
// pipeline update must send "configType":"CONFIG_TYPE_OTEL" back to Fleet.
func TestClient_UpdatePipeline_SendsConfigType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		var p fleet.Pipeline
		assert.NoError(t, json.Unmarshal(body["pipeline"], &p))
		assert.Equal(t, "CONFIG_TYPE_OTEL", p.ConfigType, "configType must be sent on update")

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdatePipeline(context.Background(), "otel-1", fleet.Pipeline{
		Name:       "macos_endpoint_otel",
		Contents:   "receivers: {}",
		ConfigType: "CONFIG_TYPE_OTEL",
	})
	require.NoError(t, err)
}

// TestClient_UpdatePipeline_OmitsEmptyConfigType guards backward compatibility:
// an Alloy pipeline with no configType must not send an empty "configType" field.
func TestClient_UpdatePipeline_OmitsEmptyConfigType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		var raw map[string]any
		assert.NoError(t, json.Unmarshal(body["pipeline"], &raw))
		assert.NotContains(t, raw, "configType", "empty configType must be omitted for Alloy pipelines")

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdatePipeline(context.Background(), "alloy-1", fleet.Pipeline{
		Name:     "alloy-pipeline",
		Contents: "logging {}",
	})
	require.NoError(t, err)
}

func TestClient_DeletePipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pipeline.v1.PipelineService/DeletePipeline")

		var body map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "p-1", body["id"])

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeletePipeline(context.Background(), "p-1")

	require.NoError(t, err)
}

func TestClient_ListCollectors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/collector.v1.CollectorService/ListCollectors")
		writeJSON(w, map[string]any{
			"collectors": []map[string]any{
				{"id": "c-1", "name": "collector-1", "collector_type": "alloy"},
				{"id": "c-2", "name": "collector-2", "collector_type": "alloy"},
				{"id": "c-3", "name": "collector-3", "collector_type": "alloy"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	collectors, err := client.ListCollectors(context.Background())

	require.NoError(t, err)
	assert.Len(t, collectors, 3)
	assert.Equal(t, "c-1", collectors[0].ID)
}

func TestClient_GetCollector(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "returns collector by ID",
			id:   "c-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/collector.v1.CollectorService/GetCollector")

				var body map[string]string
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "c-1", body["id"])

				writeJSON(w, map[string]any{
					"id": "c-1", "name": "collector-1", "collector_type": "alloy",
				})
			},
			wantID: "c-1",
		},
		{
			name: "returns error on not found",
			id:   "c-missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
			errMsg:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			collector, err := client.GetCollector(context.Background(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, collector)
			assert.Equal(t, tt.wantID, collector.ID)
		})
	}
}

func TestClient_CreateCollector(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		wantID   string
		wantName string
	}{
		{
			name:     "uses complete server response",
			response: map[string]any{"id": "server-id", "name": "server-name", "collector_type": "alloy"},
			wantID:   "server-id",
			wantName: "server-name",
		},
		{
			name:     "fills fields omitted from partial response",
			response: map[string]any{"name": "server-name"},
			wantID:   "submitted-id",
			wantName: "server-name",
		},
		{
			name:     "uses submitted collector for empty response",
			response: map[string]any{},
			wantID:   "submitted-id",
			wantName: "new-collector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/collector.v1.CollectorService/CreateCollector")

				var body map[string]json.RawMessage
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

				var col fleet.Collector
				assert.NoError(t, json.Unmarshal(body["collector"], &col))
				assert.Equal(t, "submitted-id", col.ID)
				assert.Equal(t, "new-collector", col.Name)
				assert.Equal(t, "alloy", col.CollectorType)

				writeJSON(w, tt.response)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			created, err := client.CreateCollector(context.Background(), fleet.Collector{
				ID:            "submitted-id",
				Name:          "new-collector",
				CollectorType: "alloy",
			})

			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, tt.wantID, created.ID)
			assert.Equal(t, tt.wantName, created.Name)
			assert.Equal(t, "alloy", created.CollectorType)
		})
	}
}

func TestClient_DeleteCollector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/collector.v1.CollectorService/DeleteCollector")

		var body map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "c-1", body["id"])

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteCollector(context.Background(), "c-1")

	require.NoError(t, err)
}

func TestClient_GetLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/tenant.v1.TenantService/GetLimits")

		writeJSON(w, map[string]any{
			"collectors":                    100,
			"pipelines":                     50,
			"requests_per_second_collector": 10,
			"requests_per_second_api":       20,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	limits, err := client.GetLimits(context.Background())

	require.NoError(t, err)
	require.NotNil(t, limits)
	require.NotNil(t, limits.Collectors)
	assert.Equal(t, int64(100), *limits.Collectors)
	require.NotNil(t, limits.Pipelines)
	assert.Equal(t, int64(50), *limits.Pipelines)
	require.NotNil(t, limits.RequestsPerSecondCollector)
	assert.Equal(t, int64(10), *limits.RequestsPerSecondCollector)
	require.NotNil(t, limits.RequestsPerSecondAPI)
	assert.Equal(t, int64(20), *limits.RequestsPerSecondAPI)
}

// A 404 has two meanings behind the plugin proxy: Fleet Management has no such
// resource, or Grafana has no such plugin route. The two must not be confused.
func TestClient_PluginRouteMissing(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantTypedErr bool
		wantMessage  string
	}{
		{
			name:         "missing resource stays a not found message",
			body:         `{"code":"not_found","message":"pipeline not found"}`,
			wantTypedErr: false,
			wantMessage:  "not found",
		},
		{
			name:         "missing plugin route returns the typed error",
			body:         `{"message":"plugin route match not found"}`,
			wantTypedErr: true,
			wantMessage:  "plugin route match not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := newTestClient(t, server)

			_, err := client.GetPipeline(context.Background(), "123")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMessage)

			var httpErr *fleetbase.HTTPError
			assert.Equal(t, tt.wantTypedErr, errors.As(err, &httpErr))

			_, err = client.GetCollector(context.Background(), "123")
			require.Error(t, err)
			assert.Equal(t, tt.wantTypedErr, errors.As(err, &httpErr))
		})
	}
}

// Every non-2xx from a mutation must carry the typed error, so that
// cmd/gcx/fail can map the status to an actionable message.
func TestClient_MutationsReturnTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)

	tests := []struct {
		name string
		call func() error
	}{
		{"create pipeline", func() error {
			_, err := client.CreatePipeline(context.Background(), fleet.Pipeline{Name: "p"})
			return err
		}},
		{"update pipeline", func() error {
			return client.UpdatePipeline(context.Background(), "1", fleet.Pipeline{Name: "p"})
		}},
		{"delete pipeline", func() error { return client.DeletePipeline(context.Background(), "1") }},
		{"create collector", func() error {
			_, err := client.CreateCollector(context.Background(), fleet.Collector{Name: "c"})
			return err
		}},
		{"update collector", func() error {
			return client.UpdateCollector(context.Background(), fleet.Collector{ID: "1", Name: "c"})
		}},
		{"delete collector", func() error { return client.DeleteCollector(context.Background(), "1") }},
		{"list pipelines", func() error {
			_, err := client.ListPipelines(context.Background())
			return err
		}},
		{"list collectors", func() error {
			_, err := client.ListCollectors(context.Background())
			return err
		}},
		{"get limits", func() error {
			_, err := client.GetLimits(context.Background())
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)

			var httpErr *fleetbase.HTTPError
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, http.StatusForbidden, httpErr.Status)
		})
	}
}

package faro_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *faro.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := faro.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns converted FaroApp slice",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app", r.URL.Path)
				writeJSON(w, []map[string]any{
					{
						"id":   42,
						"name": "my-web-app",
						"extraLogLabels": []map[string]string{
							{"key": "team", "value": "frontend"},
						},
					},
					{
						"id":   43,
						"name": "my-other-app",
					},
				})
			},
			wantLen: 2,
		},
		{
			name: "returns empty list",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, []map[string]any{})
			},
			wantLen: 0,
		},
		{
			name: "propagates server error",
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

			c := newTestClient(t, server)
			result, err := c.List(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)

			if tt.wantLen > 0 {
				assert.Equal(t, "42", result[0].ID)
				assert.Equal(t, "my-web-app", result[0].Name)
				assert.Equal(t, "frontend", result[0].ExtraLogLabels["team"])
			}
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "returns single converted FaroApp",
			id:   "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42", r.URL.Path)
				writeJSON(w, map[string]any{
					"id":   42,
					"name": "my-web-app",
				})
			},
			wantID: "42",
		},
		{
			name: "returns error on 404",
			id:   "999",
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

			c := newTestClient(t, server)
			result, err := c.Get(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, result.ID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	t.Run("strips ExtraLogLabels and Settings from request body", func(t *testing.T) {
		var capturedBody map[string]any
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == http.MethodPost {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &capturedBody)
				writeJSON(w, map[string]any{
					"id":   100,
					"name": "new-app",
				})
				return
			}
			// GET for list (re-fetch after create)
			writeJSON(w, []map[string]any{
				{
					"id":                 100,
					"name":               "new-app",
					"appKey":             "abc-key",
					"collectEndpointURL": "https://collect.example.com",
				},
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		app := &faro.FaroApp{
			Name: "new-app",
			CORSOrigins: []faro.CORSOrigin{
				{URL: "https://example.com"},
			},
			ExtraLogLabels: map[string]string{
				"team": "frontend",
			},
			Settings: &faro.FaroAppSettings{
				GeolocationEnabled: true,
				GeolocationLevel:   "country",
			},
		}

		result, err := c.Create(t.Context(), app)
		require.NoError(t, err)

		// Verify ExtraLogLabels was stripped from request.
		assert.Nil(t, capturedBody["extraLogLabels"], "extraLogLabels should be stripped from create request")
		// Verify Settings was stripped from request.
		assert.Nil(t, capturedBody["settings"], "settings should be stripped from create request")

		// Re-fetched via list returns full details.
		assert.Equal(t, "100", result.ID)
		assert.Equal(t, "abc-key", result.AppKey)
		assert.Equal(t, "https://collect.example.com", result.CollectEndpointURL)
	})
}

func TestClient_Update(t *testing.T) {
	t.Run("strips Settings and includes ID in body", func(t *testing.T) {
		var capturedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42", r.URL.Path)
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &capturedBody)
			writeJSON(w, map[string]any{
				"id":   42,
				"name": "updated-app",
			})
		}))
		defer server.Close()

		c := newTestClient(t, server)
		app := &faro.FaroApp{
			Name: "updated-app",
			CORSOrigins: []faro.CORSOrigin{
				{URL: "https://example.com"},
			},
			ExtraLogLabels: map[string]string{
				"team": "frontend",
			},
			Settings: &faro.FaroAppSettings{
				GeolocationEnabled: true,
			},
		}

		result, err := c.Update(t.Context(), "42", app)
		require.NoError(t, err)

		// Settings should be stripped.
		assert.Nil(t, capturedBody["settings"], "settings should be stripped from update request")
		// ID should be present in body.
		assert.InDelta(t, float64(42), capturedBody["id"], 0.01, "id should be in update request body")
		assert.Equal(t, "42", result.ID)
	})
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "returns nil on 204",
			id:   "42",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name: "returns error on 404",
			id:   "999",
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

			c := newTestClient(t, server)
			err := c.Delete(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestClient_ListSourcemaps(t *testing.T) {
	tests := []struct {
		name    string
		appID   string
		limit   int
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name:  "default limit 100 with single page",
			appID: "42",
			limit: 0,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				assert.Equal(t, "100", r.URL.Query().Get("limit"))
				assert.Empty(t, r.URL.Query().Get("page"))
				writeJSON(w, map[string]any{
					"bundles": []map[string]any{
						{"ID": "bundle-1", "Created": "2024-01-01T00:00:00Z", "Updated": "2024-01-02T00:00:00Z"},
						{"ID": "bundle-2", "Created": "2024-01-03T00:00:00Z", "Updated": "2024-01-04T00:00:00Z"},
					},
					"page": map[string]any{
						"hasNext":    false,
						"next":       "",
						"limit":      100,
						"totalItems": 2,
					},
				})
			},
			wantLen: 2,
		},
		{
			name:  "respects explicit limit without pagination",
			appID: "42",
			limit: 50,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				assert.Equal(t, "50", r.URL.Query().Get("limit"))
				writeJSON(w, map[string]any{
					"bundles": []map[string]any{
						{"ID": "bundle-1", "Created": "2024-01-01T00:00:00Z", "Updated": "2024-01-02T00:00:00Z"},
					},
					"page": map[string]any{
						"hasNext":    true,
						"next":       "page2",
						"limit":      50,
						"totalItems": 2,
					},
				})
			},
			wantLen: 1,
		},
		{
			name:  "auto-pagination with limit=0",
			appID: "42",
			limit: 0,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps", r.URL.Path)
				assert.Equal(t, "100", r.URL.Query().Get("limit"))

				switch pageParam := r.URL.Query().Get("page"); pageParam {
				case "":
					// First page
					writeJSON(w, map[string]any{
						"bundles": []map[string]any{
							{"ID": "bundle-1", "Created": "2024-01-01T00:00:00Z", "Updated": "2024-01-02T00:00:00Z"},
						},
						"page": map[string]any{
							"hasNext":    true,
							"next":       "page2",
							"limit":      100,
							"totalItems": 2,
						},
					})
				case "page2":
					// Second page
					writeJSON(w, map[string]any{
						"bundles": []map[string]any{
							{"ID": "bundle-2", "Created": "2024-01-03T00:00:00Z", "Updated": "2024-01-04T00:00:00Z"},
						},
						"page": map[string]any{
							"hasNext":    false,
							"next":       "",
							"limit":      100,
							"totalItems": 2,
						},
					})
				default:
					http.Error(w, "unexpected page param: "+pageParam, http.StatusBadRequest)
				}
			},
			wantLen: 2,
		},
		{
			name:  "propagates server error",
			appID: "42",
			limit: 0,
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

			c := newTestClient(t, server)
			result, err := c.ListSourcemaps(t.Context(), tt.appID, tt.limit)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

func TestClient_DeleteSourcemaps(t *testing.T) {
	tests := []struct {
		name      string
		appID     string
		bundleIDs []string
		handler   http.HandlerFunc
		wantErr   bool
	}{
		{
			name:      "single bundle ID",
			appID:     "42",
			bundleIDs: []string{"bundle-1"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps/batch/bundle-1", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name:      "multiple bundle IDs",
			appID:     "42",
			bundleIDs: []string{"bundle-1", "bundle-2", "bundle-3"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/42/sourcemaps/batch/bundle-1,bundle-2,bundle-3", r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			},
		},
		{
			name:      "server error",
			appID:     "42",
			bundleIDs: []string{"bundle-1"},
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

			c := newTestClient(t, server)
			err := c.DeleteSourcemaps(t.Context(), tt.appID, tt.bundleIDs)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestListRecordings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/sessions/sess-abc/recordings", r.URL.Path)
		assert.Equal(t, "app-42", r.URL.Query().Get("app_id"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))

		writeJSON(w, faro.SessionRecordingsListResponse{
			SessionID: "sess-abc",
			Items: []faro.RecordingListItem{
				{ID: "rec-1", Status: "complete"},
				{ID: "rec-2", Status: "in_progress"},
			},
			Page: faro.SessionPage{
				HasNext:    false,
				Limit:      10,
				TotalItems: 2,
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	resp, err := c.ListRecordings(t.Context(), "app-42", "sess-abc", 10)

	require.NoError(t, err)
	assert.Equal(t, "sess-abc", resp.SessionID)
	assert.Len(t, resp.Items, 2)
	assert.Equal(t, "rec-1", resp.Items[0].ID)
	assert.Equal(t, "rec-2", resp.Items[1].ID)
}

func TestListRecordingsAutoPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "app-42", r.URL.Query().Get("app_id"))
		assert.Equal(t, "50", r.URL.Query().Get("limit"))

		switch pageParam := r.URL.Query().Get("page"); pageParam {
		case "":
			writeJSON(w, faro.SessionRecordingsListResponse{
				SessionID: "sess-abc",
				Items: []faro.RecordingListItem{
					{ID: "rec-1", Status: "complete"},
				},
				Page: faro.SessionPage{
					HasNext:    true,
					Next:       "cursor-page2",
					Limit:      50,
					TotalItems: 3,
				},
			})
		case "cursor-page2":
			writeJSON(w, faro.SessionRecordingsListResponse{
				SessionID: "sess-abc",
				Items: []faro.RecordingListItem{
					{ID: "rec-2", Status: "complete"},
					{ID: "rec-3", Status: "complete"},
				},
				Page: faro.SessionPage{
					HasNext:    false,
					Limit:      50,
					TotalItems: 3,
				},
			})
		default:
			http.Error(w, "unexpected page param: "+pageParam, http.StatusBadRequest)
		}
	}))
	defer server.Close()

	c := newTestClient(t, server)
	resp, err := c.ListRecordings(t.Context(), "app-42", "sess-abc", 0)

	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.Len(t, resp.Items, 3)
	assert.Equal(t, "rec-1", resp.Items[0].ID)
	assert.Equal(t, "rec-2", resp.Items[1].ID)
	assert.Equal(t, "rec-3", resp.Items[2].ID)
}

func TestGetManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/sessions/sess-abc/recordings/rec-1/manifest", r.URL.Path)
		assert.Equal(t, "app-42", r.URL.Query().Get("app_id"))

		writeJSON(w, faro.RecordingManifestResponse{
			ID:        "rec-1",
			SessionID: "sess-abc",
			Status:    "complete",
			Segments: []faro.ManifestSegment{
				{ID: 0, StartOffsetMs: 0, EndOffsetMs: 5000},
				{ID: 1, StartOffsetMs: 5000, EndOffsetMs: 10000, RequiresSegmentID: new(int64)},
			},
			InactivityPeriods: []faro.InactivityPeriod{
				{StartOffsetMs: 2000, EndOffsetMs: 3000},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	resp, err := c.GetManifest(t.Context(), "app-42", "sess-abc", "rec-1")

	require.NoError(t, err)
	assert.Equal(t, "rec-1", resp.ID)
	assert.Equal(t, "sess-abc", resp.SessionID)
	assert.Equal(t, "complete", resp.Status)

	require.Len(t, resp.Segments, 2)
	assert.Equal(t, int64(0), resp.Segments[0].ID)
	assert.Nil(t, resp.Segments[0].RequiresSegmentID)
	assert.Equal(t, int64(1), resp.Segments[1].ID)
	require.NotNil(t, resp.Segments[1].RequiresSegmentID)
	assert.Equal(t, int64(0), *resp.Segments[1].RequiresSegmentID)

	require.Len(t, resp.InactivityPeriods, 1)
	assert.Equal(t, int64(2000), resp.InactivityPeriods[0].StartOffsetMs)
	assert.Equal(t, int64(3000), resp.InactivityPeriods[0].EndOffsetMs)
}

func TestGetSegment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/sessions/sess-abc/recordings/rec-1/segments/seg-0", r.URL.Path)
		assert.Equal(t, "app-42", r.URL.Query().Get("app_id"))

		writeJSON(w, faro.RecordingSegmentResponse{
			ID:          "seg-0",
			RecordingID: "rec-1",
			Events: []faro.RRWebEvent{
				{Type: 4, Timestamp: 1700000000000, Data: json.RawMessage(`{"source":0}`)},
				{Type: 3, Timestamp: 1700000001000, Data: json.RawMessage(`{"source":1}`)},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server)
	resp, err := c.GetSegment(t.Context(), "app-42", "sess-abc", "rec-1", "seg-0")

	require.NoError(t, err)
	assert.Equal(t, "seg-0", resp.ID)
	assert.Equal(t, "rec-1", resp.RecordingID)

	require.Len(t, resp.Events, 2)
	assert.Equal(t, 4, resp.Events[0].Type)
	assert.Equal(t, int64(1700000000000), resp.Events[0].Timestamp)
	assert.Equal(t, 3, resp.Events[1].Type)
	assert.JSONEq(t, `{"source":0}`, string(resp.Events[0].Data))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

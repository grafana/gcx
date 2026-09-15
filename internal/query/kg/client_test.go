package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *kg.Client {
	t.Helper()
	c, err := kg.NewClient(newRESTConfig(server.URL))
	require.NoError(t, err)
	return c
}

func newRESTConfig(host string) config.NamespacedRESTConfig {
	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: host},
		Namespace: "stack-123",
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func TestNewClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	assert.Equal(t, server.URL, client.Host())
	assert.Equal(t, "stack-123", client.Namespace())
	assert.NotNil(t, client.HTTPClient())
}

func TestClient_GetJSON(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "decodes response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/some/path", r.URL.Path)
				writeJSON(w, map[string]string{"foo": "bar"})
			},
		},
		{
			name: "error status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("boom"))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			var v map[string]string
			err := client.GetJSON(t.Context(), "/some/path", &v)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "bar", v["foo"])
		})
	}
}

func TestClient_PostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "baz", body["req"])
		writeJSON(w, map[string]string{"resp": "ok"})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	var v map[string]string
	err := client.PostJSON(t.Context(), "/post/path", map[string]string{"req": "baz"}, &v)
	require.NoError(t, err)
	assert.Equal(t, "ok", v["resp"])
}

func TestClient_DoJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	err := client.DoJSON(t.Context(), http.MethodPut, "/put/path", map[string]string{"a": "b"}, nil)
	require.NoError(t, err)
}

func TestClient_DoJSONStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]string{"id": "1"})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	var v map[string]string
	status, err := client.DoJSONStatus(t.Context(), http.MethodPost, "/create", nil, &v)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, status)
	assert.Equal(t, "1", v["id"])
}

func TestClient_DoYAML(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "application/x-yaml", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "error status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			err := client.DoYAML(t.Context(), http.MethodPut, "/yaml/path", "content: value")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestReadError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantMsg    string
	}{
		{
			name:       "json message body",
			statusCode: http.StatusBadRequest,
			body:       `{"message": "invalid request"}`,
			wantMsg:    "invalid request",
		},
		{
			name:       "raw body fallback",
			statusCode: http.StatusInternalServerError,
			body:       "not json",
			wantMsg:    "not json",
		},
		{
			name:       "empty body",
			statusCode: http.StatusServiceUnavailable,
			body:       "",
			wantMsg:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			resp, err := http.Get(server.URL) //nolint:noctx
			require.NoError(t, err)
			defer resp.Body.Close()

			apiErr := kg.ReadError(resp)
			require.NotNil(t, apiErr)
			assert.Equal(t, tt.statusCode, apiErr.HTTPStatusCode())
			assert.Equal(t, tt.wantMsg, apiErr.APIUserMessage())
		})
	}
}

func TestNewAPIError(t *testing.T) {
	err := kg.NewAPIError(http.StatusNotFound, "not found")
	assert.Equal(t, http.StatusNotFound, err.HTTPStatusCode())
	assert.Equal(t, "not found", err.APIUserMessage())
	assert.False(t, err.IsServerError())

	serverErr := kg.NewAPIError(http.StatusInternalServerError, "boom")
	assert.True(t, serverErr.IsServerError())
}

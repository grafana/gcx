package annotations_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/annotations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *annotations.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := annotations.NewClient(cfg)
	require.NoError(t, err)
	return client
}

// writeJSON encodes v as JSON to w.
// Panics on marshal error since test data is always known-good.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name    string
		opts    annotations.ListOptions
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "success with items",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/annotations", r.URL.Path)
				writeJSON(w, []annotations.Annotation{
					{ID: 1, Text: "one"},
					{ID: 2, Text: "two"},
				})
			},
			wantLen: 2,
		},
		{
			name: "empty result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []annotations.Annotation{})
			},
			wantLen: 0,
		},
		{
			name: "all filter params sent",
			opts: annotations.ListOptions{
				From:  1000,
				To:    2000,
				Tags:  []string{"deploy", "prod"},
				Limit: 50,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				assert.Equal(t, "1000", q.Get("from"))
				assert.Equal(t, "2000", q.Get("to"))
				assert.Equal(t, []string{"deploy", "prod"}, q["tags"])
				assert.Equal(t, "50", q.Get("limit"))
				writeJSON(w, []annotations.Annotation{})
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, annotations.ErrorResponse{Message: "internal"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			got, err := client.List(t.Context(), tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		handler http.HandlerFunc
		wantErr bool
		wantID  int64
	}{
		{
			name: "success",
			id:   42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/annotations/42", r.URL.Path)
				writeJSON(w, annotations.Annotation{ID: 42, Text: "hello"})
			},
			wantID: 42,
		},
		{
			name: "not found",
			id:   99,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, annotations.ErrorResponse{Message: "not found"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			got, err := client.Get(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}

func TestClient_Create(t *testing.T) {
	tests := []struct {
		name    string
		input   annotations.Annotation
		handler http.HandlerFunc
		wantErr bool
		wantID  int64
	}{
		{
			name:  "success sets ID",
			input: annotations.Annotation{Text: "deploy v2"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/annotations", r.URL.Path)
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				var in annotations.Annotation
				if !assert.NoError(t, json.Unmarshal(body, &in)) {
					return
				}
				assert.Equal(t, "deploy v2", in.Text)
				writeJSON(w, map[string]any{"id": int64(7), "message": "Annotation added"})
			},
			wantID: 7,
		},
		{
			name:  "server error",
			input: annotations.Annotation{Text: "x"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, annotations.ErrorResponse{Message: "bad"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			a := tt.input
			err := client.Create(t.Context(), &a)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, a.ID)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		patch   map[string]any
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:  "success",
			id:    5,
			patch: map[string]any{"text": "updated"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPatch, r.Method)
				assert.Equal(t, "/api/annotations/5", r.URL.Path)
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				var got map[string]any
				if !assert.NoError(t, json.Unmarshal(body, &got)) {
					return
				}
				assert.Equal(t, "updated", got["text"])
				writeJSON(w, map[string]any{"message": "Annotation patched"})
			},
		},
		{
			name:  "server error",
			id:    5,
			patch: map[string]any{"text": "x"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(w, annotations.ErrorResponse{Message: "forbidden"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Update(t.Context(), tt.id, tt.patch)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			id:   11,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/annotations/11", r.URL.Path)
				writeJSON(w, map[string]any{"message": "Annotation deleted"})
			},
		},
		{
			name: "server error",
			id:   11,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, annotations.ErrorResponse{Message: "boom"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.Delete(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       func(w http.ResponseWriter)
		wantErrMsg string
	}{
		{
			name:       "401 with JSON body",
			statusCode: http.StatusUnauthorized,
			body: func(w http.ResponseWriter) {
				writeJSON(w, annotations.ErrorResponse{Message: "unauthorized"})
			},
			wantErrMsg: "401",
		},
		{
			name:       "500 with plain text body",
			statusCode: http.StatusInternalServerError,
			body: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("internal server error"))
			},
			wantErrMsg: "500",
		},
		{
			name:       "500 with empty body",
			statusCode: http.StatusInternalServerError,
			body:       func(w http.ResponseWriter) {},
			wantErrMsg: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				tt.body(w)
			}))
			defer server.Close()

			client := newTestClient(t, server)
			_, err := client.List(t.Context(), annotations.ListOptions{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}

// Sanity check: tag order must be preserved as per repeated ?tags=... semantics.
func TestClient_List_TagOrder(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		writeJSON(w, []annotations.Annotation{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.List(t.Context(), annotations.ListOptions{Tags: []string{"a", "b", "c"}})
	require.NoError(t, err)
	// Each tag should appear as a separate query value.
	assert.Equal(t, 3, strings.Count(capturedQuery, "tags="))
}

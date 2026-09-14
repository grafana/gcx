package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	kg "github.com/grafana/gcx/client/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const llmSummaryPath = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/assertions/llm-summary"

func newTestClient(server *httptest.Server) *kg.Client {
	return kg.NewClient(server.Client(), server.URL)
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

func TestClient_LLMSummary(t *testing.T) {
	tests := []struct {
		name    string
		req     kg.LLMSummaryRequest
		handler http.HandlerFunc
		wantErr bool
		wantKey string
	}{
		{
			name: "success",
			req: kg.LLMSummaryRequest{
				StartTime: 1000,
				EndTime:   2000,
				EntityKeys: []kg.EntityKey{{
					Type:  "Service",
					Name:  "checkout",
					Scope: map[string]any{"env": "prod"},
				}},
				SuggestionSrcEntities: []kg.EntityKey{},
				IncludeSuggestions:    true,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, llmSummaryPath, r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var received kg.LLMSummaryRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
				assert.Equal(t, int64(1000), received.StartTime)
				assert.Equal(t, int64(2000), received.EndTime)
				require.Len(t, received.EntityKeys, 1)
				assert.Equal(t, "Service", received.EntityKeys[0].Type)
				assert.Equal(t, "checkout", received.EntityKeys[0].Name)
				assert.Equal(t, map[string]any{"env": "prod"}, received.EntityKeys[0].Scope)
				assert.True(t, received.IncludeSuggestions)

				writeJSON(w, map[string]any{"summary": "all good", "rcaPatterns": []any{}})
			},
			wantKey: "summary",
		},
		{
			name: "empty object response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, map[string]any{})
			},
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(w, map[string]string{"message": "entity not found"})
			},
			wantErr: true,
		},
		{
			name: "invalid json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(server)
			result, err := client.LLMSummary(t.Context(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.wantKey != "" {
				assert.Contains(t, result, tt.wantKey)
			}
		})
	}
}

func TestClient_LLMSummaryErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errBody    map[string]string
		wantErrMsg string
	}{
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			errBody:    map[string]string{"message": "unauthorized"},
			wantErrMsg: "401",
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			errBody:    map[string]string{"message": "entity not found"},
			wantErrMsg: "entity not found",
		},
		{
			name:       "500 internal server error",
			statusCode: http.StatusInternalServerError,
			errBody:    map[string]string{"error": "internal server error"},
			wantErrMsg: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				writeJSON(w, tt.errBody)
			}))
			defer server.Close()

			client := newTestClient(server)
			_, err := client.LLMSummary(t.Context(), kg.LLMSummaryRequest{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)

			var httpErr interface{ HTTPStatusCode() int }
			require.ErrorAs(t, err, &httpErr)
			assert.Equal(t, tt.statusCode, httpErr.HTTPStatusCode())
		})
	}
}

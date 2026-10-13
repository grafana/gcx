package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const expandedSchemas = `{"schemas":[{"domain":{"name":"kg","version":"v1","displayName":"Knowledge Graph"},"imports":[{"domain":"base","version":"v1"}],"entityTypes":[{"name":"Service","properties":[{"name":"owner","valueType":"STRING"}],"futureField":{"enabled":true}}],"relationshipTypes":[{"name":"CALLS","from":"Service","to":"Service"}],"relationshipTypeBindings":[]}]}`

func TestClientListSchemas(t *testing.T) {
	for _, body := range []string{expandedSchemas, `{"schemas":[]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			got, err := newTestClient(t, server).ListSchemas(t.Context(), true, false)
			require.NoError(t, err)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t, body, string(encoded))
		})
	}
}

func TestClientListSchemasErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"schema discovery unavailable"}`))
			}))
			defer server.Close()

			_, err := newTestClient(t, server).ListSchemas(t.Context(), false, true)
			var apiErr *kg.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, status, apiErr.StatusCode)
			assert.Contains(t, err.Error(), "schema discovery unavailable")
		})
	}
}

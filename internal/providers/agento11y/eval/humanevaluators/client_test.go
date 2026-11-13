package humanevaluators_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/humanevaluators"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *humanevaluators.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base, err := agento11yhttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "default"})
	require.NoError(t, err)
	return humanevaluators.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/eval/human-evaluators", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"evaluator_id":"helpfulness-human","evaluator_version":"2026-09-01","description":"Was the answer helpful?","instructions":"Rate 1-5.","output_key":{"key":"helpful","type":"number","min":1,"max":5}}]}`))
	}))

	items, err := client.List(t.Context())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "helpfulness-human", items[0].EvaluatorID)
	assert.Equal(t, "2026-09-01", items[0].EvaluatorVersion)
	assert.Equal(t, "helpful", items[0].OutputKey.Key)
	assert.Equal(t, "Rate 1-5.", items[0].Instructions)
}

func TestClient_List_Error(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.List(t.Context())
	require.Error(t, err)
}

func TestTableGolden(t *testing.T) {
	var row humanevaluators.RubricItem
	require.NoError(t, json.Unmarshal([]byte(`{"evaluator_id":"helpfulness-human","evaluator_version":"2026-09-01","description":"Was the answer helpful?","instructions":"Rate the answer from 1 to 5.","output_key":{"key":"helpful","type":"number"}}`), &row))
	testutils.AssertTableGolden(t, humanevaluators.Table(), row, "table", "wide")
}

package fleet //nolint:testpackage // Exercises unexported name resolution contracts.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePipelinePlainNameMissingWrapsNotFound(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pipelines []Pipeline
	}{
		{"empty list", nil},
		{"other pipeline exists", []Pipeline{{ID: "123", Name: "another-pipeline"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, pathListPipelines, r.URL.Path, "a plain name only needs a name lookup")
				writeContractJSON(t, w, map[string]any{"pipelines": tc.pipelines})
			}))
			t.Cleanup(server.Close)

			pipeline, err := resolvePipeline(context.Background(), NewClient(context.Background(), server.URL, server.Client()), "absent-name")
			require.ErrorIs(t, err, adapter.ErrNotFound)
			assert.Nil(t, pipeline)
			assert.Contains(t, err.Error(), `pipeline "absent-name"`)
			assert.Equal(t, 1, calls)
		})
	}
}

package fleet //nolint:testpackage // Exercises unexported name resolution contracts.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The typed adapter turns adapter.ErrNotFound into a Kubernetes NotFound,
// which makes push create and multi-get skip the selector. resolvePipeline
// is not a strict identity lookup, so its name miss must not carry it.
func TestResolvePipelinePlainNameMissingIsNotAdapterNotFound(t *testing.T) {
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
			require.Error(t, err)
			require.NotErrorIs(t, err, adapter.ErrNotFound)
			var missing *fleetbase.PipelineNotFoundError
			require.ErrorAs(t, err, &missing)
			assert.Equal(t, "absent-name", missing.Name)
			assert.Nil(t, pipeline)
			assert.Equal(t, `pipeline "absent-name" not found`, err.Error())
			assert.Equal(t, 1, calls)
		})
	}
}

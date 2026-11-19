package observation_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrashReader(t *testing.T) {
	item := func(name string) string {
		return fmt.Sprintf(`{"apiVersion":"dashboard.grafana.app/v1","kind":"Dashboard","metadata":{"name":%q,"resourceVersion":"7"},"spec":{"title":%q,"version":3}}`, name, name)
	}
	t.Run("follows continue tokens and normalizes entries", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "grafana.app/get-trash=true", r.URL.Query().Get("labelSelector"))
			if r.URL.Query().Get("continue") == "" {
				_, _ = fmt.Fprintf(w, `{"metadata":{"continue":"p2"},"items":[%s]}`, item("a"))
				return
			}
			_, _ = fmt.Fprintf(w, `{"metadata":{},"items":[%s]}`, item("b"))
		}))
		defer srv.Close()
		cov, objs, err := observation.NewTrashReader(srv.Client(), srv.URL, "default")(context.Background(), "v1")
		require.NoError(t, err)
		assert.Equal(t, observation.CoverageComplete, cov.Status)
		assert.Equal(t, 2, cov.Count)
		assert.Equal(t, map[string]any{"title": "a"}, objs["a"]["spec"])
	})
	t.Run("not served is unavailable", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()
		cov, objs, err := observation.NewTrashReader(srv.Client(), srv.URL, "default")(context.Background(), "v1")
		require.NoError(t, err)
		assert.Equal(t, observation.CoverageUnavailable, cov.Status)
		assert.Nil(t, objs)
	})
	t.Run("server error is incomplete and keeps nothing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		cov, objs, err := observation.NewTrashReader(srv.Client(), srv.URL, "default")(context.Background(), "v1")
		require.NoError(t, err)
		assert.Equal(t, observation.CoverageIncomplete, cov.Status)
		assert.Empty(t, objs)
	})
}

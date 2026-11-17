package search_test

import (
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestSearchTableGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		hits []testServerHit
	}{
		{name: "rows", hits: []testServerHit{
			{Name: "dash-1", Title: "API overview", Tags: []string{"prod", "api"}},
			{Name: "dash-2", Title: "Database overview", Folder: "databases"},
		}},
		{name: "empty", hits: []testServerHit{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, loader := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSONResponse(w, testServerResponse{Hits: tc.hits})
			})
			defer srv.Close()
			for _, name := range []string{"table", "wide"} {
				t.Run(name, func(t *testing.T) {
					out, err := runSearchCommand(t, loader, "overview", "-o", name)
					require.NoError(t, err)
					testutils.Golden(t, "search_"+name+"_"+tc.name, out)
				})
			}
		})
	}
}

package faro //nolint:testpackage // Tests the unexported resolver and drives the command constructors.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindApp(t *testing.T) {
	apps := []FaroApp{
		{ID: "1", Name: "web"},
		{ID: "2", Name: "checkout-1"},
		{ID: "3", Name: "web-1"},
		{ID: "4", Name: "7"},
	}
	tests := []struct {
		name    string
		arg     string
		want    int
		wantErr string
	}{
		{name: "display name", arg: "checkout-1", want: 1},
		{name: "slug-id", arg: "checkout-1-2", want: 1},
		{name: "slug-id in another case", arg: "Checkout-1-2", want: 1},
		{name: "numeric ID", arg: "2", want: 1},
		{name: "display name that ends in another app's ID", arg: "checkout-1", want: 1},
		{name: "numeric display name with no app at that ID", arg: "7", want: 3},
		{name: "one app's name and another app's slug-id", arg: "web-1", wantErr: "ambiguous"},
		{name: "slug-id with another app's slug", arg: "checkout-1", want: 1},
		{name: "unknown", arg: "nope", wantErr: "no app has that slug-id or name"},
		{name: "unknown slug-id", arg: "nope-1", wantErr: "no app has that slug-id or name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := findApp(apps, tc.arg)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, apps[tc.want].ID, apps[got].ID)
		})
	}
}

// fakeAppAPI serves app CRUD for the named apps (IDs 1..n) and records every
// mutating request.
type fakeAppAPI struct {
	apps      []map[string]any
	mutations []string
	puts      []map[string]any
}

func newFakeAppAPI(t *testing.T, apps ...map[string]any) (*httptest.Server, *fakeAppAPI) {
	t.Helper()
	f := &fakeAppAPI{apps: apps}
	byID := func(id string) map[string]any {
		for _, a := range f.apps {
			if fmt.Sprint(a["id"]) == id {
				return a
			}
		}
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc(basePath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.apps)
	})
	mux.HandleFunc(basePath+"/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, basePath+"/")
		app := byID(id)
		if app == nil {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(app)
		case http.MethodDelete:
			f.mutations = append(f.mutations, "DELETE "+id)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			f.mutations = append(f.mutations, "PUT "+id)
			body, _ := io.ReadAll(r.Body)
			var put map[string]any
			_ = json.Unmarshal(body, &put)
			f.puts = append(f.puts, put)
			_, _ = w.Write(body)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, f
}

func TestFaroDelete_Resolution(t *testing.T) {
	tests := []struct {
		name          string
		arg           string
		wantMutations []string
		wantErr       string
	}{
		// probe-1 ends in the ID of QuickPizza (app 1). It must delete itself.
		{name: "name ending in another app's ID", arg: "probe-1", wantMutations: []string{"DELETE 8"}},
		{name: "display name", arg: "QuickPizza", wantMutations: []string{"DELETE 1"}},
		{name: "slug-id", arg: "probe-1-8", wantMutations: []string{"DELETE 8"}},
		{name: "ambiguous", arg: "quickpizza-1", wantErr: "ambiguous"},
		{name: "unknown", arg: "nope-1", wantErr: "no app has that slug-id or name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, api := newFakeAppAPI(t,
				map[string]any{"id": 1, "name": "QuickPizza"},
				map[string]any{"id": 8, "name": "probe-1"},
				map[string]any{"id": 9, "name": "quickpizza-1"},
			)
			stdout, _, err := runFaroCommand(t, server, func(l *fakeConfigLoader) *cobra.Command {
				return newDeleteCommand(l)
			}, []string{tc.arg, "-o", "json"})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Empty(t, api.mutations, "nothing may be deleted")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantMutations, api.mutations)
			id := strings.TrimPrefix(tc.wantMutations[0], "DELETE ")
			assert.Contains(t, stdout, `"id": "`+id+`"`)
		})
	}
}

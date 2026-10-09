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

// fakeAppAPI serves app CRUD, sourcemap list and delete, and sourcemap upload
// for the given apps. It records every call except app reads.
type fakeAppAPI struct {
	apps  []map[string]any
	calls []string
	puts  []map[string]any
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
		id, sub, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, basePath+"/"), "/")
		app := byID(id)
		if app == nil {
			http.NotFound(w, r)
			return
		}
		if sub != "" {
			f.calls = append(f.calls, r.Method+" "+id+"/"+sub)
			_ = json.NewEncoder(w).Encode(map[string]any{"bundles": []any{}})
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(app)
		case http.MethodDelete:
			f.calls = append(f.calls, "DELETE "+id)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			f.calls = append(f.calls, "PUT "+id)
			body, _ := io.ReadAll(r.Body)
			var put map[string]any
			_ = json.Unmarshal(body, &put)
			f.puts = append(f.puts, put)
			_, _ = w.Write(body)
		}
	})
	// Direct Faro API sourcemap upload.
	mux.HandleFunc("/api/v1/app/", func(w http.ResponseWriter, r *http.Request) {
		f.calls = append(f.calls, "UPLOAD "+strings.TrimPrefix(r.URL.Path, "/api/v1/app/"))
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, f
}

func TestFaroDelete_Resolution(t *testing.T) {
	tests := []struct {
		name      string
		arg       string
		wantCalls []string
		wantErr   string
	}{
		// probe-1 ends in the ID of QuickPizza (app 1). It must delete itself.
		{name: "name ending in another app's ID", arg: "probe-1", wantCalls: []string{"DELETE 8"}},
		{name: "display name", arg: "QuickPizza", wantCalls: []string{"DELETE 1"}},
		{name: "slug-id", arg: "probe-1-8", wantCalls: []string{"DELETE 8"}},
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
				assert.Empty(t, api.calls, "nothing may be deleted")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantCalls, api.calls)
			id := strings.TrimPrefix(tc.wantCalls[0], "DELETE ")
			assert.Contains(t, stdout, `"id": "`+id+`"`)
		})
	}
}

func TestFaroCommands_AcceptDisplayName(t *testing.T) {
	manifest := writeTestFile(t, "app.yaml", "kind: FaroApp\nspec: {name: My Web App}\n")
	sourcemap := writeTestFile(t, "bundle.js.map", `{"version":3}`)
	tests := []struct {
		name      string
		build     func(l *fakeConfigLoader) *cobra.Command
		args      []string
		wantCalls []string
	}{
		{
			name:      "apps update",
			build:     func(l *fakeConfigLoader) *cobra.Command { return newUpdateCommand(l) },
			args:      []string{"My Web App", "-f", manifest},
			wantCalls: []string{"PUT 8"},
		},
		{
			name:      "apps list-sourcemaps",
			build:     func(l *fakeConfigLoader) *cobra.Command { return newListSourcemapsCommand(l) },
			args:      []string{"My Web App", "--limit", "5"},
			wantCalls: []string{"GET 8/sourcemaps"},
		},
		{
			name:      "apps delete-sourcemap",
			build:     func(l *fakeConfigLoader) *cobra.Command { return newDeleteSourcemapCommand(l) },
			args:      []string{"My Web App", "b1"},
			wantCalls: []string{"DELETE 8/sourcemaps/batch/b1"},
		},
		{
			name:      "apps apply-sourcemap",
			build:     func(l *fakeConfigLoader) *cobra.Command { return newApplySourcemapCommand(l) },
			args:      []string{"My Web App", "-f", sourcemap, "--bundle-id", "b1"},
			wantCalls: []string{"UPLOAD 8/sourcemaps/b1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// "My Web App" is app 8; app 1 exists so a stray ID lookup would show.
			server, api := newFakeAppAPI(t,
				map[string]any{"id": 1, "name": "QuickPizza"},
				map[string]any{"id": 8, "name": "My Web App"},
			)
			_, _, err := runFaroCommand(t, server, tc.build, append(tc.args, "-o", "json"))
			require.NoError(t, err)
			assert.Equal(t, tc.wantCalls, api.calls)
		})
	}
}

func TestFaroUpdate_RefusesAnotherAppsName(t *testing.T) {
	tests := []struct {
		name      string
		specName  string
		wantErr   string
		wantPutAs string
	}{
		{name: "same name", specName: "checkout", wantPutAs: "checkout"},
		// Slugs match, so this is the same app; the stored name is kept.
		{name: "same slug in another form", specName: "Checkout", wantPutAs: "checkout"},
		{name: "another name", specName: "checkout-staging", wantErr: `is named "checkout", not "checkout-staging"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, api := newFakeAppAPI(t, map[string]any{"id": 42, "name": "checkout"})
			manifest := writeTestFile(t, "app.yaml", "kind: FaroApp\nmetadata: {name: checkout-42}\nspec: {name: "+tc.specName+"}\n")
			stdout, _, err := runFaroCommand(t, server, func(l *fakeConfigLoader) *cobra.Command {
				return newUpdateCommand(l)
			}, []string{"checkout-42", "-f", manifest, "-o", "json"})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Empty(t, api.calls, "nothing may be written")
				return
			}
			require.NoError(t, err)
			require.Len(t, api.puts, 1)
			assert.Equal(t, tc.wantPutAs, api.puts[0]["name"])
			assert.Contains(t, stdout, `"name": "`+tc.wantPutAs+`"`)
		})
	}
}

func TestFaroUpdate_KeepsOmittedLists(t *testing.T) {
	storedCORS := []any{map[string]any{"url": "https://a.example.com"}}
	storedLabels := []any{map[string]any{"label": "team", "value": "rum"}}
	tests := []struct {
		name       string
		spec       string
		wantCORS   any
		wantLabels any
	}{
		{name: "omitted lists keep the stored ones", spec: "{name: web}", wantCORS: storedCORS, wantLabels: storedLabels},
		// The API clears a list that the body leaves out.
		{name: "explicit empty lists clear them", spec: "{name: web, corsOrigins: [], extraLogLabels: {}}"},
		{
			name:       "a new list replaces only that list",
			spec:       `{name: web, corsOrigins: [{url: "https://b.example.com"}]}`,
			wantCORS:   []any{map[string]any{"url": "https://b.example.com"}},
			wantLabels: storedLabels,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, api := newFakeAppAPI(t, map[string]any{
				"id": 42, "name": "web",
				"corsOrigins":    []any{map[string]any{"id": 7, "url": "https://a.example.com"}},
				"extraLogLabels": []any{map[string]any{"id": 9, "label": "team", "value": "rum"}},
			})
			manifest := writeTestFile(t, "app.yaml", "kind: FaroApp\nspec: "+tc.spec+"\n")
			_, _, err := runFaroCommand(t, server, func(l *fakeConfigLoader) *cobra.Command {
				return newUpdateCommand(l)
			}, []string{"web-42", "-f", manifest, "-o", "json"})
			require.NoError(t, err)
			require.Len(t, api.puts, 1)
			assert.Equal(t, tc.wantCORS, api.puts[0]["corsOrigins"])
			assert.Equal(t, tc.wantLabels, api.puts[0]["extraLogLabels"])
		})
	}
}

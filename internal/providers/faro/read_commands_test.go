package faro //nolint:testpackage // Drives the unexported command constructors through the loader seams.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMultiAppServer serves n apps named "App <i>" with IDs 1..n.
func newMultiAppServer(t *testing.T, n int) *httptest.Server {
	t.Helper()
	names := make([]string, n)
	for i := range n {
		names[i] = fmt.Sprintf("App %d", i+1)
	}
	return newNamedAppServer(t, names)
}

// newNamedAppServer serves one app per name, with IDs 1..len(names).
func newNamedAppServer(t *testing.T, names []string) *httptest.Server {
	t.Helper()
	apps := make([]map[string]any, len(names))
	byID := map[string]map[string]any{}
	for i, name := range names {
		apps[i] = map[string]any{"id": i + 1, "name": name}
		byID[strconv.Itoa(i+1)] = apps[i]
	}

	mux := http.NewServeMux()
	mux.HandleFunc(basePath, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(apps)
	})
	mux.HandleFunc(basePath+"/", func(w http.ResponseWriter, r *http.Request) {
		app, ok := byID[strings.TrimPrefix(r.URL.Path, basePath+"/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(app)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestFaroList_TruncationHint(t *testing.T) {
	withPlainColors(t)
	server := newMultiAppServer(t, 5)

	tests := []struct {
		name     string
		args     []string
		wantRows int
		wantHint string
	}{
		{name: "limit below total warns", args: []string{"--limit", "2"}, wantRows: 2, wantHint: "showing first 2 of 5"},
		{name: "limit at total is silent", args: []string{"--limit", "5"}, wantRows: 5},
		{name: "unlimited is silent", args: []string{"--limit", "0"}, wantRows: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runFaroCommand(t, server, func(l *fakeConfigLoader) *cobra.Command {
				return newListCommand(l)
			}, append(tc.args, "-o", "json"))
			require.NoError(t, err)

			var items []map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &items))
			assert.Len(t, items, tc.wantRows)

			if tc.wantHint == "" {
				assert.Empty(t, stderr)
				return
			}
			assert.Contains(t, stderr, tc.wantHint)
			assert.Contains(t, stderr, "--limit 0")
		})
	}
}

func TestFaroList_RejectsNegativeLimit(t *testing.T) {
	_, _, err := runFaroCommand(t, newMultiAppServer(t, 2), func(l *fakeConfigLoader) *cobra.Command {
		return newListCommand(l)
	}, []string{"--limit", "-1"})
	require.Error(t, err)
}

func TestFaroGet_ErrorsAreNotReportedAsMissingApp(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(forbidden.Close)

	// "App 1" goes straight to the list; "app-1" is slug-shaped and hits the
	// per-ID GET first. Neither may turn a 403 into "no app has that name".
	for _, arg := range []string{"App 1", "app-1"} {
		_, _, err := runFaroCommand(t, forbidden, func(l *fakeConfigLoader) *cobra.Command {
			return newGetCommand(l)
		}, []string{arg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "403")
		assert.NotContains(t, err.Error(), "no app has that")
	}
}

func TestFaroGet_Resolution(t *testing.T) {
	// "Shop-7" and "2024" look like IDs; ID 7 and 2024 do not exist, so they
	// must fall through to the name lookup.
	server := newNamedAppServer(t, []string{"App 1", "Shop-7", "2024"})

	tests := []struct {
		name    string
		arg     string
		wantID  string
		wantErr string
	}{
		{name: "slug-id", arg: "app-1-1", wantID: "1"},
		{name: "numeric id", arg: "3", wantID: "3"},
		{name: "display name", arg: "App 1", wantID: "1"},
		{name: "slug-shaped name", arg: "Shop-7", wantID: "2"},
		{name: "numeric-looking name", arg: "2024", wantID: "3"},
		{name: "unknown", arg: "Nope", wantErr: "no app has that slug-id or name"},
		{name: "unknown slug-shaped", arg: "nope-99", wantErr: "no app has that slug-id or name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := runFaroCommand(t, server, func(l *fakeConfigLoader) *cobra.Command {
				return newGetCommand(l)
			}, []string{tc.arg, "-o", "json"})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, stdout, fmt.Sprintf(`"id": "%s"`, tc.wantID))
		})
	}
}

func TestFaroDelete_HelpDocumentsPermission(t *testing.T) {
	cmd := newDeleteCommand(&fakeConfigLoader{})
	assert.Contains(t, cmd.Long, "grafana-kowalski-app.apps:delete")
	assert.Contains(t, cmd.Long, "Checkout-2024")
}

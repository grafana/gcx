package datasources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeDatasourceCommand(t *testing.T, args []string) (string, error) {
	t.Helper()

	root := helperRoot(datasources.Command())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
	}
	return stdout.String(), err
}

// newDatasourceServer serves the given items from /api/datasources. It mimics a
// Grafana instance where app-platform discovery (/apis) is unavailable, so the
// dual transport falls back to the REST API.
func newDatasourceServer(t *testing.T, items []map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(items); err != nil {
				t.Errorf("encode datasources response: %v", err)
			}
			return
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
}

// newDatasourceListServer serves count generated prometheus datasources.
func newDatasourceListServer(t *testing.T, count int) *httptest.Server {
	t.Helper()

	items := make([]map[string]any, 0, count)
	for i := range count {
		items = append(items, map[string]any{
			"uid":       fmt.Sprintf("ds-%02d", i),
			"name":      fmt.Sprintf("Datasource %02d", i),
			"type":      "prometheus",
			"url":       "https://example.com",
			"access":    "proxy",
			"isDefault": i == 0,
			"readOnly":  false,
		})
	}

	return newDatasourceServer(t, items)
}

func TestListNameFilter(t *testing.T) {
	items := []map[string]any{
		{"uid": "prom-prod-eu", "name": "prometheus-prod-eu", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-prod-us", "name": "prometheus-prod-us", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-dev", "name": "prometheus-dev", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "loki-prod-eu", "name": "loki-prod-eu", "type": "loki", "url": "https://example.com", "access": "proxy"},
	}

	server := newDatasourceServer(t, items)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)

	listUIDs := func(t *testing.T, args ...string) []string {
		t.Helper()
		base := append([]string{"datasources", "list", "--config", configFile, "--limit", "0", "-o", "json"}, args...)
		stdout, err := executeDatasourceCommand(t, base)
		require.NoError(t, err)

		var result struct {
			Datasources []struct {
				UID string `json:"uid"`
			} `json:"datasources"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		uids := make([]string, len(result.Datasources))
		for i, ds := range result.Datasources {
			uids[i] = ds.UID
		}
		return uids
	}

	t.Run("empty name does not filter", func(t *testing.T) {
		assert.Len(t, listUIDs(t), 4)
	})

	t.Run("substring match across types", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"prom-prod-eu", "prom-prod-us", "loki-prod-eu"}, listUIDs(t, "--name", "prod"))
	})

	t.Run("case-insensitive match", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"prom-prod-eu", "loki-prod-eu"}, listUIDs(t, "--name", "PROD-EU"))
	})

	t.Run("composes with type filter (AND)", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"prom-prod-eu", "prom-prod-us"}, listUIDs(t, "--type", "prometheus", "--name", "prod"))
	})

	t.Run("no match returns empty", func(t *testing.T) {
		assert.Empty(t, listUIDs(t, "--name", "does-not-exist"))
	})
}

func TestListNameFilterRespectsLimit(t *testing.T) {
	items := []map[string]any{
		{"uid": "prom-prod-eu", "name": "prometheus-prod-eu", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-prod-us", "name": "prometheus-prod-us", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
		{"uid": "prom-dev", "name": "prometheus-dev", "type": "prometheus", "url": "https://example.com", "access": "proxy"},
	}

	server := newDatasourceServer(t, items)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--name", "prod", "--limit", "1", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	// Two datasources match "prod"; the limit trims the matched set to 1.
	assert.Len(t, result.Datasources, 1)
}

func TestListDefaultReturnsAllDatasources(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--limit", "0", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 60)
}

func TestListExplicitLimitTrimsDatasources(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--limit", "10", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 10)
}

package datasources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeDatasourceCommand(t *testing.T, args []string) (string, error) {
	t.Helper()
	stdout, _, err := executeDatasourceCommandStreams(t, args)
	return stdout, err
}

func executeDatasourceCommandStreams(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	// Pin agent-mode detection off so TTY hint shapes and table-default
	// output are asserted deterministically even when the test itself runs
	// inside an agent harness (e.g. CLAUDECODE=1).
	testutils.SetAgentMode(t, false)

	root := helperRoot(datasources.Command())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()
	if err != nil {
		t.Logf("stderr: %s", stderr.String())
	}
	return stdout.String(), stderr.String(), err
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

func TestListJSONFieldDiscoveryAndSelection(t *testing.T) {
	server := newDatasourceListServer(t, 2)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)

	t.Run("--json list discovers item fields", func(t *testing.T) {
		stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--json", "list"})
		require.NoError(t, err)

		for _, field := range []string{"uid", "name", "type", "url"} {
			assert.Contains(t, stdout, field, "item field %q must be discovered", field)
		}
		assert.NotContains(t, stdout, "datasources", "wrapper key must not be listed")
	})

	t.Run("--json uid,name selects per item", func(t *testing.T) {
		stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--json", "uid,name"})
		require.NoError(t, err)

		var result struct {
			Datasources []map[string]any `json:"datasources"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		require.Len(t, result.Datasources, 2)
		assert.Equal(t, "ds-00", result.Datasources[0]["uid"])
		assert.Equal(t, "Datasource 00", result.Datasources[0]["name"])
		assert.NotContains(t, result.Datasources[0], "type")
	})
}

func TestListExplicitLimitTrimsDatasources(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	// The hint's continuation command derives from the real argv, preserving
	// the user's flags; pin it for determinism.
	testutils.PinArgv(t, "gcx", "datasources", "list", "--limit", "10")

	configFile := newConfigFileForServer(t, server.URL)
	stdout, stderr, err := executeDatasourceCommandStreams(t, []string{"datasources", "list", "--config", configFile, "--limit", "10", "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *struct {
			Truncated bool   `json:"truncated"`
			Returned  int    `json:"returned"`
			Total     *int   `json:"total"`
			Continue  string `json:"continue"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 10)

	// Truncation is machine-legible in the payload: the source is fully
	// fetched, so the total is the observed count.
	require.NotNil(t, result.ListMeta)
	assert.True(t, result.ListMeta.Truncated)
	assert.Equal(t, 10, result.ListMeta.Returned)
	require.NotNil(t, result.ListMeta.Total)
	assert.Equal(t, 60, *result.ListMeta.Total)
	assert.Equal(t, "gcx datasources list --limit 0", result.ListMeta.Continue)

	// ...and human-legible on stderr, in the maintainer-approved sentence
	// shape (not a "<summary>: <command>" splice).
	assert.Contains(t, stderr, "hint: showing first 10 of 60. See all results with: gcx datasources list --limit 0")
}

// TestListDefaultReturnsAllWithoutMeta locks the cheaply-complete-source
// default: no --limit means the full set (default 0), with no truncation
// metadata and no hint.
func TestListDefaultReturnsAllWithoutMeta(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, stderr, err := executeDatasourceCommandStreams(t, []string{"datasources", "list", "--config", configFile, "-o", "json"})
	require.NoError(t, err)

	var result struct {
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *map[string]any  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Len(t, result.Datasources, 60)
	assert.Nil(t, result.ListMeta, "complete set must not carry list_meta")
	assert.NotContains(t, stderr, "showing first")
}

// TestListTruncatedFieldSelection is the end-to-end guard for PR988 defect
// (b): `--limit 1 --json uid` on a truncated result must return the real uid
// per item plus the list_meta signal — never {"uid": null}.
func TestListTruncatedFieldSelection(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--limit", "1", "--json", "uid"})
	require.NoError(t, err)

	var result struct {
		UID         any              `json:"uid"`
		Datasources []map[string]any `json:"datasources"`
		ListMeta    *struct {
			Truncated bool `json:"truncated"`
			Total     *int `json:"total"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Nil(t, result.UID, "must not extract fields from the envelope itself")
	require.Len(t, result.Datasources, 1)
	assert.Equal(t, "ds-00", result.Datasources[0]["uid"])
	require.NotNil(t, result.ListMeta, "truncation signal must survive --json field selection")
	assert.True(t, result.ListMeta.Truncated)
	require.NotNil(t, result.ListMeta.Total)
	assert.Equal(t, 60, *result.ListMeta.Total)
}

// TestListTruncatedFieldDiscovery is the end-to-end guard for PR988 defect
// (c): `--limit 1 --json list` on a truncated result must discover item
// fields, not list_meta.* or the wrapper key.
func TestListTruncatedFieldDiscovery(t *testing.T) {
	server := newDatasourceListServer(t, 60)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	stdout, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--limit", "1", "--json", "list"})
	require.NoError(t, err)

	for _, field := range []string{"uid", "name", "type", "url"} {
		assert.Contains(t, stdout, field, "item field %q must be discovered", field)
	}
	assert.NotContains(t, stdout, "datasources", "wrapper key must not be listed")
	assert.NotContains(t, stdout, "list_meta", "reserved truncation metadata must be excluded from discovery")
}

// TestListNegativeLimitRejected locks the binder validation: --limit must be
// >= 0 (0 means all results are returned).
func TestListNegativeLimitRejected(t *testing.T) {
	server := newDatasourceListServer(t, 2)
	defer server.Close()

	configFile := newConfigFileForServer(t, server.URL)
	_, err := executeDatasourceCommand(t, []string{"datasources", "list", "--config", configFile, "--limit", "-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --limit -1")
}

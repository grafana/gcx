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

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
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

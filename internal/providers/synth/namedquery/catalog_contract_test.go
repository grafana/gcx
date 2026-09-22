package namedquery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/synth/namedquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const (
	schemaPath   = "/public/plugins/synthetic-monitoring-datasource/schema/v0alpha1/query.types.json"
	settingsPath = "/api/plugins/grafana-synthetic-monitoring-app/settings"
)

// fakeGrafanaLoader implements smcfg.GrafanaConfigLoader against a fake
// server. Discovery needs nothing else -- no SM token, no datasource UID --
// which is the point of using LoadGrafanaConfig rather than LoadSMProxyConfig.
type fakeGrafanaLoader struct {
	host string
}

func (l *fakeGrafanaLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.host}, Namespace: "default"}, nil
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return body
}

// catalogServer serves the real query.types.json fixture at the schema path
// and a 404 everywhere else, simulating a stack running SM app >= v1.62.0.
func catalogServer(t *testing.T) *httptest.Server {
	t.Helper()
	fixture := mustReadFixture(t, "query.types.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == schemaPath {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fixture)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runQueries(t *testing.T, srvURL string, agentMode bool, args ...string) (string, string, error) {
	t.Helper()
	prevNoColor := color.NoColor
	color.NoColor = true
	agent.SetFlag(agentMode)
	t.Cleanup(func() {
		agent.SetFlag(false)
		color.NoColor = prevNoColor
	})

	root := namedquery.QueriesCommands(&fakeGrafanaLoader{host: srvURL})
	root.SilenceErrors = true
	root.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func TestQueriesList_TableContract(t *testing.T) {
	srv := catalogServer(t)

	stdout, _, err := runQueries(t, srv.URL, false, "list")
	require.NoError(t, err)
	assert.Contains(t, stdout, "NAME")
	assert.Contains(t, stdout, "REQUIRED")
	assert.Contains(t, stdout, "DESCRIPTION")
	assert.Contains(t, stdout, "probe_execution_rate")
	assert.Contains(t, stdout, "checks_uptime")
	assert.Contains(t, stdout, "job, instance, frequency")
}

func TestQueriesList_JSONContract(t *testing.T) {
	srv := catalogServer(t)

	stdout, _, err := runQueries(t, srv.URL, false, "list", "-o", "json")
	require.NoError(t, err)

	var docs []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &docs))
	require.Len(t, docs, 2)

	name0, ok := docs[0]["name"].(string)
	require.True(t, ok)
	name1, ok := docs[1]["name"].(string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"probe_execution_rate", "checks_uptime"}, []string{name0, name1})
}

func TestQueriesGet_TableContract(t *testing.T) {
	srv := catalogServer(t)

	stdout, _, err := runQueries(t, srv.URL, false, "get", "checks_uptime")
	require.NoError(t, err)
	assert.Contains(t, stdout, "checks_uptime")
	assert.Contains(t, stdout, "job")
	assert.Contains(t, stdout, "instance")
	assert.Contains(t, stdout, "frequency")
	assert.Contains(t, stdout, "gcx synthetic-monitoring query checks_uptime")
}

func TestQueriesGet_JSONContract(t *testing.T) {
	srv := catalogServer(t)

	stdout, _, err := runQueries(t, srv.URL, false, "get", "checks_uptime", "-o", "json")
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, "checks_uptime", doc["name"])
	assert.ElementsMatch(t, []any{"job", "instance", "frequency"}, doc["required"])
	require.Contains(t, doc, "schema")
	require.Contains(t, doc, "example")
}

func TestQueriesGet_UnknownNameErrors(t *testing.T) {
	srv := catalogServer(t)

	_, _, err := runQueries(t, srv.URL, false, "get", "does_not_exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "queries list")
}

func TestQueriesList_404BelowMinimumVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case schemaPath:
			http.NotFound(w, r)
		case settingsPath:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{"version": "1.61.0"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	_, _, err := runQueries(t, srv.URL, false, "list")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1.62.0")
	assert.Contains(t, err.Error(), "1.61.0")
}

func TestQueriesList_404AppNotInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, _, err := runQueries(t, srv.URL, false, "list")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "no synthetic monitoring app installed")
}

func TestQueriesList_UnexpectedAPIVersionWarnsOnStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == schemaPath {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"kind": "QueryTypeDefinitionList",
				"apiVersion": "datasource.grafana.app/v1alpha2",
				"items": [{"metadata":{"name":"foo"},"spec":{"description":"d","schema":{"type":"object"}}}]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	stdout, stderr, err := runQueries(t, srv.URL, false, "list")
	require.NoError(t, err, "an unexpected apiVersion must not fail the fetch")
	assert.Contains(t, stdout, "foo")
	assert.Contains(t, stderr, "apiVersion")
}

func TestQueriesList_AgentModeSingleJSONValue(t *testing.T) {
	srv := catalogServer(t)

	stdout, _, err := runQueries(t, srv.URL, true, "list")
	require.NoError(t, err)

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc any
	require.NoError(t, dec.Decode(&doc))
}

package kg_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runDashboards(t *testing.T, server *httptest.Server, stdin string, args ...string) (string, error) {
	t.Helper()
	pinHumanMode(t)
	t.Setenv("GCX_AUTO_APPROVE", "0")
	cmd := kg.NewDashboardsCommand(thresholdsLoader(server))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

const dashboardsGetBody = `{"entityType":"Service","dashConfigDto":[{"uid":"u1","name":"Kafka","dashboardUri":"/d/u1/kafka","overrideEntityUri":false}]}`

func TestDashboardsReads(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/config/dashboard") {
			_, _ = io.WriteString(w, `{"kpiDashConfigs":[`+dashboardsGetBody+`]}`)
			return
		}
		_, _ = io.WriteString(w, dashboardsGetBody)
	}))
	t.Cleanup(server.Close)

	out, err := runDashboards(t, server, "", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Service")
	assert.Contains(t, out, "Kafka")

	out, err = runDashboards(t, server, "", "list", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, out, `"entityType": "Service"`)
	assert.Contains(t, out, `"uid": "u1"`)

	out, err = runDashboards(t, server, "", "get", "Service")
	require.NoError(t, err)
	assert.Contains(t, out, "dashConfigDto:")
	assert.True(t, strings.HasSuffix(paths[len(paths)-1], "/v1/config/dashboard/Service"), paths)

	_, err = runDashboards(t, server, "", "get")
	require.Error(t, err)
	_, err = runDashboards(t, server, "", "get", " ")
	require.Error(t, err)
}

func TestDashboardsUpsert(t *testing.T) {
	file := `dashboardUri: /grafana/d/u1/kafka
uid: u1
propertyMatchers:
  - name: service_type
    op: EQUALS
    value: kafka
  - {name: team, op: IS_NOT_NULL}
  - {name: env, op: starts with, value: 42}
dashParams: {cluster: cluster_name}
`
	t.Run("normalizes ops and posts one request per config", func(t *testing.T) {
		server, got := recordingServer(t)
		out, err := runDashboards(t, server, "", "upsert", "Service", "-f", writeTempYAML(t, file))
		require.NoError(t, err)
		assert.Contains(t, out, "1 dashboard config(s) upserted")

		require.Len(t, *got, 1)
		req := (*got)[0]
		assert.Equal(t, http.MethodPost, req.Method)
		assert.True(t, strings.HasSuffix(req.Path, "/v1/config/dashboard/Service"), req.Path)
		assert.JSONEq(t, `{"dashboardUri":"/d/u1/kafka","uid":"u1","overrideEntityUri":false,"dashParams":{"cluster":"cluster_name"},
			"propertyMatchers":[
				{"name":"service_type","op":"=","value":"kafka","type":"String"},
				{"name":"team","op":"IS NOT NULL","type":"String"},
				{"name":"env","op":"STARTS WITH","value":"42","type":"String"}]}`, req.Body)
	})

	t.Run("accepts the get wrapper and a list", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runDashboards(t, server, dashboardsGetBody, "upsert", "Service", "-f", "-")
		require.NoError(t, err)
		_, err = runDashboards(t, server, "- dashboardUri: /d/a/a\n- dashboardUri: /d/b/b\n", "upsert", "Service", "-f", "-")
		require.NoError(t, err)
		assert.Len(t, *got, 3)
	})

	for name, tc := range map[string]struct{ input, want string }{
		"bad op":            {"dashboardUri: /d/u/u\npropertyMatchers: [{name: a, op: LIKE}]\n", "unsupported op"},
		"missing uri":       {"uid: u\n", "dashboardUri is required"},
		"missing value":     {"dashboardUri: /d/u/u\npropertyMatchers: [{name: a, op: EQUALS}]\n", "needs a value"},
		"unknown field":     {"dashboardUri: /d/u/u\nbogus: 1\n", "bogus"},
		"wrong entity type": {"entityType: Pod\ndashConfigDto: [{dashboardUri: /d/u/u}]\n", "entity type"},
		"second document":   {"dashboardUri: /d/a/a\n---\ndashboardUri: /d/b/b\n", "multiple YAML documents"},
		"empty":             {"", "no dashboard configs"},
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			server, got := recordingServer(t)
			_, err := runDashboards(t, server, tc.input, "upsert", "Service", "-f", "-")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, *got)
		})
	}

	t.Run("IS NULL needs no value", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runDashboards(t, server, "dashboardUri: /d/u/u\npropertyMatchers: [{name: a, op: IS NULL}]\n", "upsert", "Service", "-f", "-")
		require.NoError(t, err)
		assert.Len(t, *got, 1)
	})

	t.Run("requires the entity type", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runDashboards(t, server, "dashboardUri: /d/u/u\n", "upsert")
		require.Error(t, err)
		assert.Empty(t, *got)
	})

	t.Run("stops at the first failure and reports partial progress", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 2 {
				http.Error(w, "boom", http.StatusInternalServerError)
			}
		}))
		t.Cleanup(server.Close)
		_, err := runDashboards(t, server, "- dashboardUri: /d/a/a\n- dashboardUri: /d/b/b\n- dashboardUri: /d/c/c\n", "upsert", "Service", "-f", "-")
		require.Error(t, err)
		assert.Equal(t, 2, calls)
	})
}

func TestDashboardsUpsertSurfacesFieldErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"Invalid request","subErrors":[{"field":"query","message":"parse error: unexpected end of input"}]}`)
	}))
	t.Cleanup(server.Close)

	_, err := runDashboards(t, server, "dashboardUri: /d/u/u\nquery: \"up{\"\n", "upsert", "Service", "-f", "-")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query: parse error: unexpected end of input")
}

func TestDashboardsDelete(t *testing.T) {
	args := []string{"delete", "Service", "-f"}

	t.Run("posts the whole config to the delete endpoint with --force", func(t *testing.T) {
		server, got := recordingServer(t)
		out, err := runDashboards(t, server, "", append(args, writeTempYAML(t, "dashboardUri: /prefix/d/u1/kafka\nuid: u1\n"), "--force")...)
		require.NoError(t, err)
		assert.Contains(t, out, "1 dashboard config(s) deleted")

		require.Len(t, *got, 1)
		req := (*got)[0]
		assert.Equal(t, http.MethodPost, req.Method)
		assert.True(t, strings.HasSuffix(req.Path, "/v1/config/dashboard/delete/Service"), req.Path)
		assert.JSONEq(t, `{"dashboardUri":"/d/u1/kafka","uid":"u1","overrideEntityUri":false}`, req.Body)
	})

	t.Run("declining the prompt sends nothing", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runDashboards(t, server, "n\n", append(args, writeTempYAML(t, "dashboardUri: /d/u1/u1\n"))...)
		requireCancelled(t, err)
		assert.Empty(t, *got)
	})

	t.Run("accepting the prompt deletes", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runDashboards(t, server, "y\n", append(args, writeTempYAML(t, "dashboardUri: /d/u1/u1\n"))...)
		require.NoError(t, err)
		assert.Len(t, *got, 1)
	})

	t.Run("stdin input needs --force", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runDashboards(t, server, "dashboardUri: /d/u1/u1\n", append(args, "-")...)
		require.Error(t, err)
		assert.Empty(t, *got)
	})
}

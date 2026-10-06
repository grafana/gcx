package kg_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestKgSchemasCommandRegistered(t *testing.T) {
	cmd, args, err := (&kg.KGProvider{}).Commands()[0].Find([]string{"schemas", "list"})
	require.NoError(t, err)
	assert.Empty(t, args)
	assert.Equal(t, "list", cmd.Name())
	assert.Equal(t, agent.StabilityExperimental, cmd.Annotations[agent.AnnotationStability])
}

func TestKgSchemasListFlags(t *testing.T) {
	tests := []struct {
		name       string
		flags      []string
		expand     bool
		latestOnly bool
	}{
		{"defaults", nil, false, true},
		{"expanded", []string{"--expand"}, true, true},
		{"all versions", []string{"--latest-only=false"}, false, false},
		{"expanded all versions", []string{"--expand", "--latest-only=false"}, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/plugins/grafana-asserts-app/resources/apis/kg.grafana.com/v1alpha1/namespaces/stacks-456/schemas", r.URL.Path)
				assert.Equal(t, url.Values{
					"expand":      {strconv.FormatBool(tc.expand)},
					"latest_only": {strconv.FormatBool(tc.latestOnly)},
				}, r.URL.Query())
				_, _ = w.Write([]byte(`{"schemas":[{"domain":{"name":"kg","version":"v1","displayName":"Knowledge Graph"}}]}`))
			}))
			defer server.Close()

			loader := writeLoaderFor(server)
			loader.Cfg.Namespace = "stacks-456"
			args := append([]string{"list", "-o", "json"}, tc.flags...)
			stdout, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewSchemasCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.JSONEq(t, `{"schemas":[{"domain":{"name":"kg","version":"v1","displayName":"Knowledge Graph"}}]}`, stdout)
		})
	}
}

func TestKgSchemasListOutput(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		agent bool
		body  string
	}{
		{"json bundle", []string{"--expand", "-o", "json"}, false, expandedSchemas},
		{"yaml bundle", []string{"--expand", "-o", "yaml"}, false, expandedSchemas},
		{"agent bundle", []string{"--expand"}, true, expandedSchemas},
		{"empty json", []string{"-o", "json"}, false, `{"schemas":[]}`},
		{"empty yaml", []string{"-o", "yaml"}, false, `{"schemas":[]}`},
		{"empty agent", nil, true, `{"schemas":[]}`},
		{"human table", nil, false, expandedSchemas},
		{"empty table", nil, false, `{"schemas":[]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetAgentMode(t, tc.agent)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			stdout, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewSchemasCommand(writeLoaderFor(server)) }, append([]string{"list"}, tc.flags...), "")
			require.NoError(t, err)
			assert.Empty(t, stderr)

			switch tc.name {
			case "yaml bundle", "empty yaml":
				var got, want map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(stdout), &got))
				require.NoError(t, json.Unmarshal([]byte(tc.body), &want))
				assert.Equal(t, want, got)
			case "human table", "empty table":
				assert.Contains(t, stdout, "DOMAIN")
				assert.Contains(t, stdout, "VERSION")
				assert.Contains(t, stdout, "DISPLAY NAME")
				if tc.body == expandedSchemas {
					assert.Contains(t, stdout, "Knowledge Graph")
					assert.Contains(t, stdout, "v1")
				}
			default:
				assert.Equal(t, decodeSingleJSON(t, []byte(tc.body)), decodeSingleJSON(t, []byte(stdout)))
			}
		})
	}
}

func TestKgSchemasListRejectsInvalidInput(t *testing.T) {
	tests := [][]string{
		{"list", "extra"},
		{"list", "--expand="},
		{"list", "--expand=invalid"},
		{"list", "--latest-only="},
		{"list", "--latest-only=invalid"},
		{"list", "-o", "invalid"},
	}
	for _, args := range tests {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("invalid input must not reach the server")
			}))
			defer server.Close()
			stdout, _, err := runKgCommand(t, func() *cobra.Command { return kg.NewSchemasCommand(writeLoaderFor(server)) }, args, "")
			require.Error(t, err)
			assert.Empty(t, stdout)
		})
	}
}

func TestKgSchemasListPropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"namespace does not match the request tenant"}`))
	}))
	defer server.Close()
	stdout, _, err := runKgCommand(t, func() *cobra.Command { return kg.NewSchemasCommand(writeLoaderFor(server)) }, []string{"list", "-o", "json"}, "")
	require.Error(t, err)
	var apiErr *kg.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Empty(t, stdout)
}

package root_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/require"
)

func TestCommandSearchRelevance(t *testing.T) {
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("GCX_CONFIG", t.TempDir()+"/absent.yaml")
	data, err := os.ReadFile("testdata/command_search.json")
	require.NoError(t, err)
	var cases []struct {
		Query    string   `json:"query"`
		Accepted []string `json:"accepted"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Query, func(t *testing.T) {
			cmd := buildRootCmd()
			cmd.Use = "gcx"
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"commands", "search", tc.Query, "-o", "json"})
			require.NoError(t, cmd.Execute())
			var result struct {
				Items []struct {
					Path string `json:"full_path"`
				} `json:"items"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			paths := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				paths = append(paths, item.Path)
			}
			require.True(t, slices.ContainsFunc(paths, func(path string) bool {
				return slices.Contains(tc.Accepted, path)
			}), "expected one of %v in top five; got %v", tc.Accepted, paths)
		})
	}
}

func TestCommandSearchTelemetry(t *testing.T) {
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	cmd := buildRootCmd()
	cmd.Use = "gcx"
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"commands", "search", "private-customer-identifier", "-o", "json"})
	require.NoError(t, cmd.Execute())
	info := root.CurrentTelemetryInfo()
	require.NotNil(t, info)
	require.Equal(t, &root.TelemetryInfo{Command: "commands search", Flags: "output", OutputFormat: "json"}, info)
}

func TestCommandSearchContinuation(t *testing.T) {
	stdout, code := runGcx(t, "commands", "search", "list dashboards", "--limit", "1", "-o", "json")
	require.Zero(t, code)
	var result struct {
		Items []map[string]any `json:"items"`
		Meta  struct {
			Truncated bool   `json:"truncated"`
			Returned  int    `json:"returned"`
			Total     int    `json:"total"`
			Continue  string `json:"continue"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Len(t, result.Items, 1)
	require.Equal(t, "create-dashboard,manage-dashboards", result.Items[0]["skill"])
	require.True(t, result.Meta.Truncated)
	require.Equal(t, 1, result.Meta.Returned)
	require.Greater(t, result.Meta.Total, 1)
	require.Contains(t, result.Meta.Continue, "commands search 'list dashboards'")
	require.Contains(t, result.Meta.Continue, "-o json")
	require.Contains(t, result.Meta.Continue, "--limit 0")
}

func BenchmarkCommandSearch(b *testing.B) {
	b.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	cmd := buildRootCmd()
	cmd.Use = "gcx"
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"commands", "--flat", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		b.Fatal(err)
	}
	var catalog struct {
		Commands []struct {
			Path        string `json:"full_path"`
			Description string `json:"description"`
			Long        string `json:"long"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		b.Fatal(err)
	}
	documents := make([]agent.SearchDocument, 0, len(catalog.Commands))
	for _, c := range catalog.Commands {
		documents = append(documents, agent.SearchDocument{Path: c.Path, Summary: c.Description, Description: c.Long})
	}
	b.ReportAllocs()
	for b.Loop() {
		agent.SearchCommands(documents, "create an uptime check")
	}
}

package docs_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/docs"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disableAgentMode ensures tests run with agent mode off so default output
// format is "text" rather than "agents".
func disableAgentMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("CURSOR_AGENT", "")
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_CODE", "")
	agent.ResetForTesting()
}

func loadTestIndex(t *testing.T) *grafanadocs.Index {
	t.Helper()
	f, err := os.Open("testdata/sample-index.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	idx, err := grafanadocs.LoadIndexFromReader(f)
	require.NoError(t, err)
	return idx
}

// run builds the docs command group with a pre-loaded index and executes it
// with the given args, capturing stdout and stderr separately.
func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	idx := loadTestIndex(t)
	cmd := docs.CommandWithIndex(idx)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestSearchCommand(t *testing.T) {
	disableAgentMode(t)

	tests := []struct {
		name        string
		args        []string
		wantErr     string
		wantStdout  []string
		wantStderr  string
		checkStdout func(t *testing.T, stdout string)
	}{
		{
			name:       "text output has header and a hit",
			args:       []string{"search", "clustering"},
			wantStdout: []string{"TITLE", "PRODUCT", "URL", "Clustering"},
		},
		{
			name:       "product filter is case-insensitive substring",
			args:       []string{"search", "clustering", "--product", "agent"},
			wantStdout: []string{"Clustering"},
		},
		{
			name:    "empty query is rejected",
			args:    []string{"search", ""},
			wantErr: "query is required",
		},
		{
			name:       "no matches still emits a clean table and guidance",
			args:       []string{"search", "zzzznotathing"},
			wantStdout: []string{"TITLE"},
			wantStderr: "no results found",
		},
		{
			name:       "over-fetch proving more hits emits a completeness hint",
			args:       []string{"search", "grafana", "--limit", "1"},
			wantStdout: []string{"TITLE"},
			wantStderr: "showing first 1",
		},
		{
			name: "complete page has no list_meta",
			args: []string{"search", "clustering", "-o", "json"},
			checkStdout: func(t *testing.T, stdout string) {
				t.Helper()
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &got))
				results, ok := got["results"].([]any)
				require.True(t, ok)
				require.NotEmpty(t, results)
				first, ok := results[0].(map[string]any)
				require.True(t, ok)
				assert.Contains(t, first["title"], "Clustering")
				_, hasMeta := got["list_meta"]
				assert.False(t, hasMeta, "complete page must omit list_meta")
			},
		},
		{
			name: "truncated page carries list_meta",
			args: []string{"search", "grafana", "--limit", "1", "-o", "json"},
			checkStdout: func(t *testing.T, stdout string) {
				t.Helper()
				var got struct {
					Results  []map[string]any `json:"results"`
					ListMeta *struct {
						Truncated bool `json:"truncated"`
						Returned  int  `json:"returned"`
					} `json:"list_meta"`
				}
				require.NoError(t, json.Unmarshal([]byte(stdout), &got))
				assert.Len(t, got.Results, 1)
				require.NotNil(t, got.ListMeta, "truncated page must carry list_meta")
				assert.True(t, got.ListMeta.Truncated)
				assert.Equal(t, 1, got.ListMeta.Returned)
			},
		},
		{
			name: "empty results serialize as an array, not null",
			args: []string{"search", "zzzznotathing", "-o", "json"},
			checkStdout: func(t *testing.T, stdout string) {
				t.Helper()
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(stdout), &got))
				results, ok := got["results"].([]any)
				require.True(t, ok)
				assert.Empty(t, results)
				_, hasMeta := got["list_meta"]
				assert.False(t, hasMeta)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := run(t, tt.args...)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			for _, want := range tt.wantStdout {
				assert.Contains(t, stdout, want)
			}
			if tt.wantStderr != "" {
				assert.Contains(t, stderr, tt.wantStderr)
			}
			if tt.checkStdout != nil {
				tt.checkStdout(t, stdout)
			}
		})
	}
}

func TestProductsCommand(t *testing.T) {
	disableAgentMode(t)

	t.Run("text lists products and counts", func(t *testing.T) {
		stdout, _, err := run(t, "list-products")
		require.NoError(t, err)
		assert.Contains(t, stdout, "PRODUCT")
		assert.Contains(t, stdout, "COUNT")
		assert.Contains(t, stdout, "Grafana Agent")
		assert.NotContains(t, stdout, "Documentation home")
		assert.NotContains(t, stdout, "Copyright notice")
	})

	t.Run("json wraps products", func(t *testing.T) {
		stdout, _, err := run(t, "list-products", "-o", "json")
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &got))
		products, ok := got["products"].([]any)
		require.True(t, ok)
		assert.NotEmpty(t, products)
	})
}

func TestLinksCommand(t *testing.T) {
	disableAgentMode(t)

	t.Run("text lists names and urls", func(t *testing.T) {
		stdout, _, err := run(t, "list-links")
		require.NoError(t, err)
		assert.Contains(t, stdout, "NAME")
		assert.Contains(t, stdout, "URL")
		assert.Contains(t, stdout, "ServiceAccounts")
		assert.Contains(t, stdout, "https://grafana.com/docs/")
	})

	t.Run("json wraps links", func(t *testing.T) {
		stdout, _, err := run(t, "list-links", "-o", "json")
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &got))
		links, ok := got["links"].([]any)
		require.True(t, ok)
		assert.NotEmpty(t, links)
		first, ok := links[0].(map[string]any)
		require.True(t, ok)
		assert.NotEmpty(t, first["name"])
		assert.NotEmpty(t, first["url"])
	})
}

func TestGetCommandGuards(t *testing.T) {
	disableAgentMode(t)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing url arg", args: []string{"get"}, wantErr: "accepts 1 arg"},
		{name: "non-grafana host", args: []string{"get", "https://evil.com/docs/x.md"}, wantErr: "rejected host"},
		{name: "outline missing url", args: []string{"outline"}, wantErr: "accepts 1 arg"},
		{name: "outline non-grafana host", args: []string{"outline", "https://evil.com/docs/x"}, wantErr: "rejected host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := run(t, tt.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

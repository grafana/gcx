package docs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/docs"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleDoc is a small markdown fixture with a predictable line/heading layout:
//
//	line 1: # Doc Title
//	line 2: (blank)
//	line 3: ## Alpha
//	line 4: (blank)
//	line 5: First section text.
//	line 6: (blank)
//	line 7: ## Beta
//	line 8: (blank)
//	line 9: Second section text.
const sampleDoc = "# Doc Title\n\n## Alpha\n\nFirst section text.\n\n## Beta\n\nSecond section text.\n"

// okDoc returns a fetcher that always serves sampleDoc for the requested URL,
// so the get/outline success paths run without any network access.
func okDoc() func(context.Context, string) (*grafanadocs.Doc, error) {
	return func(_ context.Context, u string) (*grafanadocs.Doc, error) {
		return &grafanadocs.Doc{URL: u, Content: []byte(sampleDoc)}, nil
	}
}

// runWithFetcher builds the docs command group with the page fetcher replaced,
// executes it with agent mode disabled, and captures stdout.
func runWithFetcher(t *testing.T, fetch func(context.Context, string) (*grafanadocs.Doc, error), args ...string) (string, error) {
	t.Helper()
	disableAgentMode(t)
	cmd := docs.CommandWithFetcher(fetch)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestGetCommandSuccess(t *testing.T) {
	const url = "https://grafana.com/docs/tempo/latest/"

	t.Run("offset/limit paging returns a bounded range", func(t *testing.T) {
		stdout, err := runWithFetcher(t, okDoc(), "get", url, "--offset", "0", "--limit", "3", "-o", "json")
		require.NoError(t, err)

		var res struct {
			Content       string `json:"content"`
			URL           string `json:"url"`
			TotalLines    int    `json:"total_lines"`
			ReturnedRange [2]int `json:"returned_range"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &res))
		assert.Equal(t, url, res.URL)
		assert.Equal(t, [2]int{1, 3}, res.ReturnedRange)
		assert.Equal(t, 10, res.TotalLines)
		assert.Contains(t, res.Content, "# Doc Title")
		assert.Contains(t, res.Content, "## Alpha")
		assert.NotContains(t, res.Content, "## Beta")
	})

	t.Run("section extraction returns only that section", func(t *testing.T) {
		stdout, err := runWithFetcher(t, okDoc(), "get", url, "--section", "Alpha", "-o", "json")
		require.NoError(t, err)

		var res struct {
			Content       string `json:"content"`
			ReturnedRange [2]int `json:"returned_range"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &res))
		assert.Equal(t, [2]int{3, 6}, res.ReturnedRange)
		assert.Contains(t, res.Content, "## Alpha")
		assert.Contains(t, res.Content, "First section text.")
		assert.NotContains(t, res.Content, "## Beta")
	})

	t.Run("missing section is a helpful error", func(t *testing.T) {
		_, err := runWithFetcher(t, okDoc(), "get", url, "--section", "Nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `section "Nonexistent" not found`)
		assert.Contains(t, err.Error(), "gcx docs outline")
	})
}

func TestOutlineCommandSuccess(t *testing.T) {
	const url = "https://grafana.com/docs/tempo/latest/"

	stdout, err := runWithFetcher(t, okDoc(), "outline", url, "-o", "json")
	require.NoError(t, err)

	var res struct {
		URL      string `json:"url"`
		Headings []struct {
			Level int    `json:"level"`
			Text  string `json:"text"`
			Line  int    `json:"line"`
		} `json:"headings"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &res))
	assert.Equal(t, url, res.URL)
	require.Len(t, res.Headings, 3)
	assert.Equal(t, "Doc Title", res.Headings[0].Text)
	assert.Equal(t, 1, res.Headings[0].Level)
	assert.Equal(t, "Alpha", res.Headings[1].Text)
	assert.Equal(t, "Beta", res.Headings[2].Text)
}

// TestFetchErrorIsCleaned asserts that a grafanadocs error surfaced by get is
// rewritten into product-facing language: the URL is added for context and the
// internal "grafanadocs:" package prefix is stripped. This exercises the
// cleanFetchErr helper end-to-end through the command.
func TestFetchErrorIsCleaned(t *testing.T) {
	const url = "https://evil.com/docs/x"
	fetch := func(_ context.Context, _ string) (*grafanadocs.Doc, error) {
		return nil, fmt.Errorf("grafanadocs: rejected host %q (only grafana.com allowed)", "evil.com")
	}

	_, err := runWithFetcher(t, fetch, "get", url)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching "+url)
	assert.Contains(t, err.Error(), "rejected host")
	assert.NotContains(t, err.Error(), "grafanadocs:")
}

package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleDoc is a small markdown fixture with a predictable heading layout
// used by both get and outline tests.
const sampleDoc = `# Doc Title

## Alpha

First section text.

## Beta

Second section text.
`

// okDoc returns a fetcher that always serves sampleDoc for the requested URL,
// so the get/outline success paths run without any network access.
func okDoc() docFetcher {
	return func(_ context.Context, u string) (*grafanadocs.Doc, error) {
		return &grafanadocs.Doc{URL: u, Content: []byte(sampleDoc)}, nil
	}
}

func TestGetCommandSuccess(t *testing.T) {
	disableAgentMode(t)
	const url = "https://grafana.com/docs/tempo/latest/"

	t.Run("offset/limit paging returns a bounded range", func(t *testing.T) {
		stdout, _, err := testCommand(t, nil, okDoc(), "get", url, "--offset", "0", "--limit", "3", "-o", "json")
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
		stdout, _, err := testCommand(t, nil, okDoc(), "get", url, "--section", "Alpha", "-o", "json")
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
		_, _, err := testCommand(t, nil, okDoc(), "get", url, "--section", "Nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `section "Nonexistent" not found`)
		assert.Contains(t, err.Error(), "gcx docs outline")
	})

	t.Run("negative limit uses the default cap", func(t *testing.T) {
		stdoutNeg, _, err := testCommand(t, nil, okDoc(), "get", url, "--limit", "-1", "-o", "json")
		require.NoError(t, err)
		stdoutZero, _, err := testCommand(t, nil, okDoc(), "get", url, "--limit", "0", "-o", "json")
		require.NoError(t, err)

		var neg, zero struct {
			Content       string `json:"content"`
			TotalLines    int    `json:"total_lines"`
			ReturnedRange [2]int `json:"returned_range"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdoutNeg), &neg))
		require.NoError(t, json.Unmarshal([]byte(stdoutZero), &zero))
		assert.Equal(t, zero.ReturnedRange, neg.ReturnedRange)
		assert.Equal(t, zero.TotalLines, neg.TotalLines)
		assert.Equal(t, zero.Content, neg.Content)
		assert.Equal(t, [2]int{1, 10}, neg.ReturnedRange)
	})
}

func TestGetPartialityHint(t *testing.T) {
	const url = "https://grafana.com/docs/tempo/latest/"

	t.Run("partial page emits a hint", func(t *testing.T) {
		disableAgentMode(t)
		_, stderr, err := testCommand(t, nil, okDoc(), "get", url, "--offset", "0", "--limit", "3")
		require.NoError(t, err)
		assert.Contains(t, stderr, "showing lines 1-3 of 10")
		assert.Contains(t, stderr, "--offset 3 --limit 3")
		assert.Contains(t, stderr, shellQuote(url))
	})

	t.Run("full page emits no hint", func(t *testing.T) {
		disableAgentMode(t)
		_, stderr, err := testCommand(t, nil, okDoc(), "get", url, "--limit", "100")
		require.NoError(t, err)
		assert.Empty(t, stderr)
	})

	t.Run("section extraction emits no hint", func(t *testing.T) {
		disableAgentMode(t)
		_, stderr, err := testCommand(t, nil, okDoc(), "get", url, "--section", "Alpha")
		require.NoError(t, err)
		assert.Empty(t, stderr)
	})

	t.Run("agent mode emits a JSONL hint", func(t *testing.T) {
		enableAgentMode(t)
		_, stderr, err := testCommand(t, nil, okDoc(), "get", url, "--offset", "0", "--limit", "3", "-o", "text")
		require.NoError(t, err)
		assert.Contains(t, stderr, `"class":"hint"`)
		assert.Contains(t, stderr, "showing lines 1-3 of 10")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(stderr), "{"), "agent-mode hint must be JSONL")
	})
}

// TestFetchErrorIsCleaned asserts that a grafanadocs error surfaced by get is
// rewritten into product-facing language: the URL is added for context and the
// internal "grafanadocs:" package prefix is stripped. This exercises the
// cleanFetchErr helper end-to-end through the command.
func TestFetchErrorIsCleaned(t *testing.T) {
	disableAgentMode(t)
	const url = "https://evil.com/docs/x"
	fetch := func(_ context.Context, _ string) (*grafanadocs.Doc, error) {
		return nil, fmt.Errorf("grafanadocs: rejected host %q (only grafana.com allowed)", "evil.com")
	}

	_, _, err := testCommand(t, nil, fetch, "get", url)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching "+url)
	assert.Contains(t, err.Error(), "rejected host")
	assert.NotContains(t, err.Error(), "grafanadocs:")
}

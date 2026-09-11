package docs_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestOutlineShorthandResolution(t *testing.T) {
	idx := loadTestIndex(t)

	t.Run("shorthand resolves and shows outline", func(t *testing.T) {
		stdout, err := runWithIndexAndFetcher(t, idx, okDoc(), "outline", "clustering", "-o", "json")
		require.NoError(t, err)

		var res struct {
			URL      string `json:"url"`
			Headings []struct {
				Text string `json:"text"`
			} `json:"headings"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &res))
		assert.Contains(t, res.URL, "clustering")
		require.NotEmpty(t, res.Headings)
	})

	t.Run("no match gives guidance", func(t *testing.T) {
		_, err := runWithIndexAndFetcher(t, idx, okDoc(), "outline", "zzzznotathing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no matching page found")
	})
}

func TestOutlineChildPages(t *testing.T) {
	idx := loadTestIndex(t)

	t.Run("directory page includes child pages", func(t *testing.T) {
		// The sample index has tempo/latest.md as parent with configuration.md as child.
		stdout, err := runWithIndexAndFetcher(t, idx, okDoc(), "outline", "https://grafana.com/docs/tempo/latest.md", "-o", "json")
		require.NoError(t, err)

		var res struct {
			URL        string `json:"url"`
			ChildPages []struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"child_pages"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &res))
		require.NotEmpty(t, res.ChildPages, "directory page should have child pages")
		assert.Equal(t, "Configuration", res.ChildPages[0].Title)
		assert.Contains(t, res.ChildPages[0].URL, "configuration")
	})

	t.Run("leaf page has no child pages", func(t *testing.T) {
		stdout, err := runWithIndexAndFetcher(t, idx, okDoc(), "outline", "https://grafana.com/docs/tempo/latest/configuration.md", "-o", "json")
		require.NoError(t, err)

		var res struct {
			ChildPages []struct{} `json:"child_pages"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &res))
		assert.Empty(t, res.ChildPages, "leaf page should have no child pages")
	})
}

package docs //nolint:testpackage // white-box tests call newDocsCommand without exported test constructors.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutlineCommandSuccess(t *testing.T) {
	disableAgentMode(t)
	const url = "https://grafana.com/docs/tempo/latest/"

	stdout, _, err := testCommand(t, nil, okDoc(), "outline", url, "-o", "json")
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
	disableAgentMode(t)
	idx := loadTestIndex(t)

	t.Run("shorthand resolves and shows outline", func(t *testing.T) {
		stdout, _, err := testCommand(t, idx, okDoc(), "outline", "clustering", "-o", "json")
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

	t.Run("shorthand with product scopes resolution", func(t *testing.T) {
		stdout, _, err := testCommand(t, idx, okDoc(), "outline", "configuration", "--product", "tempo", "-o", "json")
		require.NoError(t, err)

		var res struct {
			URL string `json:"url"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &res))
		assert.Contains(t, res.URL, "configuration")
		assert.Contains(t, res.URL, "tempo")
	})

	t.Run("product filter excludes non-matching products", func(t *testing.T) {
		_, _, err := testCommand(t, idx, okDoc(), "outline", "configuration", "--product", "agent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no matching page found")
	})

	t.Run("no match gives guidance", func(t *testing.T) {
		_, _, err := testCommand(t, idx, okDoc(), "outline", "zzzznotathing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no matching page found")
	})
}

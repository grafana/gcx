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

package testutils

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// ListPage is the decoded form of the list envelope that list commands write
// in structured formats: {"items": [...], "list_meta": {...}}.
type ListPage struct {
	Items    []map[string]any `json:"items"`
	ListMeta *ListPageMeta    `json:"list_meta"`
}

// ListPageMeta is the decoded list_meta object of a truncated page.
type ListPageMeta struct {
	Truncated bool   `json:"truncated"`
	Returned  int    `json:"returned"`
	Total     *int   `json:"total"`
	Continue  string `json:"continue"`
}

// DecodeListPage decodes stdout as one list envelope. The test fails when
// stdout is not a JSON object, when it has keys other than items and
// list_meta, or when items is missing or null.
func DecodeListPage(t *testing.T, stdout string) ListPage {
	t.Helper()

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stdout), &raw), "stdout must be a JSON object envelope, got: %s", stdout)
	for key := range raw {
		require.Contains(t, []string{"items", "list_meta"}, key, "unexpected envelope key %q", key)
	}
	require.Contains(t, raw, "items", "envelope must always have items")
	require.NotEqual(t, "null", string(raw["items"]), "items must be an array, not null")

	var page ListPage
	require.NoError(t, json.Unmarshal([]byte(stdout), &page))
	return page
}

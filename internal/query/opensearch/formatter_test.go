package opensearch_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatIndices(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, opensearch.FormatIndices(&buf, []opensearch.IndexInfo{{Name: "grafana-logs", Fields: 11}}))
	out := buf.String()
	assert.Contains(t, out, "grafana-logs")
	assert.Contains(t, out, "11")

	buf.Reset()
	require.NoError(t, opensearch.FormatIndices(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatFields(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, opensearch.FormatFields(&buf, []opensearch.FieldInfo{{Index: "grafana-logs", Name: "tags.app", Type: "keyword"}}))
	out := buf.String()
	assert.Contains(t, out, "tags.app")
	assert.Contains(t, out, "keyword")

	buf.Reset()
	require.NoError(t, opensearch.FormatFields(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

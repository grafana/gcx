package elasticsearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// list-indices used to always fetch the mapping for every index with no way
// to narrow, unlike list-fields. This locks that --index exists here too,
// with the same shape (no shorthand) as list-fields' own --index flag.
func TestListIndicesCmd_HasIndexFlag(t *testing.T) {
	cmd := elasticsearch.ListIndicesCmd(nil)

	f := cmd.Flags().Lookup("index")
	require.NotNil(t, f, "list-indices must expose --index")
	assert.Empty(t, f.Shorthand)
	assert.Empty(t, f.DefValue, "default is unrestricted (all indices)")
}

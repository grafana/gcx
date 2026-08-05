package elasticsearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAgg(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		require.NoError(t, elasticsearch.ValidateAgg("count", ""))
		for _, agg := range []string{"avg", "sum", "min", "max", "cardinality"} {
			require.NoError(t, elasticsearch.ValidateAgg(agg, "duration_ms"), agg)
		}
	})

	t.Run("rejects unknown agg", func(t *testing.T) {
		err := elasticsearch.ValidateAgg("percentiles", "f")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "supported: avg, cardinality, count, max, min, sum")
	})

	t.Run("rejects missing field", func(t *testing.T) {
		err := elasticsearch.ValidateAgg("avg", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--field is required")
	})
}

func TestParseMapping(t *testing.T) {
	t.Run("multiple indices sorted with nested fields flattened", func(t *testing.T) {
		body := []byte(`{
			"zz-index": {"mappings": {"properties": {"msg": {"type": "text"}}}},
			"aa-index": {"mappings": {"properties": {
				"@timestamp": {"type": "date"},
				"nested": {"properties": {"deep": {"properties": {"leaf": {"type": "keyword"}}}}}
			}}}
		}`)

		indices, fields, err := elasticsearch.ParseMapping(body)
		require.NoError(t, err)
		require.Len(t, indices, 2)
		assert.Equal(t, "aa-index", indices[0].Name)
		assert.Equal(t, 2, indices[0].Fields)

		require.Len(t, fields, 3)
		assert.Equal(t, "nested.deep.leaf", fields[1].Name)
		assert.Equal(t, "keyword", fields[1].Type)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, _, err := elasticsearch.ParseMapping([]byte(`{bad`))
		require.Error(t, err)
	})
}

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

	t.Run("multi-fields surface as dotted sibling fields", func(t *testing.T) {
		body := []byte(`{
			"logs": {"mappings": {"properties": {
				"message": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"host": {"properties": {
					"name": {"type": "text", "fields": {"keyword": {"type": "keyword"}}}
				}}
			}}}
		}`)

		indices, fields, err := elasticsearch.ParseMapping(body)
		require.NoError(t, err)
		require.Len(t, indices, 1)
		assert.Equal(t, 4, indices[0].Fields, "message, message.keyword, host.name, host.name.keyword")

		names := make(map[string]string, len(fields))
		for _, f := range fields {
			names[f.Name] = f.Type
		}
		assert.Equal(t, "text", names["message"])
		assert.Equal(t, "keyword", names["message.keyword"])
		assert.Equal(t, "text", names["host.name"])
		assert.Equal(t, "keyword", names["host.name.keyword"])
	})
}

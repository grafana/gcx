package opensearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAgg(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		require.NoError(t, opensearch.ValidateAgg("count", ""))
		for _, agg := range []string{"avg", "sum", "min", "max", "cardinality"} {
			require.NoError(t, opensearch.ValidateAgg(agg, "duration_ms"), agg)
		}
	})

	t.Run("rejects unknown agg", func(t *testing.T) {
		err := opensearch.ValidateAgg("percentiles", "f")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "supported: avg, cardinality, count, max, min, sum")
	})

	t.Run("rejects missing field", func(t *testing.T) {
		err := opensearch.ValidateAgg("avg", "")
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

		indices, fields, err := opensearch.ParseMapping(body)
		require.NoError(t, err)
		require.Len(t, indices, 2)
		assert.Equal(t, "aa-index", indices[0].Name)
		assert.Equal(t, 2, indices[0].Fields)

		require.Len(t, fields, 3)
		assert.Equal(t, "nested.deep.leaf", fields[1].Name)
		assert.Equal(t, "keyword", fields[1].Type)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, _, err := opensearch.ParseMapping([]byte(`{bad`))
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

		indices, fields, err := opensearch.ParseMapping(body)
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

	// Regression fixture built from the live _mapping response of the
	// grafana-logs index on the dev stack's opensearch-ds-m datasource, so a
	// change to the flattening logic gets caught against a real shape, not
	// just synthetic JSON.
	t.Run("live grafana-logs mapping shape", func(t *testing.T) {
		body := []byte(`{
			"grafana-logs": {"mappings": {"properties": {
				"@timestamp": {"type": "date"},
				"app": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"datacenter": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"duration_ms": {"type": "long"},
				"host": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"level": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"logger": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"message": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
				"request_id": {"type": "keyword"},
				"type": {"type": "keyword"},
				"user_id": {"type": "keyword"}
			}}}
		}`)

		indices, fields, err := opensearch.ParseMapping(body)
		require.NoError(t, err)
		require.Len(t, indices, 1)
		assert.Equal(t, "grafana-logs", indices[0].Name)

		names := make(map[string]string, len(fields))
		for _, f := range fields {
			names[f.Name] = f.Type
		}
		assert.Equal(t, "date", names["@timestamp"])
		assert.Equal(t, "text", names["app"])
		assert.Equal(t, "keyword", names["app.keyword"])
		assert.Equal(t, "long", names["duration_ms"])
		assert.Equal(t, "keyword", names["request_id"])
	})
}

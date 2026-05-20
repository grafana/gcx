package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/itchyny/gojq"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// JQCodec applies a jq expression to a value and writes each yielded result
// as pretty-printed JSON on its own line (NDJSON shape, matching real jq).
//
// JQCodec intentionally bypasses the agents codec's spill-to-tempfile behavior:
// a caller using --jq wants the transformed results in-stream, not a "spilled
// to /tmp" summary.
type JQCodec struct {
	query *gojq.Query
}

// NewJQCodec returns a JQCodec that runs the given compiled query.
// Callers should obtain the query via gojq.Parse so syntax errors surface
// during flag validation, not encoding.
func NewJQCodec(query *gojq.Query) *JQCodec {
	return &JQCodec{query: query}
}

func (c *JQCodec) Format() format.Format {
	return format.JSON
}

func (c *JQCodec) Encode(dst io.Writer, value any) error {
	input, err := toJQInput(value)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(dst)
	encoder.SetIndent("", "  ")

	iter := c.query.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			return nil
		}
		if e, ok := v.(error); ok {
			return fmt.Errorf("jq runtime: %w", e)
		}
		if err := encoder.Encode(v); err != nil {
			return err
		}
	}
}

func (c *JQCodec) Decode(src io.Reader, value any) error {
	return format.NewJSONCodec().Decode(src, value)
}

// toJQInput converts an arbitrary Go value into the generic JSON primitives
// gojq expects (map[string]any, []any, string, float64, bool, nil).
//
// Unstructured types are handled directly to avoid pointer-receiver
// MarshalJSON quirks (mirrors marshalToSampleMap in format.go). For all other
// types we round-trip through encoding/json.
func toJQInput(value any) (any, error) {
	switch v := value.(type) {
	case unstructured.Unstructured:
		return v.Object, nil
	case *unstructured.Unstructured:
		if v != nil {
			return v.Object, nil
		}
	case unstructured.UnstructuredList:
		return unstructuredItemsAsMap(v.Items), nil
	case *unstructured.UnstructuredList:
		if v != nil {
			return unstructuredItemsAsMap(v.Items), nil
		}
	}

	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("jq: marshal input: %w", err)
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("jq: parse input: %w", err)
	}
	return parsed, nil
}

func unstructuredItemsAsMap(items []unstructured.Unstructured) map[string]any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item.Object
	}
	return map[string]any{"items": out}
}

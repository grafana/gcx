package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/itchyny/gojq"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	jqShapeMaxFields     = 20 // cap on field paths carried in JQRuntimeError
	jqShapeMaxFieldDepth = 3  // max dot-separated segments per carried path
)

// JQRuntimeError is returned by JQCodec.Encode when the compiled --jq
// expression fails against the actual output value. It carries a compact
// description of the input shape so the expression can be corrected in one
// retry instead of blind trial and error.
type JQRuntimeError struct {
	Err        error    // underlying gojq evaluation error
	Shape      string   // e.g. "an array of 25 objects"
	Fields     []string // capped, sorted dot-notation paths of a sample object/element
	MoreFields int      // discovered paths beyond the cap
	ArrayInput bool     // input is an array or an object with an items[] array
}

func (e JQRuntimeError) Error() string {
	return fmt.Sprintf("jq runtime: %v", e.Err)
}

func (e JQRuntimeError) Unwrap() error {
	return e.Err
}

// newJQRuntimeError builds a JQRuntimeError from the already-normalized jq
// input (the output of toJQInput): the top-level shape plus field paths from
// a representative object, filtered to shallow paths and capped so the error
// stays compact regardless of input size.
func newJQRuntimeError(evalErr error, input any) JQRuntimeError {
	jqErr := JQRuntimeError{Err: evalErr}

	var sample map[string]any
	switch v := input.(type) {
	case nil:
		jqErr.Shape = "null"
	case string:
		jqErr.Shape = "a string"
	case bool:
		jqErr.Shape = "a boolean"
	case []any:
		jqErr.ArrayInput = true
		if first, ok := firstObject(v); ok {
			jqErr.Shape = fmt.Sprintf("an array of %d objects", len(v))
			sample = first
		} else {
			jqErr.Shape = fmt.Sprintf("an array of %d elements", len(v))
		}
	case map[string]any:
		items, _ := v["items"].([]any)
		if first, ok := firstObject(items); ok {
			jqErr.ArrayInput = true
			jqErr.Shape = fmt.Sprintf(`an object with an "items" array of %d objects`, len(items))
			sample = first
		} else {
			jqErr.Shape = "an object"
			sample = v
		}
	default:
		// toJQInput normalizes everything else to JSON numbers.
		jqErr.Shape = "a number"
	}

	if sample != nil {
		jqErr.Fields, jqErr.MoreFields = shallowFieldPaths(sample)
	}
	return jqErr
}

// firstObject returns the first element of vals as an object map, when the
// slice is non-empty and its first element is one.
func firstObject(vals []any) (map[string]any, bool) {
	if len(vals) == 0 {
		return nil, false
	}
	m, ok := vals[0].(map[string]any)
	return m, ok
}

// shallowFieldPaths discovers dot-notation field paths on sample, keeps only
// paths shallower than jqShapeMaxFieldDepth segments (deep k8s metadata noise
// would drown the useful ones), and caps the result at jqShapeMaxFields.
func shallowFieldPaths(sample map[string]any) ([]string, int) {
	var shallow []string
	for _, p := range DiscoverFields(sample) {
		if strings.Count(p, ".") < jqShapeMaxFieldDepth {
			shallow = append(shallow, p)
		}
	}
	if len(shallow) > jqShapeMaxFields {
		return shallow[:jqShapeMaxFields], len(shallow) - jqShapeMaxFields
	}
	return shallow, 0
}

// JQCodec applies a jq expression to a value and writes each yielded result
// as pretty-printed JSON on its own line (NDJSON shape, matching real jq).
//
// JQCodec intentionally bypasses the agents codec's spill-to-tempfile behavior:
// a caller using --jq wants the transformed results in-stream, not a "spilled
// to /tmp" summary.
type JQCodec struct {
	query   *gojq.Query
	decoder *format.JSONCodec
}

// NewJQCodec returns a JQCodec that runs the given compiled query.
// Callers should obtain the query via gojq.Parse so syntax errors surface
// during flag validation, not encoding.
func NewJQCodec(query *gojq.Query) *JQCodec {
	return &JQCodec{query: query, decoder: format.NewJSONCodec()}
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
			return newJQRuntimeError(e, input)
		}
		if err := encoder.Encode(v); err != nil {
			return err
		}
	}
}

func (c *JQCodec) Decode(src io.Reader, value any) error {
	return c.decoder.Decode(src, value)
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

package output

import (
	"encoding/json"
	goio "io"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FieldSelectCodec wraps the JSON codec and emits only the requested fields
// from each output object. It implements format.Codec.
//
// Field paths support dot-notation (e.g. "metadata.name") which is resolved
// by walking nested maps.
//
// For a single object the output is a flat JSON object containing only the
// selected fields. For a collection (printItems or
// unstructured.UnstructuredList) the output is {"items": [...]}.
//
// Missing fields produce a null value rather than being omitted (FR-008).
type FieldSelectCodec struct {
	fields []string
	json   *format.JSONCodec
}

// NewFieldSelectCodec creates a FieldSelectCodec for the given field paths.
func NewFieldSelectCodec(fields []string) *FieldSelectCodec {
	return &FieldSelectCodec{
		fields: fields,
		json:   format.NewJSONCodec(),
	}
}

func (c *FieldSelectCodec) Format() format.Format {
	return format.JSON
}

// Encode writes the selected fields to dst as JSON.
func (c *FieldSelectCodec) Encode(dst goio.Writer, value any) error {
	switch v := value.(type) {
	case unstructured.UnstructuredList:
		items := make([]map[string]any, len(v.Items))
		for i, item := range v.Items {
			items[i] = extractFields(item.Object, c.fields)
		}
		return c.json.Encode(dst, map[string]any{"items": items})

	case *unstructured.UnstructuredList:
		items := make([]map[string]any, len(v.Items))
		for i, item := range v.Items {
			items[i] = extractFields(item.Object, c.fields)
		}
		return c.json.Encode(dst, map[string]any{"items": items})

	case unstructured.Unstructured:
		return c.json.Encode(dst, extractFields(v.Object, c.fields))

	case *unstructured.Unstructured:
		return c.json.Encode(dst, extractFields(v.Object, c.fields))

	case map[string]any:
		return c.json.Encode(dst, extractFields(v, c.fields))

	default:
		// For arbitrary types: marshal → map → extract fields.
		m, err := toMap(value)
		if err != nil {
			// toMap fails when value is an array/slice (JSON is [...] not {...}).
			// Fall back to marshaling as an array of objects.
			items, arrErr := toSlice(value)
			if arrErr != nil {
				return err // return the original toMap error
			}
			extracted := make([]map[string]any, len(items))
			for i, item := range items {
				extracted[i] = extractFields(item, c.fields)
			}
			return c.json.Encode(dst, map[string]any{"items": extracted})
		}

		// If the value serialized to an object with an "items" array treat it
		// as a collection (covers the printItems struct used in get.go).
		if raw, ok := m["items"]; ok {
			items := toSliceOfMaps(raw)
			extracted := make([]map[string]any, len(items))
			for i, item := range items {
				extracted[i] = extractFields(item, c.fields)
			}
			return c.json.Encode(dst, map[string]any{"items": extracted})
		}

		return c.json.Encode(dst, extractFields(m, c.fields))
	}
}

func (c *FieldSelectCodec) Decode(src goio.Reader, value any) error {
	return format.NewJSONCodec().Decode(src, value)
}

// Fields returns the list of field paths this codec selects.
func (c *FieldSelectCodec) Fields() []string {
	return c.fields
}

// ExtractFields is the exported equivalent of extractFields, for use by callers
// that need to apply field selection outside of Encode (e.g. partial failure envelopes).
func ExtractFields(obj map[string]any, fields []string) map[string]any {
	return extractFields(obj, fields)
}

// extractFields returns a new map containing only the requested field paths
// and their values. Dot-notation paths are resolved against nested maps.
// A missing path produces a null (nil) value.
func extractFields(obj map[string]any, fields []string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		result[field] = getNestedField(obj, field)
	}
	return result
}

// getNestedField resolves a dot-separated field path in a nested map.
// Returns nil when any segment of the path is missing or not a map.
func getNestedField(obj map[string]any, path string) any {
	parts := strings.SplitN(path, ".", 2)
	val, ok := obj[parts[0]]
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return val
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	return getNestedField(nested, parts[1])
}

// toMap marshals an arbitrary value to JSON and back into a map[string]any.
func toMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// toSlice marshals an arbitrary value to JSON and back into []map[string]any.
// Returns an error if the JSON representation is not an array of objects.
func toSlice(value any) ([]map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// toSliceOfMaps converts an any value to []map[string]any. Values that are
// not slices or whose elements are not maps are treated as empty slices.
func toSliceOfMaps(raw any) []map[string]any {
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(slice))
	for _, elem := range slice {
		if m, ok := elem.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// DiscoverFields enumerates the available field paths from a sample object map.
// It returns top-level keys and one level of spec.* sub-keys, sorted.
func DiscoverFields(obj map[string]any) []string {
	seen := make(map[string]struct{})

	for key, val := range obj {
		seen[key] = struct{}{}
		// Expand spec.* sub-fields one level deep.
		if key == "spec" {
			if nested, ok := val.(map[string]any); ok {
				for subKey := range nested {
					seen["spec."+subKey] = struct{}{}
				}
			}
		}
	}

	paths := make([]string, 0, len(seen))
	for k := range seen {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	return paths
}

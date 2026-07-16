package output

import (
	"encoding/json"
	"fmt"
	goio "io"
	"reflect"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UnknownFieldSelectionError is returned by FieldSelectCodec when the caller
// requests one or more fields that are not present in the value's field set.
// The validator is optional — it is only invoked when wired by the caller
// (e.g. instrumentation list commands via Options.SetJSONFieldValidator).
type UnknownFieldSelectionError struct {
	Fields []string // the offending field names
}

func (e UnknownFieldSelectionError) Error() string {
	return fmt.Sprintf("unknown field(s) in --json: %s. Run --json list to enumerate valid fields.",
		strings.Join(e.Fields, ", "))
}

// FieldSelectCodec wraps the JSON codec and emits only the requested fields
// from each output object. It implements format.Codec.
//
// Field paths support dot-notation (e.g. "metadata.name") which is resolved
// by walking nested maps.
//
// For a single object the output is a flat JSON object containing only the
// selected fields. For k8s unstructured collections (UnstructuredList) or
// objects with an "items" field, the output is {"items": [...]}. For plain
// Go slices, the output preserves the array shape ([...]). Single-key list
// envelopes (an object whose only key holds an array of objects, e.g.
// {"datasources": [...]}) get per-item selection with the wrapper key
// preserved.
//
// Missing fields produce a null value rather than being omitted.
type FieldSelectCodec struct {
	fields    []string
	json      *format.JSONCodec
	validator func(fields []string) error // optional; if non-nil, Encode calls it before field extraction
}

// NewFieldSelectCodec creates a FieldSelectCodec for the given field paths.
func NewFieldSelectCodec(fields []string) *FieldSelectCodec {
	return &FieldSelectCodec{
		fields: fields,
		json:   format.NewJSONCodec(),
	}
}

// NewFieldSelectCodecWithValidator creates a FieldSelectCodec for the given
// field paths, with an optional validator invoked before field extraction.
// If the validator returns an error, Encode returns that error immediately.
func NewFieldSelectCodecWithValidator(fields []string, validator func(fields []string) error) *FieldSelectCodec {
	return &FieldSelectCodec{
		fields:    fields,
		json:      format.NewJSONCodec(),
		validator: validator,
	}
}

func (c *FieldSelectCodec) Format() format.Format {
	return format.JSON
}

// Encode writes the selected fields to dst as JSON.
// If a validator was configured (via NewFieldSelectCodecWithValidator), it is
// invoked before any field extraction. If the validator returns an error,
// Encode returns that error immediately.
func (c *FieldSelectCodec) Encode(dst goio.Writer, value any) error {
	if c.validator != nil {
		if err := c.validator(c.fields); err != nil {
			return err
		}
	}

	switch v := value.(type) {
	case unstructured.UnstructuredList:
		items := make([]map[string]any, len(v.Items))
		for i, item := range v.Items {
			items[i] = extractFields(item.Object, c.fields)
		}
		return c.json.Encode(dst, listFieldSelectionOutput(items, paginationMetadataFromUnstructuredList(v)))

	case *unstructured.UnstructuredList:
		if v == nil {
			return c.json.Encode(dst, listFieldSelectionOutput(nil, nil))
		}
		items := make([]map[string]any, len(v.Items))
		for i, item := range v.Items {
			items[i] = extractFields(item.Object, c.fields)
		}
		return c.json.Encode(dst, listFieldSelectionOutput(items, paginationMetadataFromUnstructuredList(*v)))

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
			// Preserve array shape: output [...] not {"items":[...]}
			return c.json.Encode(dst, extracted)
		}

		// If the value serialized to an object with an "items" array treat it
		// as a collection (covers the printItems struct used in get.go). Preserve
		// pagination metadata from the list envelope even when item fields are
		// selected, so callers can detect and fetch additional pages.
		if raw, ok := m["items"]; ok {
			items := toSliceOfMaps(raw)
			extracted := make([]map[string]any, len(items))
			for i, item := range items {
				extracted[i] = extractFields(item, c.fields)
			}
			return c.json.Encode(dst, listFieldSelectionOutput(extracted, paginationMetadataFromObjectMap(m)))
		}

		// Single-key list envelope (e.g. {"datasources": [...]}): apply field
		// selection per item and preserve the wrapper key. No pagination
		// metadata by construction (the envelope has exactly one key).
		if key, items, ok := singleKeyItems(m); ok {
			extracted := make([]map[string]any, len(items))
			for i, item := range items {
				extracted[i] = extractFields(item, c.fields)
			}
			return c.json.Encode(dst, map[string]any{key: extracted})
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

func listFieldSelectionOutput(items []map[string]any, metadata map[string]any) map[string]any {
	out := map[string]any{"items": items}
	if len(metadata) > 0 {
		out["metadata"] = metadata
	}
	return out
}

func paginationMetadataFromUnstructuredList(list unstructured.UnstructuredList) map[string]any {
	metadata := make(map[string]any, 3)
	if token := list.GetContinue(); token != "" {
		metadata["continue"] = token
	}
	if resourceVersion := list.GetResourceVersion(); resourceVersion != "" {
		metadata["resourceVersion"] = resourceVersion
	}
	if remaining := list.GetRemainingItemCount(); remaining != nil {
		metadata["remainingItemCount"] = *remaining
	}
	return metadata
}

func paginationMetadataFromObjectMap(obj map[string]any) map[string]any {
	rawMetadata, ok := obj["metadata"].(map[string]any)
	if !ok {
		return nil
	}

	metadata := make(map[string]any, 3)
	copyIfPresent := func(key string) {
		value, ok := rawMetadata[key]
		if !ok || !paginationMetadataValuePresent(value) {
			return
		}
		metadata[key] = value
	}
	copyIfPresent("continue")
	copyIfPresent("resourceVersion")
	copyIfPresent("remainingItemCount")
	return metadata
}

func paginationMetadataValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if s, ok := value.(string); ok {
		return s != ""
	}
	return true
}

// singleKeyItems reports whether m is a single-key list envelope: exactly one
// key whose value is an array of objects (or an empty array). Provider list
// commands wrap their items under such keys (e.g. {"datasources": [...]}).
// Callers must check the k8s "items" shape first — that path carries
// pagination metadata and a fixed output shape.
func singleKeyItems(m map[string]any) (string, []map[string]any, bool) {
	if len(m) != 1 {
		return "", nil, false
	}
	for key, raw := range m {
		arr, ok := raw.([]any)
		if !ok {
			return "", nil, false
		}
		items := toSliceOfMaps(arr)
		if len(items) != len(arr) {
			// Elements are not all objects (scalar or mixed array).
			return "", nil, false
		}
		return key, items, true
	}
	return "", nil, false
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

// MakeFieldValidator builds a validator function from a sample value.
// The returned function checks that all requested fields are present in the
// discovered field set derived from the sample. Unknown fields cause the
// validator to return UnknownFieldSelectionError listing the offending names.
//
// The sample should be an instance of the item type (zero or non-zero) — NOT
// the list envelope — so that the validator sees item-level fields.
//
// Field discovery uses reflection to enumerate exported struct fields by their
// JSON names. This correctly handles struct fields tagged `json:"...,omitempty"`
// that would be absent from a zero-value JSON marshal. Fields tagged json:"-"
// are excluded (they are not selectable).
//
// If the field set cannot be derived from the sample (e.g. the type is a
// primitive, map, or interface), the function returns nil (fail open — no
// validation). This prevents false positives for exotic types.
func MakeFieldValidator(sample any) func(fields []string) error {
	// Use reflection to enumerate JSON field names from the struct type.
	// reflectFields (from format.go, same package) handles slices and pointers
	// by unwrapping to the element type, and skips json:"-" fields.
	structFields := reflectFields(reflect.TypeOf(sample))
	if len(structFields) == 0 {
		// Cannot determine the field set — fail open.
		return nil
	}

	allowed := make(map[string]struct{}, len(structFields))
	for _, f := range structFields {
		allowed[f] = struct{}{}
	}

	return func(requested []string) error {
		var unknown []string
		for _, f := range requested {
			if _, ok := allowed[f]; !ok {
				unknown = append(unknown, f)
			}
		}
		if len(unknown) > 0 {
			return UnknownFieldSelectionError{Fields: unknown}
		}
		return nil
	}
}

// DiscoverFields enumerates all dot-notation field paths reachable from a
// sample object map by recursively expanding nested objects. Top-level keys
// are always included; nested objects are expanded to their full depth so
// that deep paths such as "status.links.alert.rule.uid" are discoverable.
func DiscoverFields(obj map[string]any) []string {
	seen := make(map[string]struct{})
	collectFields(obj, "", seen)
	paths := make([]string, 0, len(seen))
	for k := range seen {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	return paths
}

// collectFields recursively walks a nested map and records every dot-notation
// path into seen. Both leaf paths (e.g. "status.state") and intermediate paths
// (e.g. "status.links") are recorded so callers can select at any depth.
func collectFields(obj map[string]any, prefix string, seen map[string]struct{}) {
	for key, val := range obj {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		seen[full] = struct{}{}
		if nested, ok := val.(map[string]any); ok {
			collectFields(nested, full, seen)
		}
	}
}

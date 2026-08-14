package output

import (
	"encoding/json"
	"fmt"
	goio "io"
	"maps"
	"reflect"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UnknownFieldSelectionError is returned by FieldSelectCodec when the caller
// requests one or more fields that are not present in the value's field set.
//
// Two paths produce it. The optional validator runs before extraction and is
// only invoked when wired by the caller (e.g. instrumentation list commands
// via Options.SetJSONFieldValidator). Extraction itself produces it for any
// requested path that exists in no emitted object.
type UnknownFieldSelectionError struct {
	Fields []string // the offending field names

	// Candidates maps an offending name to the real dotted paths that end
	// with it, so a caller who typed a leaf name instead of a path reads the
	// correction in the error. Empty when extraction found no near match.
	Candidates map[string][]string
}

func (e UnknownFieldSelectionError) Error() string {
	msg := fmt.Sprintf("unknown field(s) in --json: %s.", strings.Join(e.Fields, ", "))
	if suggestion := e.suggestion(); suggestion != "" {
		msg += " " + suggestion
	}
	return msg + " Run --json list to enumerate valid fields."
}

// suggestion renders the candidate paths as a "did you mean" clause, in the
// order of e.Fields so the output is stable.
func (e UnknownFieldSelectionError) suggestion() string {
	var paths []string
	for _, field := range e.Fields {
		paths = append(paths, e.Candidates[field]...)
	}
	if len(paths) == 0 {
		return ""
	}
	return fmt.Sprintf("Did you mean %s?", strings.Join(paths, ", "))
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
		items, err := c.selectItems(unstructuredObjects(v.Items))
		if err != nil {
			return err
		}
		return c.json.Encode(dst, listFieldSelectionOutput(items, paginationMetadataFromUnstructuredList(v)))

	case *unstructured.UnstructuredList:
		if v == nil {
			return c.json.Encode(dst, listFieldSelectionOutput(nil, nil))
		}
		items, err := c.selectItems(unstructuredObjects(v.Items))
		if err != nil {
			return err
		}
		return c.json.Encode(dst, listFieldSelectionOutput(items, paginationMetadataFromUnstructuredList(*v)))

	case unstructured.Unstructured:
		return c.encodeOne(dst, v.Object)

	case *unstructured.Unstructured:
		return c.encodeOne(dst, v.Object)

	case map[string]any:
		// A dynamic map is treated as a list envelope only when it carries
		// the reserved list_meta key — attaching the key is the producer's
		// opt-in to the contract. The map may hold native Go values (a
		// *ListMeta, a []map[string]any or typed item slice), so envelope
		// handling runs on a JSON-normalized copy; the key-presence check
		// happens before normalization so native metadata values opt in too,
		// and the reserved shape is validated after. Maps without the key —
		// raw passthrough payloads (e.g. gcx api responses that happen to be
		// items-shaped) — keep whole-object selection on the original value.
		out, handled, err := c.dynamicMapEnvelopeSelection(v)
		if err != nil {
			return err
		}
		if handled {
			return c.json.Encode(dst, out)
		}
		return c.encodeOne(dst, v)

	default:
		// For arbitrary types: marshal → map → extract fields.
		m, err := toMap(value)
		if err == nil {
			// Explicitly-marked list envelope (e.g. {"investigations": [...],
			// "total": 42}): apply field selection per item under the declared
			// key and pass every other key through untouched as list-level
			// metadata.
			if env, ok := value.(ListEnvelope); ok {
				key := env.ListItemsKey()
				extracted, selErr := c.selectItems(toSliceOfMaps(m[key]))
				if selErr != nil {
					return selErr
				}
				out := make(map[string]any, len(m))
				maps.Copy(out, m)
				out[key] = extracted
				return c.json.Encode(dst, out)
			}
		}
		if err != nil {
			// toMap fails when value is an array/slice (JSON is [...] not {...}).
			// Fall back to marshaling as an array of objects.
			items, arrErr := toSlice(value)
			if arrErr != nil {
				return err // return the original toMap error
			}
			extracted, selErr := c.selectItems(items)
			if selErr != nil {
				return selErr
			}
			// Preserve array shape: output [...] not {"items":[...]}
			return c.json.Encode(dst, extracted)
		}

		out, handled, envErr := c.envelopeFieldSelection(m)
		if envErr != nil {
			return envErr
		}
		if handled {
			return c.json.Encode(dst, out)
		}

		return c.encodeOne(dst, m)
	}
}

// dynamicMapEnvelopeSelection applies envelope field selection to a dynamic
// map, and reports whether it handled the value.
//
// A dynamic map counts as a list envelope only when it carries the reserved
// list_meta key — attaching the key is the producer's opt-in to the contract.
// The map may hold native Go values (a *ListMeta, a []map[string]any or a
// typed item slice), so envelope handling runs on a JSON-normalized copy. The
// key-presence check happens before normalization, so native metadata values
// opt in too, and the reserved shape is validated after. A map without the
// key — a raw passthrough payload such as a `gcx api` response that happens
// to be items-shaped — keeps whole-object selection on the original value.
func (c *FieldSelectCodec) dynamicMapEnvelopeSelection(v map[string]any) (map[string]any, bool, error) {
	if _, ok := v[ListMetaKey]; !ok {
		return nil, false, nil
	}
	// A map that does not normalize is not an envelope; whole-object
	// selection on the original value still works, so the failure is not
	// itself an error.
	m, err := toMap(v)
	if err != nil {
		return nil, false, nil //nolint:nilerr // fall back to whole-object selection
	}
	if !hasListMetaEntry(m) {
		return nil, false, nil
	}
	return c.envelopeFieldSelection(m)
}

// selectItems applies field selection to a list of objects, rejecting any
// requested path that exists in none of them.
func (c *FieldSelectCodec) selectItems(objs []map[string]any) ([]map[string]any, error) {
	return selectFields(objs, c.fields)
}

// encodeOne applies field selection to a single object and writes it.
func (c *FieldSelectCodec) encodeOne(dst goio.Writer, obj map[string]any) error {
	selected, err := selectFields([]map[string]any{obj}, c.fields)
	if err != nil {
		return err
	}
	return c.json.Encode(dst, selected[0])
}

// unstructuredObjects unwraps a slice of unstructured items to their maps.
func unstructuredObjects(items []unstructured.Unstructured) []map[string]any {
	objs := make([]map[string]any, len(items))
	for i, item := range items {
		objs[i] = item.Object
	}
	return objs
}

// envelopeFieldSelection applies per-item field selection when m is a list
// envelope, returning the selected output and true. Two envelope shapes are
// recognized:
//
//   - an "items"-keyed map (covers the printItems struct used in get.go and
//     the k8s list shape) — pagination metadata from the envelope is
//     preserved so callers can detect and fetch additional pages;
//   - a single-key list envelope (e.g. {"datasources": [...]}), optionally
//     with a reserved list_meta sibling.
//
// The reserved list_meta truncation-metadata entry is re-attached in both
// cases so the signal survives field selection. Returns false for any other
// shape (the caller falls back to whole-object selection).
func (c *FieldSelectCodec) envelopeFieldSelection(m map[string]any) (map[string]any, bool, error) {
	if raw, ok := m["items"]; ok {
		extracted, err := c.selectItems(toSliceOfMaps(raw))
		if err != nil {
			return nil, false, err
		}
		out := listFieldSelectionOutput(extracted, paginationMetadataFromObjectMap(m))
		attachListMetaEntry(out, m)
		return out, true, nil
	}

	if key, items, ok := singleKeyItems(m); ok {
		extracted, err := c.selectItems(items)
		if err != nil {
			return nil, false, err
		}
		out := map[string]any{key: extracted}
		attachListMetaEntry(out, m)
		return out, true, nil
	}

	// A single-key envelope whose items are scalars (e.g. {"data": ["a"],
	// "list_meta": {...}}) never matches singleKeyItems, but a truncated page
	// must keep its reserved metadata under field selection all the same.
	// Scalar items have no fields to select into, so selection runs on the
	// whole envelope and the reserved entry is re-attached.
	if hasListMetaEntry(m) && singleKeyScalarArray(m) {
		selected, err := selectFields([]map[string]any{m}, c.fields)
		if err != nil {
			return nil, false, err
		}
		out := selected[0]
		attachListMetaEntry(out, m)
		return out, true, nil
	}

	return nil, false, nil
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

// selectFields applies field selection across a set of objects and rejects
// any requested path that exists in none of them.
//
// A path that resolves nowhere used to produce one null per row. A caller who
// typed a leaf name (`username`) rather than a path (`spec.username`) then
// read a full result set of nulls, and a script that searched that result
// found nothing and reported zero. The absence of the path is a caller error,
// so it fails here instead.
//
// The check is per-path existence, not per-value: a path that exists and
// holds null in every object is a real field and stays. It is also across the
// whole set, so a heterogeneous list keeps a path that only some objects
// carry. An empty set skips the check, because it proves nothing about the
// field names.
func selectFields(objs []map[string]any, fields []string) ([]map[string]any, error) {
	selected := make([]map[string]any, len(objs))
	present := make(map[string]bool, len(fields))

	for i, obj := range objs {
		result := make(map[string]any, len(fields))
		for _, field := range fields {
			value, ok := lookupNestedField(obj, field)
			if ok {
				present[field] = true
			}
			result[field] = value
		}
		selected[i] = result
	}

	if len(objs) == 0 {
		return selected, nil
	}
	if err := absentFieldError(objs, fields, present); err != nil {
		return nil, err
	}
	return selected, nil
}

// absentFieldError builds the error for every requested field that exists in
// no object, with the real dotted paths that end with the same name.
func absentFieldError(objs []map[string]any, fields []string, present map[string]bool) error {
	var absent []string
	for _, field := range fields {
		if !present[field] {
			absent = append(absent, field)
		}
	}
	if len(absent) == 0 {
		return nil
	}
	return UnknownFieldSelectionError{Fields: absent, Candidates: candidatePaths(objs, absent)}
}

// candidatePathSampleSize bounds how many objects candidatePaths walks. The
// suggestion is a convenience, so a long list pays for the first objects only.
const candidatePathSampleSize = 20

// candidatePaths returns, per absent name, the dotted paths in the sampled
// objects whose last segment equals that name. A name that is already dotted
// gets no candidate: the caller wrote a path, and it does not exist.
func candidatePaths(objs []map[string]any, absent []string) map[string][]string {
	wanted := make(map[string]bool, len(absent))
	for _, field := range absent {
		if !strings.Contains(field, ".") {
			wanted[field] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	seen := make(map[string]map[string]bool, len(wanted))
	for i, obj := range objs {
		if i >= candidatePathSampleSize {
			break
		}
		for _, path := range DiscoverFields(obj) {
			leaf := path[strings.LastIndex(path, ".")+1:]
			if !wanted[leaf] || leaf == path {
				continue
			}
			if seen[leaf] == nil {
				seen[leaf] = make(map[string]bool)
			}
			seen[leaf][path] = true
		}
	}

	candidates := make(map[string][]string, len(seen))
	for leaf, paths := range seen {
		list := make([]string, 0, len(paths))
		for path := range paths {
			list = append(list, path)
		}
		sort.Strings(list)
		candidates[leaf] = list
	}
	return candidates
}

// getNestedField resolves a dot-separated field path in a nested map.
// Returns nil when any segment of the path is missing or not a map.
func getNestedField(obj map[string]any, path string) any {
	value, _ := lookupNestedField(obj, path)
	return value
}

// lookupNestedField resolves a dot-separated field path in a nested map and
// reports whether the path exists. A path that exists and holds null returns
// (nil, true); a path that does not exist returns (nil, false).
func lookupNestedField(obj map[string]any, path string) (any, bool) {
	parts := strings.SplitN(path, ".", 2)
	val, ok := obj[parts[0]]
	if !ok {
		return nil, false
	}
	if len(parts) == 1 {
		return val, true
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return nil, false
	}
	return lookupNestedField(nested, parts[1])
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

// attachListMetaEntry copies the reserved ListMetaKey truncation-metadata
// object from the source envelope into the field-selection output, so a
// truncated page stays machine-legible under --json field selection.
func attachListMetaEntry(out, src map[string]any) {
	if lm, ok := src[ListMetaKey].(map[string]any); ok {
		out[ListMetaKey] = lm
	}
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

// ListEnvelope marks a list-result type whose items live under the returned
// JSON key, with any sibling keys carrying list-level metadata (e.g. a
// pagination total). Multi-key list envelopes must implement this so that
// --json field selection and discovery operate on the items rather than the
// envelope; single-key envelopes are detected structurally and need no
// marker. The interface is satisfied structurally, so result types do not
// need to import this package.
type ListEnvelope interface {
	// ListItemsKey returns the JSON key under which the item array lives.
	ListItemsKey() string
}

// singleKeyItems reports whether m is a single-key list envelope: exactly one
// key whose value is an array of objects (or an empty array), optionally
// accompanied by the reserved ListMetaKey truncation-metadata object.
// Provider list commands wrap their items under such keys (e.g.
// {"datasources": [...]} or {"datasources": [...], "list_meta": {...}}).
// Callers must check the k8s "items" shape first — that path carries
// pagination metadata and a fixed output shape.
func singleKeyItems(m map[string]any) (string, []map[string]any, bool) {
	if nonListMetaKeyCount(m) != 1 {
		return "", nil, false
	}
	for key, raw := range m {
		if isListMetaEntry(key, raw) {
			continue
		}
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

// singleKeyScalarArray reports whether m is a single-key envelope (excluding
// a reserved ListMetaKey entry) whose value is an array not composed entirely
// of objects — the shape a []string list envelope normalizes to. Empty arrays
// are not scalar: they satisfy singleKeyItems.
func singleKeyScalarArray(m map[string]any) bool {
	if nonListMetaKeyCount(m) != 1 {
		return false
	}
	for key, raw := range m {
		if isListMetaEntry(key, raw) {
			continue
		}
		arr, ok := raw.([]any)
		return ok && len(toSliceOfMaps(arr)) != len(arr)
	}
	return false
}

// nonListMetaKeyCount counts m's keys excluding a reserved ListMetaKey entry
// (see isListMetaEntry). Used to recognize list envelopes regardless of
// whether truncation metadata rides alongside the items key.
func nonListMetaKeyCount(m map[string]any) int {
	n := 0
	for key, val := range m {
		if isListMetaEntry(key, val) {
			continue
		}
		n++
	}
	return n
}

// isListMetaEntry reports whether the key/value pair is the reserved
// truncation-metadata sibling: the ListMetaKey key holding an object (or an
// explicit null). Any other value shape under the key is not treated as
// metadata, so unrelated payloads that happen to use the name keep their
// pre-reservation behavior.
func isListMetaEntry(key string, val any) bool {
	if key != ListMetaKey {
		return false
	}
	if val == nil {
		return true
	}
	_, ok := val.(map[string]any)
	return ok
}

// hasListMetaEntry reports whether m carries the reserved truncation-metadata
// sibling (see isListMetaEntry). Producers opt into the list-envelope
// treatment of dynamic maps by attaching the reserved key; maps without it
// keep their pre-reservation behavior.
func hasListMetaEntry(m map[string]any) bool {
	val, ok := m[ListMetaKey]
	return ok && isListMetaEntry(ListMetaKey, val)
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

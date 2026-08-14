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
// order of e.Fields so the output is stable. Each group names the field it
// corrects when more than one field is absent, so the caller reads which
// candidate belongs to which name.
func (e UnknownFieldSelectionError) suggestion() string {
	var groups [][]string
	var names []string
	for _, field := range e.Fields {
		if paths := e.Candidates[field]; len(paths) > 0 {
			groups = append(groups, paths)
			names = append(names, field)
		}
	}
	if len(groups) == 0 {
		return ""
	}

	parts := make([]string, 0, len(groups))
	for i, paths := range groups {
		part := strings.Join(paths, ", ")
		if len(e.Fields) > 1 {
			part += " for " + names[i]
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("Did you mean %s?", strings.Join(parts, "; "))
}

// ArrayPathSelectionError is returned when a requested path continues past an
// array. Field selection walks maps only, so it cannot reach a value inside an
// array; --jq can, because it iterates the array.
type ArrayPathSelectionError struct {
	Fields []string // the offending field paths
}

func (e ArrayPathSelectionError) Error() string {
	return fmt.Sprintf(
		"--json cannot reach a value inside an array: %s. Use --jq to read a value inside an array.",
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
// A requested path that the item type declares but that no object emits
// produces a null value. A path that the item type denies, and that no object
// carries, is a caller error (see selectFields).
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
		items, err := c.selectItems(unstructuredObjects(v.Items), nil)
		if err != nil {
			return err
		}
		return c.json.Encode(dst, listFieldSelectionOutput(items, paginationMetadataFromUnstructuredList(v)))

	case *unstructured.UnstructuredList:
		if v == nil {
			return c.json.Encode(dst, listFieldSelectionOutput(nil, nil))
		}
		items, err := c.selectItems(unstructuredObjects(v.Items), nil)
		if err != nil {
			return err
		}
		return c.json.Encode(dst, listFieldSelectionOutput(items, paginationMetadataFromUnstructuredList(*v)))

	case unstructured.Unstructured:
		return c.encodeOne(dst, v.Object, nil)

	case *unstructured.Unstructured:
		return c.encodeOne(dst, v.Object, nil)

	case map[string]any:
		// See dynamicMapEnvelopeSelection for the opt-in rule.
		out, handled, err := c.dynamicMapEnvelopeSelection(v)
		if err != nil {
			return err
		}
		if handled {
			return c.json.Encode(dst, out)
		}
		return c.encodeOne(dst, v, nil)

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
				extracted, selErr := c.selectItems(toSliceOfMaps(m[key]), sliceElemTypeByKey(reflect.TypeOf(value), key))
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
			extracted, selErr := c.selectItems(items, structTypeOf(value))
			if selErr != nil {
				return selErr
			}
			// Preserve array shape: output [...] not {"items":[...]}
			return c.json.Encode(dst, extracted)
		}

		out, handled, envErr := c.envelopeFieldSelection(m, value)
		if envErr != nil {
			return envErr
		}
		if handled {
			return c.json.Encode(dst, out)
		}

		return c.encodeOne(dst, m, structTypeOf(value))
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
	// A dynamic map declares no field set, so selection falls back to the
	// emitted keys.
	return c.envelopeFieldSelection(m, nil)
}

// selectItems applies field selection to a list of objects, rejecting a
// requested path that itemType denies and that no object carries.
func (c *FieldSelectCodec) selectItems(objs []map[string]any, itemType reflect.Type) ([]map[string]any, error) {
	return selectFields(objs, c.fields, itemType)
}

// encodeOne applies field selection to a single object and writes it.
func (c *FieldSelectCodec) encodeOne(dst goio.Writer, obj map[string]any, objType reflect.Type) error {
	selected, err := selectFields([]map[string]any{obj}, c.fields, objType)
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
// The value argument carries the Go value the map came from, so selection can
// read the declared item type; it is nil for a dynamic map.
func (c *FieldSelectCodec) envelopeFieldSelection(m map[string]any, value any) (map[string]any, bool, error) {
	if raw, ok := m["items"]; ok {
		extracted, err := c.selectItems(toSliceOfMaps(raw), sliceElemTypeByKey(reflect.TypeOf(value), "items"))
		if err != nil {
			return nil, false, err
		}
		out := listFieldSelectionOutput(extracted, paginationMetadataFromObjectMap(m))
		attachListMetaEntry(out, m)
		return out, true, nil
	}

	if key, items, ok := singleKeyItems(m); ok {
		extracted, err := c.selectItems(items, sliceElemTypeByKey(reflect.TypeOf(value), key))
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
		selected, err := selectFields([]map[string]any{m}, c.fields, structTypeOf(value))
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

// ExtractFields copies the requested field paths out of one dynamic map (the
// partial-failure envelope of `gcx resources get`). A path the map does not
// carry holds null. A dynamic map declares no type, so gcx rejects no path
// here — see selectFields for the rule.
func ExtractFields(obj map[string]any, fields []string) map[string]any {
	return extractFields(obj, fields, make(map[string]bool, len(fields)))
}

// selectFields applies field selection across a set of objects, and rejects a
// requested path only when a declared type denies it.
//
// A path that resolves nowhere used to produce one null per row. A caller who
// typed a leaf name (`username`) rather than a path (`spec.username`) then
// read a full result set of nulls, and a script that searched that result
// found nothing and reported zero. The absence of the path is a caller error,
// so a type that denies the path fails it here instead.
//
// A rejection needs a type that denies the path, and it is per-path
// existence, not per-value:
//
//   - itemType is the only authority for a rejection. Where itemType is nil —
//     an unstructured object, a dynamic map, a slice of maps — gcx knows no
//     field set, so it rejects nothing and every requested path keeps its
//     null.
//   - A path that any object emits is real, even when the type does not
//     declare it, so a heterogeneous list keeps a path that only some objects
//     carry.
//   - A field that the type declares but that no object emits — an omitempty
//     field that holds its zero value in every row — keeps its null.
//   - A path that exists and holds null is a real field and stays.
//   - A path that continues past an array is rejected on its own, because
//     field selection walks maps only (see ArrayPathSelectionError).
func selectFields(objs []map[string]any, fields []string, itemType reflect.Type) ([]map[string]any, error) {
	selected := make([]map[string]any, len(objs))
	present := make(map[string]bool, len(fields))
	for i, obj := range objs {
		selected[i] = extractFields(obj, fields, present)
	}
	if itemType == nil {
		return selected, nil
	}

	var missing []string
	for _, field := range fields {
		if !present[field] {
			missing = append(missing, field)
		}
	}
	unknown, inArray := classifyPaths(itemType, missing)
	if len(inArray) > 0 {
		return nil, ArrayPathSelectionError{Fields: inArray}
	}
	if len(unknown) == 0 {
		return selected, nil
	}
	return nil, UnknownFieldSelectionError{
		Fields:     unknown,
		Candidates: candidatePaths(objs, itemType, unknown),
	}
}

// extractFields copies the requested paths out of one object. A path the
// object does not carry holds null. Each path that resolves is recorded in
// present, so the caller sees which paths the object set carries.
func extractFields(obj map[string]any, fields []string, present map[string]bool) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		value, ok := lookupNestedField(obj, field)
		if ok {
			present[field] = true
		}
		result[field] = value
	}
	return result
}

// classifyPaths sorts the given paths into the ones the type does not declare
// and the ones that continue past an array. A path the type declares appears
// in neither list.
func classifyPaths(itemType reflect.Type, paths []string) ([]string, []string) {
	var unknown, inArray []string
	for _, path := range paths {
		switch resolvePath(unwrapType(itemType), path) {
		case pathInsideArray:
			inArray = append(inArray, path)
		case pathAbsent:
			unknown = append(unknown, path)
		case pathPresent:
		}
	}
	return unknown, inArray
}

// candidatePathSampleSize bounds how many objects candidatePaths walks. The
// suggestion is a convenience, so a long list pays for the first objects only.
const candidatePathSampleSize = 20

// candidatePaths returns, per absent name, the dotted paths whose last
// segment equals that name. It reads the sampled objects and the declared
// type, so an empty result set still names the real path.
func candidatePaths(objs []map[string]any, itemType reflect.Type, absent []string) map[string][]string {
	var paths []string
	for i, obj := range objs {
		if i >= candidatePathSampleSize {
			break
		}
		paths = append(paths, DiscoverFields(obj)...)
	}
	paths = append(paths, typePaths(unwrapType(itemType), "", typePathDepth)...)
	return candidatesByLeaf(paths, absent)
}

// candidatesByLeaf groups the known dotted paths under the absent name that
// each one ends with. A name that is already dotted gets no candidate: the
// caller wrote a path, and it does not exist. A top-level path is no
// candidate either, because the caller already wrote that name.
func candidatesByLeaf(paths, absent []string) map[string][]string {
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
	for _, path := range paths {
		leaf := path[strings.LastIndex(path, ".")+1:]
		if !wanted[leaf] || leaf == path {
			continue
		}
		if seen[leaf] == nil {
			seen[leaf] = make(map[string]bool)
		}
		seen[leaf][path] = true
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

// pathVerdict is the answer a Go type gives about a dotted field path.
type pathVerdict int

const (
	// pathAbsent: the type declares no such path.
	pathAbsent pathVerdict = iota
	// pathPresent: the type declares the path, or the path lands in a map or
	// an interface, which can hold anything.
	pathPresent
	// pathInsideArray: the path continues past an array, which field
	// selection cannot walk.
	pathInsideArray
)

// resolvePath reports how the Go type answers the dotted field path. It
// matches each segment against the JSON names of the struct fields, and walks
// into the field type for the rest of the path. A segment that lands on a map
// or an interface accepts the rest of the path: the type cannot say what such
// a value holds. A segment that lands on an array stops the walk, because
// lookupNestedField descends into maps only.
//
// selectFields uses it to keep a field that the type declares but that no
// object emits, and MakeFieldValidator uses it to check a requested path.
func resolvePath(t reflect.Type, path string) pathVerdict {
	t = derefType(t)
	if t == nil {
		return pathAbsent
	}
	switch t.Kind() {
	case reflect.Map, reflect.Interface:
		return pathPresent
	case reflect.Array, reflect.Slice:
		return pathInsideArray
	case reflect.Struct:
	default:
		return pathAbsent
	}

	name, rest, nested := strings.Cut(path, ".")
	field, ok := jsonFieldByName(t, name)
	if !ok {
		return pathAbsent
	}
	if !nested {
		return pathPresent
	}
	return resolvePath(field.Type, rest)
}

// typePathDepth bounds the walk that typePaths makes. A deep or recursive
// type would otherwise never end, and the paths only feed a suggestion.
const typePathDepth = 4

// typePaths enumerates the dotted field paths that the type declares, down to
// typePathDepth levels. MakeFieldValidator uses them to suggest the real path
// for a leaf name. The walk stops at an array, because field selection cannot
// reach a value inside one, and a suggestion must name a path that works.
func typePaths(t reflect.Type, prefix string, depth int) []string {
	t = derefType(t)
	if t == nil || t.Kind() != reflect.Struct || depth <= 0 {
		return nil
	}

	var paths []string
	for f := range t.Fields() {
		name, ok := jsonFieldName(f)
		if !ok {
			continue
		}
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		paths = append(paths, full)
		paths = append(paths, typePaths(f.Type, full, depth-1)...)
	}
	return paths
}

// derefType reduces a pointer type to the type it points at.
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// unwrapType reduces a pointer, slice, or array type to the type it holds.
func unwrapType(t reflect.Type) reflect.Type {
	for t != nil {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			t = t.Elem()
		default:
			return t
		}
	}
	return nil
}

// structTypeOf returns the type of value when it unwraps to a struct, and nil
// otherwise. A dynamic map or an unstructured object declares no field set.
func structTypeOf(value any) reflect.Type {
	t := unwrapType(reflect.TypeOf(value))
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

// sliceElemTypeByKey returns the struct type of the elements of the slice
// field whose JSON name matches key. Returns nil when t does not unwrap to a
// struct, has no such field, or the elements are not structs.
func sliceElemTypeByKey(t reflect.Type, key string) reflect.Type {
	t = unwrapType(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	field, ok := jsonFieldByName(t, key)
	if !ok || field.Type.Kind() != reflect.Slice {
		return nil
	}
	elem := unwrapType(field.Type)
	if elem == nil || elem.Kind() != reflect.Struct {
		return nil
	}
	return elem
}

// jsonFieldByName returns the struct field of t whose JSON name matches name.
func jsonFieldByName(t reflect.Type, name string) (reflect.StructField, bool) {
	for f := range t.Fields() {
		if fieldName, ok := jsonFieldName(f); ok && fieldName == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// jsonFieldName returns the JSON name of a struct field, and reports whether
// the field is selectable at all. An unexported field and a field tagged
// json:"-" are not. A field with no json tag uses its Go name.
func jsonFieldName(f reflect.StructField) (string, bool) {
	if !f.IsExported() {
		return "", false
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	return name, true
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
// The returned function checks that the type of the sample carries every
// requested path. An unknown path makes the validator return
// UnknownFieldSelectionError, which names the offending paths and the real
// dotted paths that end with the same leaf name. A path that continues past
// an array makes it return ArrayPathSelectionError.
//
// The sample should be an instance of the item type (zero or non-zero) — NOT
// the list envelope — so that the validator sees item-level fields.
//
// The check uses the same resolvePath walk as selectFields, so both routes
// accept the same paths. It reads the declared type, which correctly handles
// a field tagged `json:"...,omitempty"` that a zero-value marshal leaves out.
// A field tagged json:"-" is not selectable.
//
// If the field set cannot be derived from the sample (e.g. the type is a
// primitive, map, or interface), the function returns nil (fail open — no
// validation). This prevents false positives for exotic types.
func MakeFieldValidator(sample any) func(fields []string) error {
	// reflectFields (from format.go, same package) returns nothing for a type
	// that declares no field set, which is the fail-open case.
	itemType := reflect.TypeOf(sample)
	if len(reflectFields(itemType)) == 0 {
		return nil
	}

	return func(requested []string) error {
		unknown, inArray := classifyPaths(itemType, requested)
		if len(inArray) > 0 {
			return ArrayPathSelectionError{Fields: inArray}
		}
		if len(unknown) == 0 {
			return nil
		}
		return UnknownFieldSelectionError{
			Fields:     unknown,
			Candidates: candidatesByLeaf(typePaths(unwrapType(itemType), "", typePathDepth), unknown),
		}
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

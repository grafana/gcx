package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/itchyny/gojq"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	jsonDiscoverySentinel     = "?"
	jsonDiscoveryListSentinel = "list"
)

type Options struct {
	OutputFormat  string
	JSONFields    []string
	JSONDiscovery bool

	// IsPiped reports whether stdout is not connected to a terminal.
	// Populated from terminal.IsPiped() during BindFlags.
	IsPiped bool

	// NoTruncate reports whether table column truncation should be suppressed.
	// Populated from terminal.NoTruncate() during BindFlags.
	NoTruncate bool

	// ErrWriter is the writer for hints and diagnostics (defaults to os.Stderr).
	ErrWriter io.Writer

	customCodecs        map[string]format.Codec
	defaultFormat       string
	flags               *pflag.FlagSet
	jsonFieldValidator  func(fields []string) error // optional; invoked before field extraction when --json is used
	jqQuery             *gojq.Query                 // compiled --jq query; nil when flag not set
	jsonFieldsHintShown bool
}

// SetJSONFieldValidator registers an optional validator invoked before field
// extraction when --json is used for field selection. The validator receives
// the list of requested field names and may return UnknownFieldSelectionError
// (or any error) to abort encoding with an error.
//
// The validator is NOT invoked for --json list (field discovery) — that path
// enumerates available fields and returns them; selection is not performed.
func (opts *Options) SetJSONFieldValidator(validator func(fields []string) error) {
	opts.jsonFieldValidator = validator
}

func (opts *Options) RegisterCustomCodec(name string, codec format.Codec) {
	if opts.customCodecs == nil {
		opts.customCodecs = make(map[string]format.Codec)
	}

	opts.customCodecs[name] = codec
}

func (opts *Options) DefaultFormat(name string) {
	opts.defaultFormat = name
}

func (opts *Options) BindFlags(flags *pflag.FlagSet) {
	defaultFormat := "json"
	if opts.defaultFormat != "" {
		defaultFormat = opts.defaultFormat
	}

	// Agent mode: override any per-command default with the agents codec.
	// Explicit -o flag from user still takes precedence (via cobra flag parsing).
	if agent.IsAgentMode() {
		defaultFormat = string(agentsFormat)
	}

	// Populate pipe/truncation state from package-level terminal detection.
	// These are set by root PersistentPreRun via terminal.Detect() and
	// terminal.SetNoTruncate(). Codecs may also read terminal state directly.
	opts.IsPiped = terminal.IsPiped()
	opts.NoTruncate = terminal.NoTruncate()

	flags.StringVarP(&opts.OutputFormat, "output", "o", defaultFormat, "Output format. One of: "+strings.Join(opts.allowedCodecs(), ", "))
	flags.String("json", "", "Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields")
	flags.String("jq", "", "jq expression to apply to JSON output. Mutually exclusive with --json.")

	opts.flags = flags
}

func (opts *Options) Validate() error {
	codec := opts.codecFor(opts.OutputFormat)
	if codec == nil {
		return fmt.Errorf("unknown output format '%s'. Valid formats are: %s", opts.OutputFormat, strings.Join(opts.allowedCodecs(), ", "))
	}

	if err := opts.applyJSONFlag(); err != nil {
		return err
	}
	return opts.applyJQFlag()
}

// applyJSONFlag processes the --json flag value. When -o/--output is explicitly
// set to a non-JSON format, it returns an error because field selection only
// works with JSON output. Combining -o json with --json is allowed since
// there is no conflict. The agents format is intentionally excluded — in agent
// mode the implicit default is agents, and users should pass only --json
// (without an explicit -o) to combine field selection with the agents codec.
func (opts *Options) applyJSONFlag() error {
	if opts.flags == nil {
		return nil
	}

	jsonFlag := opts.flags.Lookup("json")
	if jsonFlag == nil || !jsonFlag.Changed {
		return nil
	}

	// Only reject when -o is explicitly set to a non-JSON format.
	// -o json (or omitted) is fine — --json implies JSON anyway.
	outputFlag := opts.flags.Lookup("output")
	if outputFlag != nil && outputFlag.Changed &&
		outputFlag.Value.String() != "json" {
		return fmt.Errorf("--json requires JSON output, but -o %s was specified", outputFlag.Value.String())
	}

	jsonValue := jsonFlag.Value.String()
	if jsonValue == jsonDiscoverySentinel || jsonValue == jsonDiscoveryListSentinel {
		opts.JSONDiscovery = true
		opts.OutputFormat = "json" // force JSON so Encode routes to encodeDiscovery for table-default commands
		return nil
	}

	fields := strings.Split(jsonValue, ",")
	nonEmpty := fields[:0]
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			nonEmpty = append(nonEmpty, f)
		}
	}
	opts.JSONFields = nonEmpty
	opts.OutputFormat = "json"

	return nil
}

// applyJQFlag processes the --jq flag. The flag is mutually exclusive with
// --json (both field selection and discovery): jq strictly supersedes those
// mechanisms, so combining them adds confusion without value. Also, the two
// resources/* commands that bypass Options.Encode() and construct
// FieldSelectCodec directly (get.go, schemas.go) only fire when JSONFields or
// JSONDiscovery is set — mutual exclusion preserves correctness there.
//
// When -o is unset, --jq auto-flips OutputFormat to "json" (mirrors --json).
// An explicit non-JSON -o is rejected because jq operates on JSON input.
// The expression is parsed eagerly so syntax errors surface during validation,
// not encoding.
func (opts *Options) applyJQFlag() error {
	if opts.flags == nil {
		return nil
	}

	jqFlag := opts.flags.Lookup("jq")
	if jqFlag == nil || !jqFlag.Changed {
		return nil
	}

	jsonFlag := opts.flags.Lookup("json")
	if jsonFlag != nil && jsonFlag.Changed {
		return errors.New("--jq and --json cannot be used together; use jq selectors instead of --json field selection")
	}

	outputFlag := opts.flags.Lookup("output")
	if outputFlag != nil && outputFlag.Changed && outputFlag.Value.String() != "json" {
		return fmt.Errorf("--jq requires JSON output, but -o %s was specified", outputFlag.Value.String())
	}

	query, err := gojq.Parse(jqFlag.Value.String())
	if err != nil {
		return fmt.Errorf("invalid --jq expression: %w", err)
	}

	opts.jqQuery = query
	opts.OutputFormat = "json"
	return nil
}

// Codec returns the codec for the configured output format.
// We have to return an interface here.
func (opts *Options) Codec() (format.Codec, error) { //nolint:ireturn
	codec := opts.codecFor(opts.OutputFormat)
	if codec == nil {
		return nil, fmt.Errorf(
			"unknown output format '%s'. Valid formats are: %s", opts.OutputFormat, strings.Join(opts.allowedCodecs(), ", "),
		)
	}

	return codec, nil
}

func (opts *Options) Encode(dst io.Writer, value any) error {
	codec, err := opts.Codec()
	if err != nil {
		return err
	}

	// In agent mode, nudge toward --json field selection / --jq transformation
	// whenever the resolved codec is JSON-like (json or agents format). The
	// hint still fires when --json field1,field2 is in use — the caller may
	// not realize --jq exists for transformation (group_by, filter, count).
	// Suppressed when --jq is already in use (caller already has the more
	// powerful tool) or when --json list is requested (discovery output is
	// not a transformation target). Emitted once per invocation to stderr
	// (never pollutes stdout) as JSONL {"class":"hint",...} via emitHint
	// (FR-104). Suppressed outside agent mode to avoid noise on TTYs.
	isJSONLike := codec.Format() == format.JSON || codec.Format() == agentsFormat
	if !opts.jsonFieldsHintShown && agent.IsAgentMode() && isJSONLike && !opts.JSONDiscovery && opts.jqQuery == nil {
		opts.jsonFieldsHintShown = true
		w := opts.ErrWriter
		if w == nil {
			w = os.Stderr
		}
		emitHint(w,
			"use --json list / --json field1,field2 for field selection, or --jq '<expr>' for transformation (group_by, filter, count) — no external parsing needed",
			"",
		)
	}

	// Intercept JSON field discovery, field selection, and jq transformation
	// when the resolved codec is JSON-like. Commands that already check
	// JSONFields/JSONDiscovery before calling Encode() will never reach here
	// (they return early), so there is no double-application risk. --jq is
	// mutually exclusive with --json (enforced in applyJQFlag), so the order
	// of the branches below does not matter for correctness.
	if isJSONLike {
		if opts.jqQuery != nil {
			return NewJQCodec(opts.jqQuery).Encode(dst, value)
		}
		if opts.JSONDiscovery {
			return opts.encodeDiscovery(dst, value)
		}
		if len(opts.JSONFields) > 0 {
			return NewFieldSelectCodecWithValidator(opts.JSONFields, opts.jsonFieldValidator).Encode(dst, value)
		}
	}

	return codec.Encode(dst, value)
}

// encodeDiscovery marshals value to discover its available field names, prints
// them one per line, and returns without encoding the full value.
func (opts *Options) encodeDiscovery(dst io.Writer, value any) error {
	obj, err := marshalToSampleMap(value)
	if err != nil {
		return fmt.Errorf("field discovery: %w", err)
	}
	for _, field := range DiscoverFields(obj) {
		fmt.Fprintln(dst, field)
	}
	return nil
}

// marshalToSampleMap converts an arbitrary value into a single map[string]any
// suitable for field discovery. For slices/arrays it returns the first element.
// Handles unstructured.Unstructured and unstructured.UnstructuredList directly
// because their value-type MarshalJSON may not be available (pointer receiver).
func marshalToSampleMap(value any) (map[string]any, error) {
	// Handle k8s unstructured types directly — avoids MarshalJSON pointer
	// receiver issues and is more efficient than marshal/unmarshal.
	switch v := value.(type) {
	case unstructured.Unstructured:
		return v.Object, nil
	case *unstructured.Unstructured:
		return v.Object, nil
	case unstructured.UnstructuredList:
		if len(v.Items) > 0 {
			return v.Items[0].Object, nil
		}
		return nil, errors.New("cannot discover fields from empty UnstructuredList")
	case *unstructured.UnstructuredList:
		if len(v.Items) > 0 {
			return v.Items[0].Object, nil
		}
		return nil, errors.New("cannot discover fields from empty UnstructuredList")
	case map[string]any:
		return v, nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	// Try as object first.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		return sampleFromObject(m, value), nil
	}

	// Try as array — use first element.
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err == nil {
		if len(arr) > 0 {
			return arr[0], nil
		}
		// Empty array: fall through to reflection-based field enumeration below.
	}

	// Reflection fallback: enumerate exported struct fields from the Go type.
	// Handles empty typed slices where there is no data to sample.
	if fields := reflectFields(reflect.TypeOf(value)); len(fields) > 0 {
		return nullFieldMap(fields), nil
	}

	return nil, fmt.Errorf("cannot discover fields from %T: not a JSON object or array", value)
}

// reflectFields enumerates the JSON field names of a Go struct type using
// reflection. Handles slices and pointers by unwrapping to the element type.
// Returns nil if the type is not a struct after unwrapping.
// Fields tagged json:"-" are excluded. Fields with no json tag use the
// struct field name.
func reflectFields(t reflect.Type) []string {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var fields []string
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		fields = append(fields, name)
	}
	return fields
}

// sampleFromObject picks the representative sample map for field discovery
// from a marshaled object: the first element of an "items" array or of a
// single-key list envelope (e.g. {"datasources": [...]}), reflected item
// fields for an envelope with no rows, or the object itself.
func sampleFromObject(m map[string]any, value any) map[string]any {
	// If the object has an "items" array, use the first element.
	if raw, ok := m["items"]; ok {
		if items := toSliceOfMaps(raw); len(items) > 0 {
			return items[0]
		}
	}
	// Single-key list envelope: sample the first item so discovery lists
	// item-level fields.
	if _, items, ok := singleKeyItems(m); ok && len(items) > 0 {
		return items[0]
	}
	// Single-key envelope with no rows (empty or nil slice): reflect on the
	// wrapper struct's sole slice field so discovery still works.
	if len(m) == 1 {
		if fields := reflectSingleSliceField(reflect.TypeOf(value)); len(fields) > 0 {
			return nullFieldMap(fields)
		}
	}
	return m
}

// nullFieldMap builds a discovery sample map whose keys are the given field
// names, each mapped to nil. Used when there is no data row to sample and the
// field set has to come from reflection instead.
func nullFieldMap(fields []string) map[string]any {
	result := make(map[string]any, len(fields))
	for _, f := range fields {
		result[f] = nil
	}
	return result
}

// reflectSingleSliceField returns the JSON field names of the element type of
// a struct's sole exported field, which must be a slice of structs. Returns
// nil unless t (after pointer unwrapping) is a struct with exactly one
// exported non-json:"-" field of slice kind. Used to discover item fields of
// an empty single-key list envelope.
func reflectSingleSliceField(t reflect.Type) []string {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var sliceType reflect.Type
	exported := 0
	for f := range t.Fields() {
		if !f.IsExported() || f.Tag.Get("json") == "-" {
			continue
		}
		exported++
		if f.Type.Kind() == reflect.Slice {
			sliceType = f.Type
		}
	}
	if exported != 1 || sliceType == nil {
		return nil
	}
	return reflectFields(sliceType)
}

// We have to return an interface here.
func (opts *Options) codecFor(format string) format.Codec { //nolint:ireturn
	if opts.customCodecs != nil && opts.customCodecs[format] != nil {
		return opts.customCodecs[format]
	}

	return opts.builtinCodecs()[format]
}

func (opts *Options) builtinCodecs() map[string]format.Codec {
	errWriter := opts.ErrWriter
	if errWriter == nil {
		errWriter = os.Stderr
	}
	return map[string]format.Codec{
		"yaml":   format.NewYAMLCodec(),
		"json":   format.NewJSONCodec(),
		"agents": newAgentsCodec(errWriter),
	}
}

func (opts *Options) allowedCodecs() []string {
	// Merge builtins and custom codecs into a set so that custom codecs
	// that shadow a builtin name (e.g. a custom "json" codec) are not listed twice.
	all := make(map[string]struct{})
	for name := range opts.builtinCodecs() {
		all[name] = struct{}{}
	}
	for name := range opts.customCodecs {
		all[name] = struct{}{}
	}

	allowedCodecs := slices.Collect(maps.Keys(all))
	sort.Strings(allowedCodecs)

	return allowedCodecs
}

// EmitHint writes a hint diagnostic to w. In agent mode the record is JSONL
// with class:"hint" to match the FR-104 typed-class schema used by provider
// commands. In TTY mode the line is "hint: <summary>" (with ": <command>"
// appended when command is non-empty). command may be empty.
func EmitHint(w io.Writer, summary, command string) {
	if agent.IsAgentMode() {
		type hintEvent struct {
			Class   string `json:"class"`
			Summary string `json:"summary"`
			Command string `json:"command,omitempty"`
		}
		b, _ := json.Marshal(hintEvent{Class: "hint", Summary: summary, Command: command}) //nolint:errchkjson
		fmt.Fprintln(w, string(b))
		return
	}
	if command != "" {
		fmt.Fprintf(w, "hint: %s: %s\n", summary, command)
		return
	}
	fmt.Fprintf(w, "hint: %s\n", summary)
}

// emitHint is the package-private call-through to EmitHint for backward
// compatibility with callers within this package.
func emitHint(w io.Writer, summary, command string) {
	EmitHint(w, summary, command)
}

// EmitWarn writes a warn-class diagnostic to w. In agent mode the record is
// JSONL with class:"warning" to match the FR-104 typed-class schema used by
// provider commands. In TTY mode the line is "warn: <summary>".
func EmitWarn(w io.Writer, summary string) {
	if agent.IsAgentMode() {
		type warnEvent struct {
			Class   string `json:"class"`
			Summary string `json:"summary"`
		}
		b, _ := json.Marshal(warnEvent{Class: "warning", Summary: summary}) //nolint:errchkjson
		fmt.Fprintln(w, string(b))
		return
	}
	fmt.Fprintf(w, "warn: %s\n", summary)
}

// EmitNote writes a note-class diagnostic to w. In agent mode the record is
// JSONL with class:"note" to match the FR-104 typed-class schema used by
// provider commands. In TTY mode the line is "note: <summary>".
func EmitNote(w io.Writer, summary string) {
	if agent.IsAgentMode() {
		type noteEvent struct {
			Class   string `json:"class"`
			Summary string `json:"summary"`
		}
		b, _ := json.Marshal(noteEvent{Class: "note", Summary: summary}) //nolint:errchkjson
		fmt.Fprintln(w, string(b))
		return
	}
	fmt.Fprintf(w, "note: %s\n", summary)
}

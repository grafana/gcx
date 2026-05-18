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

	customCodecs       map[string]format.Codec
	defaultFormat      string
	flags              *pflag.FlagSet
	jsonFieldValidator func(fields []string) error // optional; invoked before field extraction when --json is used
	agentHintShown     bool
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

	opts.flags = flags
}

func (opts *Options) Validate() error {
	codec := opts.codecFor(opts.OutputFormat)
	if codec == nil {
		return fmt.Errorf("unknown output format '%s'. Valid formats are: %s", opts.OutputFormat, strings.Join(opts.allowedCodecs(), ", "))
	}

	return opts.applyJSONFlag()
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

	// In agent mode, nudge toward --json field selection when the command
	// outputs JSON without explicit --json usage. The hint is emitted
	// once per command invocation to stderr so it doesn't pollute stdout.
	isJSONLike := codec.Format() == format.JSON || codec.Format() == agentsFormat
	if !opts.agentHintShown && agent.IsAgentMode() && isJSONLike && len(opts.JSONFields) == 0 && !opts.JSONDiscovery {
		opts.agentHintShown = true
		w := opts.ErrWriter
		if w == nil {
			w = os.Stderr
		}
		fmt.Fprintln(w, "hint: use --json list to discover fields, --json field1,field2 to select — no external parsing needed")
	}

	// Intercept JSON field discovery and field selection when the resolved
	// codec is JSON-like. Commands that already check JSONFields/JSONDiscovery
	// before calling Encode() will never reach here (they return early), so
	// there is no double-application risk.
	if isJSONLike {
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
		// If the object has an "items" array, use the first element.
		if raw, ok := m["items"]; ok {
			if items := toSliceOfMaps(raw); len(items) > 0 {
				return items[0], nil
			}
		}
		return m, nil
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
		result := make(map[string]any, len(fields))
		for _, f := range fields {
			result[f] = nil
		}
		return result, nil
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

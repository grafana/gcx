package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const jsonDiscoverySentinel = "?"

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

	customCodecs  map[string]format.Codec
	defaultFormat string
	flags         *pflag.FlagSet
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

	// Agent mode: override any per-command default with JSON.
	// Explicit -o flag from user still takes precedence (via cobra flag parsing).
	if agent.IsAgentMode() {
		defaultFormat = "json"
	}

	// Populate pipe/truncation state from package-level terminal detection.
	// These are set by root PersistentPreRun via terminal.Detect() and
	// terminal.SetNoTruncate(). Codecs may also read terminal state directly.
	opts.IsPiped = terminal.IsPiped()
	opts.NoTruncate = terminal.NoTruncate()

	flags.StringVarP(&opts.OutputFormat, "output", "o", defaultFormat, "Output format. One of: "+strings.Join(opts.allowedCodecs(), ", "))
	flags.String("json", "", "Comma-separated list of fields to include in JSON output, or '?' to discover available fields")

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
// there is no conflict.
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
	if outputFlag != nil && outputFlag.Changed && outputFlag.Value.String() != "json" {
		return fmt.Errorf("--json requires JSON output, but -o %s was specified", outputFlag.Value.String())
	}

	jsonValue := jsonFlag.Value.String()
	if jsonValue == jsonDiscoverySentinel {
		opts.JSONDiscovery = true
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

	// Intercept JSON field discovery and field selection when the resolved
	// codec is JSON. Commands that already check JSONFields/JSONDiscovery
	// before calling Encode() will never reach here (they return early), so
	// there is no double-application risk.
	if codec.Format() == format.JSON {
		if opts.JSONDiscovery {
			return opts.encodeDiscovery(dst, value)
		}
		if len(opts.JSONFields) > 0 {
			return NewFieldSelectCodec(opts.JSONFields).Encode(dst, value)
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
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		return arr[0], nil
	}

	return nil, fmt.Errorf("cannot discover fields from %T: not a JSON object or array", value)
}

// We have to return an interface here.
func (opts *Options) codecFor(format string) format.Codec { //nolint:ireturn
	if opts.customCodecs != nil && opts.customCodecs[format] != nil {
		return opts.customCodecs[format]
	}

	return opts.builtinCodecs()[format]
}

func (opts *Options) builtinCodecs() map[string]format.Codec {
	return map[string]format.Codec{
		"yaml": format.NewYAMLCodec(),
		"json": format.NewJSONCodec(),
	}
}

func (opts *Options) allowedCodecs() []string {
	allowedCodecs := slices.Collect(maps.Keys(opts.builtinCodecs()))
	for name := range opts.customCodecs {
		allowedCodecs = append(allowedCodecs, name)
	}

	// the allowed codecs are stored in a map: let's sort them to make the
	// return value of this function deterministic
	sort.Strings(allowedCodecs)

	return allowedCodecs
}

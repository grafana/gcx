package output

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/grafana/grafanactl/internal/agent"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/terminal"
	"github.com/spf13/pflag"
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

// applyJSONFlag processes the --json flag value and enforces mutual exclusion
// with -o/--output. It sets JSONFields, JSONDiscovery, or returns an error.
func (opts *Options) applyJSONFlag() error {
	if opts.flags == nil {
		return nil
	}

	jsonFlag := opts.flags.Lookup("json")
	if jsonFlag == nil || !jsonFlag.Changed {
		return nil
	}

	// Enforce mutual exclusion with -o/--output.
	outputFlag := opts.flags.Lookup("output")
	if outputFlag != nil && outputFlag.Changed {
		return errors.New("--json and -o/--output are mutually exclusive: use one or the other, not both")
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

	return codec.Encode(dst, value)
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

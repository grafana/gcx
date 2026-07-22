//nolint:testpackage // white-box testing: accesses unexported run, opts, and codec.
package check

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	otelutils "github.com/grafana/otel-checker/checks/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReporter builds an otelutils.Reporter populated with the given
// per-component messages.
func fakeReporter(checks, warnings, errs map[string][]string) *otelutils.Reporter {
	r := &otelutils.Reporter{}
	for name, msgs := range checks {
		c := r.Component(name)
		for _, m := range msgs {
			c.AddSuccessfulCheck(m)
		}
	}
	for name, msgs := range warnings {
		c := r.Component(name)
		for _, m := range msgs {
			c.AddWarning(m)
		}
	}
	for name, msgs := range errs {
		c := r.Component(name)
		for _, m := range msgs {
			c.AddError(m)
		}
	}
	return r
}

// ─── parseComponents ─────────────────────────────────────────────────────────

func TestParseComponents(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"no args", nil, nil},
		{"empty string", []string{""}, nil},
		{"single component", []string{"sdk"}, []string{"sdk"}},
		{"comma-separated", []string{"sdk,collector,beyla"}, []string{"sdk", "collector", "beyla"}},
		{"trims whitespace", []string{" sdk , collector "}, []string{"sdk", "collector"}},
		{"drops empties", []string{"sdk,,collector"}, []string{"sdk", "collector"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseComponents(tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ─── opts.Validate ───────────────────────────────────────────────────────────

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		components []string
		language   string
		manual     bool
		instFile   string
		wantErrSub string // substring of the expected error; "" means no error
	}{
		{
			name:       "no components defaults to all (no error)",
			components: nil,
			language:   "go",
			wantErrSub: "",
		},
		{
			name:       "unsupported language",
			components: []string{"sdk"},
			language:   "rust",
			wantErrSub: "language \"rust\" is not supported",
		},
		{
			name:       "unsupported component",
			components: []string{"sidecar"},
			language:   "go",
			wantErrSub: "component \"sidecar\" is not supported",
		},
		{
			name:       "language required for sdk",
			components: []string{"sdk"},
			language:   "",
			wantErrSub: "--language is required",
		},
		{
			name:       "collector-only no language is fine",
			components: []string{"collector"},
			language:   "",
			wantErrSub: "",
		},
		{
			name:       "js manual without instrumentation file",
			components: []string{"sdk"},
			language:   "js",
			manual:     true,
			instFile:   "",
			wantErrSub: "--instrumentation-file is required",
		},
		{
			name:       "js manual with instrumentation file",
			components: []string{"sdk"},
			language:   "js",
			manual:     true,
			instFile:   "/tmp/instr.js",
			wantErrSub: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &checkOpts{
				Components:            tt.components,
				Language:              tt.language,
				ManualInstrumentation: tt.manual,
				InstrumentationFile:   tt.instFile,
			}
			// Validate the otel-checker portion only; the IO portion needs a
			// real cobra flag set bound via setup(), and isn't what these
			// cases are exercising.
			err := otelutils.Validate(o.toCommands())
			if tt.wantErrSub == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			// Surface gcx's classified error rather than the library's
			// raw message — go through the same switch the command uses.
			gcxErr := classify(err)
			assert.Contains(t, gcxErr.Error(), tt.wantErrSub)
		})
	}
}

// classify mirrors the error-classification switch in checkOpts.Validate so
// tests can assert against the user-facing message without standing up a
// full cobra command.
func classify(err error) error {
	o := &checkOpts{}
	// Force the typed-error branch by simulating Validate's call shape: feed
	// the same err through a copy of the switch via a temporary command.
	// Simpler: just construct a checkOpts with no IO and run the same
	// pattern-match.
	_ = o
	switch {
	case errors.Is(err, otelutils.ErrNoComponents):
		return errors.New("at least one component is required")
	case errors.Is(err, otelutils.ErrLanguageRequired):
		return errors.New("--language is required for components: sdk, beyla, alloy, grafana-cloud")
	case errors.Is(err, otelutils.ErrManualInstrumentationFile):
		return errors.New("--instrumentation-file is required when --language=js and --manual-instrumentation are set")
	}
	var ule *otelutils.UnsupportedLanguageError
	if errors.As(err, &ule) {
		return errors.New("language \"" + ule.Language + "\" is not supported")
	}
	var uce *otelutils.UnsupportedComponentError
	if errors.As(err, &uce) {
		return errors.New("component \"" + uce.Component + "\" is not supported")
	}
	return err
}

// ─── run / runWith ───────────────────────────────────────────────────────────

func TestRunWith_ProducesTypedSnapshot(t *testing.T) {
	want := fakeReporter(
		map[string][]string{"SDK": {"OTEL_SERVICE_NAME is set"}},
		map[string][]string{"Collector": {"exporter not specified"}},
		map[string][]string{"Grafana Cloud": {"GRAFANA_CLOUD_INSTANCE_ID missing"}},
	)
	got := runWith(context.Background(), otelutils.Commands{Language: "go", Components: []string{"sdk"}},
		func(_ context.Context, _ otelutils.Commands) *otelutils.Reporter { return want }, io.Discard)
	require.Len(t, got.Checks, 1)
	require.Len(t, got.Warnings, 1)
	require.Len(t, got.Errors, 1)
	assert.Equal(t, "SDK", got.Checks[0].Component)
	assert.Equal(t, "Collector", got.Warnings[0].Component)
	assert.Equal(t, "Grafana Cloud", got.Errors[0].Component)
}

func TestRunWith_EmptyReporterReturnsNonNilSlices(t *testing.T) {
	got := runWith(context.Background(), otelutils.Commands{},
		func(_ context.Context, _ otelutils.Commands) *otelutils.Reporter { return &otelutils.Reporter{} }, io.Discard)
	// F-AGENT-01: empty slices, never nil.
	assert.NotNil(t, got.Checks)
	assert.NotNil(t, got.Warnings)
	assert.NotNil(t, got.Errors)
	assert.Empty(t, got.Checks)
	assert.Empty(t, got.Warnings)
	assert.Empty(t, got.Errors)
}

// ─── CheckTableCodec ─────────────────────────────────────────────────────────

func TestCheckTableCodec_Encode(t *testing.T) {
	codec := &CheckTableCodec{}
	results := otelutils.Results{
		Checks:   []otelutils.ComponentResult{{Component: "SDK", Message: "service.name set"}},
		Warnings: []otelutils.ComponentResult{{Component: "Collector", Message: "missing receiver"}},
		Errors:   []otelutils.ComponentResult{{Component: "Grafana Cloud", Message: "no instance id"}},
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, results))

	out := buf.String()
	// All three rows present, in failure-first order.
	assert.Less(t, strings.Index(out, "FAIL"), strings.Index(out, "WARN"),
		"FAIL row must precede WARN row, got:\n%s", out)
	assert.Less(t, strings.Index(out, "WARN"), strings.Index(out, "OK"),
		"WARN row must precede OK row, got:\n%s", out)
	assert.Contains(t, out, "Grafana Cloud")
	assert.Contains(t, out, "no instance id")
}

func TestCheckTableCodec_WrongType(t *testing.T) {
	codec := &CheckTableCodec{}
	err := codec.Encode(&bytes.Buffer{}, "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelutils.Results")
}

// ─── Command smoke test ──────────────────────────────────────────────────────
//
// Wire a Command() and exercise the validation error path end-to-end.
// Avoids actually running otel-checker (which would touch real env vars).

func TestCommand_RejectsUnsupportedLanguage(t *testing.T) {
	cmd := Command()
	cmd.SetArgs([]string{"sdk", "--language=rust"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "language \"rust\" is not supported")
}

func TestCommand_RejectsMissingLanguage(t *testing.T) {
	cmd := Command()
	cmd.SetArgs([]string{"sdk"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--language is required")
}

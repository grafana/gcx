package linter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/linter"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type testOpts struct {
	IO cmdio.Options

	debug      bool
	bundleMode bool
	coverage   bool
	runRegex   string
	timeout    time.Duration
	ignore     []string

	// flags is the bound flag set, kept so the run path can detect --json/
	// --jq (which require the re-encoded report, not the verbatim OPA one).
	flags *pflag.FlagSet
}

// opaRenderedCodec is a menu/validation placeholder for the formats rendered
// directly by the OPA test reporters ("pretty", and "json" shadowing the
// builtin). Registering it keeps the -o menu and Options.Validate honest;
// the actual bytes are written by tester.PrettyReporter/JSONReporter in
// runLintTests. Encode is normally short-circuited by the shared --json/--jq
// interception in Options.Encode; fallback covers degenerate flag values
// (e.g. `--json ,`) that fall through to the codec itself.
type opaRenderedCodec struct {
	name     format.Format
	fallback format.Codec
}

func (c *opaRenderedCodec) Format() format.Format { return c.name }

func (c *opaRenderedCodec) Decode(io.Reader, any) error {
	return errors.New("test report codec does not support decoding")
}

func (c *opaRenderedCodec) Encode(w io.Writer, value any) error {
	if c.fallback != nil {
		return c.fallback.Encode(w, value) //nolint:wrapcheck
	}
	return errors.New("internal error: test report output is rendered by the OPA reporter, not the codec system")
}

func (opts *testOpts) setup(flags *pflag.FlagSet) {
	// "pretty" and "json" are rendered directly by the OPA test reporters
	// (byte-identical to the pre-codec output); "agents" (the agent-mode
	// default) and "yaml" re-encode the JSON report through the codec system
	// so agents get exactly one machine-readable document on stdout.
	opts.IO.RegisterCustomCodec("pretty", &opaRenderedCodec{name: "pretty"})
	opts.IO.RegisterCustomCodec("json", &opaRenderedCodec{name: format.JSON, fallback: format.NewJSONCodec()})
	opts.IO.DefaultFormat("pretty")
	opts.IO.BindFlags(flags)
	opts.flags = flags

	flags.BoolVar(&opts.debug, "debug", false, "Enable debug mode")
	flags.BoolVar(&opts.bundleMode, "bundle", false, "Enable bundle mode")
	flags.BoolVar(&opts.coverage, "coverage", false, "Report coverage")
	flags.DurationVar(&opts.timeout, "timeout", 0, "Set test timeout")
	flags.StringVar(&opts.runRegex, "run", "", "Run only test cases matching the regular expression")
	flags.StringSliceVar(&opts.ignore, "ignore", nil, "File and directory names to ignore during loading (e.g., '.*' excludes hidden files)")
}

// toOptions builds the internal runner options with an explicit OPA output
// format ("pretty" or "json") — the runner only understands those two; the
// cmd layer maps the resolved -o format onto them.
func (opts *testOpts) toOptions(outputFormat string) linter.TestsOptions {
	return linter.TestsOptions{
		OutputFormat: outputFormat,
		Debug:        opts.debug,
		BundleMode:   opts.bundleMode,
		Coverage:     opts.coverage,
		RunRegex:     opts.runRegex,
		Timeout:      opts.timeout,
		Ignore:       opts.ignore,
	}
}

// flagChanged reports whether the named flag was explicitly set.
func (opts *testOpts) flagChanged(name string) bool {
	if opts.flags == nil {
		return false
	}
	f := opts.flags.Lookup(name)
	return f != nil && f.Changed
}

func testCmd() *cobra.Command {
	opts := testOpts{}

	cmd := &cobra.Command{
		Use:   "test PATH...",
		Short: "Run linter rule tests",
		Long:  "Run test suites for linter rules. Each rule directory should contain test fixtures and expected output files. Reports pass/fail for each test case.",
		Example: `
	# Run all tests in a directory:

	gcx dev lint test ./internal/linter/bundle/gcx/
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			return runLintTests(cmd, args, &opts)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func runLintTests(cmd *cobra.Command, args []string, opts *testOpts) error {
	runner := linter.TestsRunner{}

	// "pretty" and explicit "json" write the OPA reporter output verbatim —
	// byte-identical to the pre-codec behavior. --json/--jq force the
	// re-encoded path because field selection and jq transformation only
	// exist in the codec system.
	direct := (opts.IO.OutputFormat == "pretty" || opts.IO.OutputFormat == "json") &&
		!opts.flagChanged("json") && !opts.flagChanged("jq")

	if direct {
		err := runner.Run(cmd.Context(), cmd.OutOrStdout(), args, opts.toOptions(opts.IO.OutputFormat))
		if errors.Is(err, linter.ErrTestsFailed) {
			// The full report (with FAIL entries) is already on stdout —
			// EmittedError carries the failure exit code without a second
			// error document.
			return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, err)
		}
		return err
	}

	// agents/yaml/--json/--jq: run the OPA JSON reporter into a buffer and
	// re-encode the report through the codec system so agent mode gets
	// exactly one compact JSON value on stdout (with spill handling), -o yaml
	// works, and --json/--jq apply. Load/compile errors surface before
	// anything is written to stdout.
	var buf bytes.Buffer
	runErr := runner.Run(cmd.Context(), &buf, args, opts.toOptions("json"))
	if runErr != nil && !errors.Is(runErr, linter.ErrTestsFailed) {
		return runErr
	}

	var report any
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		return fmt.Errorf("decoding test report: %w", err)
	}

	if err := opts.IO.Encode(cmd.OutOrStdout(), report); err != nil {
		return err
	}

	if runErr != nil {
		// The complete report document is already on stdout.
		return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, runErr)
	}
	return nil
}

package linter

import (
	"time"

	"github.com/grafana/gcx/internal/linter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type testOpts struct {
	outputFormat string
	debug        bool
	bundleMode   bool
	coverage     bool
	runRegex     string
	timeout      time.Duration
	ignore       []string
}

func (opts *testOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.outputFormat, "output", "o", "pretty", "Output format. One of: json, pretty")
	flags.BoolVar(&opts.debug, "debug", false, "Enable debug mode")
	flags.BoolVar(&opts.bundleMode, "bundle", false, "Enable bundle mode")
	flags.BoolVar(&opts.coverage, "coverage", false, "Report coverage")
	flags.DurationVar(&opts.timeout, "timeout", 0, "Set test timeout")
	flags.StringVar(&opts.runRegex, "run", "", "Run only test cases matching the regular expression")
	flags.StringSliceVar(&opts.ignore, "ignore", nil, "File and directory names to ignore during loading (e.g., '.*' excludes hidden files)")
}

func (opts *testOpts) toOptions() linter.TestsOptions {
	return linter.TestsOptions{
		OutputFormat: opts.outputFormat,
		Debug:        opts.debug,
		BundleMode:   opts.bundleMode,
		Coverage:     opts.coverage,
		RunRegex:     opts.runRegex,
		Timeout:      opts.timeout,
		Ignore:       opts.ignore,
	}
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
			runner := linter.TestsRunner{}

			return runner.Run(cmd.Context(), cmd.OutOrStdout(), args, opts.toOptions())
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

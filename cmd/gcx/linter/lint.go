package linter

import (
	"context"
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/linter"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type lintOpts struct {
	IO cmdio.Options

	debug         bool
	rules         []string
	maxConcurrent int

	disableAll         bool
	disabledResources  []string
	disabledCategories []string
	disabledRules      []string
	enableAll          bool
	enabledResources   []string
	enabledCategories  []string
	enabledRules       []string
}

func (opts *lintOpts) validate(args []string) error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	if len(args) == 0 {
		return errors.New("at least one file or directory must be provided for linting")
	}

	if opts.maxConcurrent < 1 {
		return errors.New("max-concurrent must be greater than zero")
	}

	return nil
}

func (opts *lintOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("pretty", &reporterCodec{reporter: linter.PrettyReporter{}})
	opts.IO.RegisterCustomCodec("compact", &reporterCodec{reporter: linter.CompactReporter{}})
	opts.IO.DefaultFormat("pretty")

	opts.IO.BindFlags(flags)

	flags.BoolVar(&opts.debug, "debug", false, "Enable debug mode")
	flags.StringArrayVarP(&opts.rules, "rules", "r", nil, "Path to custom rules")
	flags.IntVar(&opts.maxConcurrent, "max-concurrent", 10, "Maximum number of concurrent operations")

	flags.BoolVar(&opts.disableAll, "disable-all", false, "Disable all rules")
	flags.StringArrayVar(&opts.disabledResources, "disable-resource", nil, "Disable all rules for a resource type")
	flags.StringArrayVar(&opts.disabledCategories, "disable-category", nil, "Disable all rules in a category")
	flags.StringArrayVar(&opts.disabledRules, "disable", nil, "Disable a rule")

	flags.BoolVar(&opts.enableAll, "enable-all", false, "Enable all rules")
	flags.StringArrayVar(&opts.enabledResources, "enable-resource", nil, "Enable all rules for a resource type")
	flags.StringArrayVar(&opts.enabledCategories, "enable-category", nil, "Enable all rules in a category")
	flags.StringArrayVar(&opts.enabledRules, "enable", nil, "Enable a rule")
}

func lintCmd() *cobra.Command {
	opts := lintOpts{}

	cmd := &cobra.Command{
		Use:   "run PATH...",
		Short: "Lint Grafana resources",
		Long:  "Lint Grafana resources against Rego-based rules. Checks dashboards and other resource types for best practices, naming conventions, and configuration issues. Use --rules to load custom rules alongside built-in ones.",
		Args:  cobra.MinimumNArgs(1),
		Example: `
	# Lint Grafana resources using builtin rules:

	gcx dev lint run ./resources

	# Lint specific files:

	gcx dev lint run ./resources/file.json ./resources/other.yaml

	# Display compact results:

	gcx dev lint run ./resources -o compact

	# Use custom rules:

	gcx dev lint run --rules ./custom-rules ./resources

	# Disable all rules for a resource type:

	gcx dev lint run --disable-resource dashboard ./resources

	# Disable all rules in a category:

	gcx dev lint run --disable-category idiomatic ./resources

	# Disable specific rules:

	gcx dev lint run --disable uneditable-dashboard --disable panel-title-description ./resources

	# Enable rules for specific resource types:

	gcx dev lint run --disable-all --enable-resource dashboard ./resources

	# Enable only some categories:

	gcx dev lint run --disable-all --enable-category idiomatic ./resources

	# Enable only specific rules:

	gcx dev lint run --disable-all --enable uneditable-dashboard ./resources
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(args); err != nil {
				return err
			}
			return lint(cmd, args, opts)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

func lint(cmd *cobra.Command, inputPaths []string, opts lintOpts) error {
	linterOpts := []linter.Option{
		linter.InputPaths(inputPaths),
		linter.WithCustomRules(opts.rules),
		linter.MaxConcurrency(opts.maxConcurrent),
		linter.ResourceReader(&local.FSReader{
			Decoders:           format.Codecs(),
			MaxConcurrentReads: opts.maxConcurrent,
			StopOnError:        false,
		}),
	}

	if opts.disableAll {
		linterOpts = append(linterOpts, linter.DisableAll())
	}
	if len(opts.disabledResources) != 0 {
		linterOpts = append(linterOpts, linter.DisabledResources(opts.disabledResources))
	}
	if len(opts.disabledCategories) != 0 {
		linterOpts = append(linterOpts, linter.DisabledCategories(opts.disabledCategories))
	}
	if len(opts.disabledRules) != 0 {
		linterOpts = append(linterOpts, linter.DisabledRules(opts.disabledRules))
	}
	if opts.enableAll {
		linterOpts = append(linterOpts, linter.EnableAll())
	}
	if len(opts.enabledResources) != 0 {
		linterOpts = append(linterOpts, linter.EnabledResources(opts.enabledResources))
	}
	if len(opts.enabledCategories) != 0 {
		linterOpts = append(linterOpts, linter.EnabledCategories(opts.enabledCategories))
	}
	if len(opts.enabledRules) != 0 {
		linterOpts = append(linterOpts, linter.EnabledRules(opts.enabledRules))
	}

	if opts.debug {
		linterOpts = append(linterOpts, linter.Debug(cmd.ErrOrStderr()))
	}

	engine, err := linter.New(linterOpts...)
	if err != nil {
		return err
	}

	report, err := engine.Lint(cmd.Context())
	if err != nil {
		return err
	}

	return opts.IO.Encode(cmd.OutOrStdout(), report)
}

type reporterCodec struct {
	reporter linter.Reporter
}

func (c *reporterCodec) Encode(output io.Writer, input any) error {
	//nolint:forcetypeassert
	return c.reporter.Publish(context.Background(), output, input.(linter.Report))
}

func (c *reporterCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("not supported")
}

func (c *reporterCodec) Format() format.Format {
	return "reporterCodec"
}

// Package check implements the "gcx instrumentation check" command, a wrapper
// around the otel-checker library that validates OpenTelemetry instrumentation
// for an application: env vars, SDK config, collector config, Beyla/Alloy
// config, and Grafana Cloud connectivity.
package check

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	otelchecks "github.com/grafana/otel-checker/checks"
	otelutils "github.com/grafana/otel-checker/checks/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type checkOpts struct {
	IO                    cmdio.Options
	Language              string
	ManualInstrumentation bool
	InstrumentationFile   string
	PackageJSONPath       string
	CollectorConfigPath   string
	Debug                 bool

	// Components is parsed from the positional argument; empty means "all".
	Components []string
}

func (o *checkOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("table")
	o.IO.RegisterCustomCodec("table", &CheckTableCodec{})
	o.IO.RegisterCustomCodec("wide", &CheckTableCodec{Wide: true})
	o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(otelutils.Results{}))
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Language, "language", "",
		"Application language. Required for sdk, beyla, alloy, grafana-cloud. Possible values: "+
			strings.Join(otelutils.SupportedLanguages, ", "))
	flags.BoolVar(&o.ManualInstrumentation, "manual-instrumentation", false,
		"Application is using manual instrumentation (JS only).")
	flags.StringVar(&o.InstrumentationFile, "instrumentation-file", "",
		"Path to the JS instrumentation file. Required when --language=js and --manual-instrumentation.")
	flags.StringVar(&o.PackageJSONPath, "package-json-path", "",
		"Path to package.json for JS dependency checks.")
	flags.StringVar(&o.CollectorConfigPath, "collector-config-path", "",
		"Path to the OpenTelemetry Collector config file.")
	flags.BoolVar(&o.Debug, "debug", false,
		"Print additional diagnostic output from the checker.")
}

// Validate finalizes opts after flag parsing and runs the otel-checker
// library's own input validation. Pattern-matches Validate's typed/sentinel
// errors so gcx can render its own UI rather than leaking the library's
// error strings.
func (o *checkOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}

	cmd := o.toCommands()
	err := otelutils.Validate(cmd)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, otelutils.ErrNoComponents):
		return errors.New("at least one component is required")
	case errors.Is(err, otelutils.ErrLanguageRequired):
		return fmt.Errorf("--language is required for components: %s",
			strings.Join(otelutils.LanguageRequiredFor, ", "))
	case errors.Is(err, otelutils.ErrManualInstrumentationFile):
		return errors.New("--instrumentation-file is required when --language=js and --manual-instrumentation are set")
	}

	var ule *otelutils.UnsupportedLanguageError
	if errors.As(err, &ule) {
		return fmt.Errorf("language %q is not supported. Possible values: %s",
			ule.Language, strings.Join(otelutils.SupportedLanguages, ", "))
	}
	var uce *otelutils.UnsupportedComponentError
	if errors.As(err, &uce) {
		return fmt.Errorf("component %q is not supported. Possible values: %s",
			uce.Component, strings.Join(otelutils.SupportedComponents, ", "))
	}
	return err
}

// toCommands maps the gcx-side opts into the otel-checker library's input
// shape. When Components is empty, all SupportedComponents are checked.
// WebServer/Listen/Format are intentionally left zero: gcx renders its own
// way and never uses the library's web server.
func (o *checkOpts) toCommands() otelutils.Commands {
	components := o.Components
	if len(components) == 0 {
		components = append([]string(nil), otelutils.SupportedComponents...)
	}
	return otelutils.Commands{
		Language:              o.Language,
		Components:            components,
		ManualInstrumentation: o.ManualInstrumentation,
		InstrumentationFile:   o.InstrumentationFile,
		PackageJsonPath:       o.PackageJSONPath,
		CollectorConfigPath:   o.CollectorConfigPath,
		Debug:                 o.Debug,
	}
}

// Command returns the "gcx instrumentation check" cobra command.
func Command() *cobra.Command {
	return commandWith(otelchecks.Run)
}

// commandWith builds the check command with an injectable checker. Production
// code passes otelchecks.Run via Command; tests inject a fake to drive the
// command end-to-end without touching real env vars.
func commandWith(c checker) *cobra.Command {
	opts := &checkOpts{}

	cmd := &cobra.Command{
		Use:   "check [components]",
		Short: "Validate OpenTelemetry instrumentation for an application",
		Long: `Validate OpenTelemetry instrumentation configuration for an application
running locally.

Checks performed:
  - Common OTEL_* environment variables (resource attributes, exporter, etc.)
  - SDK setup and dependencies for the chosen language
  - OpenTelemetry Collector config file (YAML schema, pipelines, exporters)
  - Grafana Beyla configuration
  - Grafana Alloy configuration
  - Grafana Cloud connectivity (uses env vars for endpoint and credentials)

Components is an optional comma-separated list — defaults to all when omitted.
Supported components: ` + strings.Join(otelutils.SupportedComponents, ", ") + `.

Powered by github.com/grafana/otel-checker.`,
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return otelutils.SupportedComponents, cobra.ShellCompDirectiveNoFileComp
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Components = parseComponents(args)
			if err := opts.Validate(); err != nil {
				return fmt.Errorf("instrumentation check: %w", err)
			}

			results := runWith(cmd.Context(), opts.toCommands(), c, cmd.ErrOrStderr())

			if err := opts.IO.Encode(cmd.OutOrStdout(), results); err != nil {
				return fmt.Errorf("instrumentation check: %w", err)
			}

			if len(results.Errors) > 0 {
				// The result document (with the failing checks enumerated) is
				// already on stdout — EmittedError carries the exit code
				// without a second error document. EmittedError also
				// suppresses reportError's stderr rendering, so the failure
				// count diagnostic the old bare error produced is emitted
				// explicitly here.
				summary := fmt.Sprintf("%d check(s) failed", len(results.Errors))
				cmdio.EmitWarn(cmd.ErrOrStderr(), summary)
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New(summary))
			}
			return nil
		},
	}

	opts.setup(cmd.Flags())

	if err := cmd.RegisterFlagCompletionFunc("language", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return otelutils.SupportedLanguages, cobra.ShellCompDirectiveNoFileComp
	}); err != nil {
		// RegisterFlagCompletionFunc only errors when the flag doesn't
		// exist — impossible here since we just bound it.
		panic(err)
	}

	return cmd
}

// parseComponents splits the single positional argument on commas and trims
// surrounding whitespace. Returns nil for an empty/missing arg so callers can
// distinguish "no components given" from "explicit empty list".
func parseComponents(args []string) []string {
	if len(args) == 0 || args[0] == "" {
		return nil
	}
	parts := strings.Split(args[0], ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

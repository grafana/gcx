package linter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/grafana/gcx/internal/linter/builtins"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/cover"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/tester"
	"github.com/open-policy-agent/opa/v1/topdown"
)

var ErrTestsFailed = errors.New("tests failed")

type TestsOptions struct {
	OutputFormat string
	Debug        bool
	BundleMode   bool
	Coverage     bool
	RunRegex     string
	Timeout      time.Duration
	Ignore       []string
}

//nolint:ireturn
func (opts *TestsOptions) testReporter(stdout io.Writer, cov *cover.Cover, modules map[string]*ast.Module) tester.Reporter {
	if opts.Coverage {
		return tester.JSONCoverageReporter{
			Cover:   cov,
			Modules: modules,
			Output:  stdout,
		}
	}

	if opts.OutputFormat == "json" {
		return tester.JSONReporter{Output: stdout}
	}

	return tester.PrettyReporter{
		Verbose:     opts.Debug,
		Output:      stdout,
		FailureLine: true,
		LocalVars:   true,
	}
}

func (opts *TestsOptions) getTimeout() time.Duration {
	if opts.Timeout != 0 {
		return opts.Timeout
	}

	return 5 * time.Second
}

func (opts *TestsOptions) validate(args []string) error {
	if opts.OutputFormat != "json" && opts.OutputFormat != "pretty" {
		return fmt.Errorf("unknown output format '%s'. Valid formats are: json, pretty", opts.OutputFormat)
	}

	if len(args) == 0 {
		return errors.New("at least one file or directory must be provided")
	}

	return nil
}

type loaderFilter struct {
	Ignore []string
}

func (f loaderFilter) Apply(abspath string, info os.FileInfo, depth int) bool {
	return slices.ContainsFunc(f.Ignore, func(s string) bool {
		return loader.GlobExcludeName(s, 1)(abspath, info, depth)
	})
}

type TestsRunner struct {
}

func (runner *TestsRunner) Run(ctx context.Context, stdout io.Writer, inputPaths []string, opts TestsOptions) error {
	if err := opts.validate(inputPaths); err != nil {
		return err
	}

	filter := loaderFilter{
		Ignore: opts.Ignore,
	}

	var (
		modules map[string]*ast.Module
		bundles map[string]*bundle.Bundle
		store   storage.Store
		err     error
	)

	if opts.BundleMode {
		bundles, err = tester.LoadBundlesWithRegoVersion(inputPaths, filter.Apply, ast.RegoV1)
		store = inmem.NewWithOpts(inmem.OptRoundTripOnWrite(false))
	} else {
		modules, store, err = tester.Load(inputPaths, filter.Apply)
	}
	if err != nil {
		return err
	}

	var cov *cover.Cover
	var coverTracer topdown.QueryTracer
	if opts.Coverage {
		cov = cover.New()
		coverTracer = cov
	}

	builtinFuncs := builtins.Tester()

	capabilities := ast.CapabilitiesForThisVersion()
	for _, f := range builtinFuncs {
		capabilities.Builtins = append(capabilities.Builtins, f.Decl)
	}

	compiler := ast.NewCompiler().
		WithCapabilities(capabilities).
		WithEnablePrintStatements(true).
		WithUseTypeCheckAnnotations(true).
		WithModuleLoader(moduleLoader(&BuiltinBundle))

	r := tester.NewRunner().
		SetCompiler(compiler).
		SetStore(store).
		CapturePrintOutput(true).
		EnableTracing(opts.Debug).
		SetCoverageQueryTracer(coverTracer).
		SetModules(modules).
		SetBundles(bundles).
		SetTimeout(opts.getTimeout()).
		AddCustomBuiltins(builtinFuncs).
		Filter(opts.RunRegex)

	reporter := opts.testReporter(stdout, cov, modules)

	return runTests(ctx, store, r, reporter)
}

func moduleLoader(gcxRules *bundle.Bundle) ast.ModuleLoader {
	// We use the package declarations to know which modules we still need, and return
	// those from the embedded gcxRules bundle.
	extra := map[string]struct{}{}
	for _, mod := range gcxRules.Modules {
		extra[mod.Parsed.Package.Path.String()] = struct{}{}
	}

	return func(present map[string]*ast.Module) (map[string]*ast.Module, error) {
		for _, mod := range present {
			delete(extra, mod.Package.Path.String())
		}

		extraMods := map[string]*ast.Module{}

		for id, mod := range gcxRules.ParsedModules("bundle") {
			if _, ok := extra[mod.Package.Path.String()]; ok {
				extraMods[id] = mod
			}
		}

		return extraMods, nil
	}
}

func runTests(ctx context.Context, store storage.Store, runner *tester.Runner, reporter tester.Reporter) error {
	txn, err := store.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer store.Abort(ctx, txn)

	ch, err := runner.RunTests(ctx, txn)
	if err != nil {
		return err
	}

	hasFailure := false
	dup := make(chan *tester.Result)
	go func() {
		defer close(dup)

		for tr := range ch {
			if tr.Fail {
				hasFailure = true
			}

			tr.Trace = nil
			dup <- tr
		}
	}()

	if err := reporter.Report(dup); err != nil {
		return err
	}

	if !hasFailure {
		return nil
	}

	return ErrTestsFailed
}

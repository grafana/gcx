package services

import (
	"context"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/pflag"
)

// RunList exposes the internal runList function for use in external test packages.
func RunList(
	ctx context.Context,
	opts *ListOpts,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	return runList(ctx, opts, outOpts, client, promHeaders, out)
}

// ListOpts is an alias for listOpts so external tests can construct opts.
type ListOpts = listOpts

// RunGet exposes the internal runGet function for use in external test packages.
func RunGet(
	ctx context.Context,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	cluster, namespace, service string,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	return runGet(ctx, outOpts, client, cluster, namespace, service, promHeaders, out)
}

// NewMutationTestIO builds a cmdio.Options wired the same way the mutation
// commands wire theirs (text codec default, structured value for agents and
// -o json/yaml), for tests that drive the run* functions directly. args are
// optional flag arguments (e.g. "-o", "yaml").
func NewMutationTestIO(tb interface {
	Fatalf(format string, args ...any)
}, args ...string) *cmdio.Options {
	opts := &cmdio.Options{ErrWriter: io.Discard}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	instoutput.BindMutationIO(opts, flags)
	if err := flags.Parse(args); err != nil {
		tb.Fatalf("flag parse error: %v", err)
	}
	if err := opts.Validate(); err != nil {
		tb.Fatalf("options validate error: %v", err)
	}
	return opts
}

// RunInclude exposes the internal runInclude function for use in external test packages.
func RunInclude(
	ctx context.Context,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	return runInclude(ctx, outOpts, client, cluster, namespace, service, urls, promHeaders, out)
}

// RunExclude exposes the internal runExclude function for use in external test packages.
func RunExclude(
	ctx context.Context,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	return runExclude(ctx, outOpts, client, cluster, namespace, service, urls, promHeaders, out)
}

// RunClear exposes the internal runClear function for use in external test packages.
func RunClear(
	ctx context.Context,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	return runClear(ctx, outOpts, client, cluster, namespace, service, urls, promHeaders, out)
}

// ApplyIncludeMutation exposes applyIncludeMutation for unit tests.
func ApplyIncludeMutation(ns instrumentation.App, service string) instrumentation.App {
	return applyIncludeMutation(ns, service)
}

// ApplyExcludeMutation exposes applyExcludeMutation for unit tests.
func ApplyExcludeMutation(ns instrumentation.App, service string) instrumentation.App {
	return applyExcludeMutation(ns, service)
}

// ApplyClearMutation exposes applyClearMutation for unit tests.
func ApplyClearMutation(ns instrumentation.App, service string) instrumentation.App {
	return applyClearMutation(ns, service)
}

// NormalizeStatus exposes normalizeStatus for unit tests.
func NormalizeStatus(s string) instrumentation.InstrumentationStatus {
	return normalizeStatus(s)
}

// ValidateWorkloadExists exposes validateWorkloadExists for unit tests.
func ValidateWorkloadExists(
	ctx context.Context,
	client *instrumentation.Client,
	promHeaders instrumentation.PromHeaders,
	cluster, namespace, service string,
) error {
	return validateWorkloadExists(ctx, client, promHeaders, cluster, namespace, service)
}

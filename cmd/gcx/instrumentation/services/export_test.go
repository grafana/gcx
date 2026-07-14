package services

import (
	"context"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
)

// RunList exposes the internal runList function for use in external test packages.
func RunList(
	ctx context.Context,
	opts *ListOpts,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	out io.Writer,
) error {
	return runList(ctx, opts, outOpts, client, out)
}

// ListOpts is an alias for listOpts so external tests can construct opts.
type ListOpts = listOpts

// RunGet exposes the internal runGet function for use in external test packages.
func RunGet(
	ctx context.Context,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	cluster, namespace, service string,
	out io.Writer,
) error {
	return runGet(ctx, outOpts, client, cluster, namespace, service, out)
}

// RunInclude exposes the internal runInclude function for use in external test packages.
func RunInclude(
	ctx context.Context,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	out io.Writer,
) error {
	return runInclude(ctx, client, cluster, namespace, service, urls, out)
}

// RunExclude exposes the internal runExclude function for use in external test packages.
func RunExclude(
	ctx context.Context,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	out io.Writer,
) error {
	return runExclude(ctx, client, cluster, namespace, service, urls, out)
}

// RunClear exposes the internal runClear function for use in external test packages.
func RunClear(
	ctx context.Context,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	out io.Writer,
) error {
	return runClear(ctx, client, cluster, namespace, service, urls, out)
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
	cluster, namespace, service string,
) error {
	return validateWorkloadExists(ctx, client, cluster, namespace, service)
}

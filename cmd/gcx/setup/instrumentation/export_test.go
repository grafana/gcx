package instrumentation

import (
	"context"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/spf13/cobra"
)

// RunApply exposes the internal runApply function for use in external test packages.
func RunApply(ctx context.Context, opts *applyOpts, client *instrum.Client, urls instrum.BackendURLs, out io.Writer) error {
	return runApply(ctx, opts, client, urls, out)
}

// ApplyOpts is an alias for applyOpts so external tests can construct opts.
type ApplyOpts = applyOpts

// NewDiscoverCommand exposes newDiscoverCommand for use in external test packages.
func NewDiscoverCommand(loader fleet.ConfigLoader) *cobra.Command {
	return newDiscoverCommand(loader)
}

// NewStatusCommand exposes newStatusCommand for use in external test packages.
func NewStatusCommand(loader fleet.ConfigLoader) *cobra.Command {
	return newStatusCommand(loader)
}

// RunShow exposes the internal runShow function for use in external test packages.
func RunShow(ctx context.Context, opts *showOpts, client *instrum.Client, cluster string, out io.Writer) error {
	return runShow(ctx, opts, client, cluster, out)
}

// ShowOpts is an alias for showOpts so external tests can construct opts.
type ShowOpts = showOpts

// NewShowCommand exposes newShowCommand for use in external test packages.
func NewShowCommand(loader fleet.ConfigLoader) *cobra.Command {
	return newShowCommand(loader)
}

package instrumentation

import (
	"context"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/spf13/cobra"
)

// RunApply exposes the internal runApply function for use in external test packages.
func RunApply(ctx context.Context, opts *applyOpts, client *instrum.Client, out io.Writer) error {
	return runApply(ctx, opts, client, out)
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

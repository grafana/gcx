package crudcmd

import (
	"context"
	"fmt"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// DeleteOpts is the standard opts struct for delete commands: a --force flag
// that skips the confirmation prompt.
type DeleteOpts struct {
	Force bool
}

// Setup registers the --force flag.
func (o *DeleteOpts) Setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

// DeleteConfig configures a generic "delete ID..." command: confirm once,
// then delete each argument in turn, reporting success per item.
type DeleteConfig struct {
	// Use and Short populate the cobra command.
	Use, Short string
	// Args validates the positional arguments (e.g. cobra.ExactArgs(1) for
	// single-ID delete commands, cobra.MinimumNArgs(1) for batch delete).
	Args cobra.PositionalArgs

	// Confirm builds the confirmation prompt from the raw args.
	Confirm func(args []string) string

	// NewDelete builds the delete function once per invocation (so it can
	// load config / construct a client a single time), returning a function
	// that deletes a single item by ID.
	NewDelete func(ctx context.Context) (func(id string) error, error)

	// Success builds the success message for a deleted ID.
	Success func(id string) string

	// WrapErr wraps a per-item delete error. Defaults to
	// fmt.Errorf("failed to delete %s: %w", id, err).
	WrapErr func(id string, err error) error

	// Out returns the writer used for the confirmation prompt and success
	// messages. Defaults to cmd.OutOrStdout().
	Out func(cmd *cobra.Command) io.Writer
}

// NewDeleteCommand builds a cobra command implementing the standard
// confirm-then-delete-each-argument flow shared by every provider's delete
// command.
func NewDeleteCommand(cfg DeleteConfig) *cobra.Command {
	opts := &DeleteOpts{}
	cmd := &cobra.Command{
		Use:   cfg.Use,
		Short: cfg.Short,
		Args:  cfg.Args,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if cfg.Out != nil {
				out = cfg.Out(cmd)
			}

			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), out, opts.Force, cfg.Confirm(args))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			ctx := cmd.Context()
			del, err := cfg.NewDelete(ctx)
			if err != nil {
				return err
			}

			wrapErr := cfg.WrapErr
			if wrapErr == nil {
				wrapErr = func(id string, err error) error {
					return fmt.Errorf("failed to delete %s: %w", id, err)
				}
			}

			for _, id := range args {
				if err := del(id); err != nil {
					return wrapErr(id, err)
				}
				cmdio.Success(out, "%s", cfg.Success(id))
			}
			return nil
		},
	}
	opts.Setup(cmd.Flags())
	return cmd
}

package crudcmd

import (
	"context"
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// errFilenameRequired is returned by NewCreateCommand/NewUpdateCommand when
// -f/--filename is empty, matching the message every hand-written
// create/update command used before delegating file parsing to Read.
var errFilenameRequired = errors.New("--filename/-f is required")

// ListConfig configures a generic "list" command for the common case: fetch
// a (possibly limited) slice of T and encode it as-is. Resources whose list
// output diverges by format (e.g. a table view vs. a K8s-envelope
// unstructured view for yaml/json) should call ListOpts directly instead —
// this constructor is for the mechanical "fetch, truncate, encode" shape.
type ListConfig[T any] struct {
	Use, Short   string
	Example      string
	DefaultFmt   string
	LimitDefault int64
	LimitUsage   string
	Codecs       []format.Codec
	// ExtraFlags registers additional resource-specific flags (e.g.
	// --scope). Optional. Flags should bind to variables captured by Fetch's
	// closure.
	ExtraFlags func(flags *pflag.FlagSet)
	// Fetch loads items, applying limit (0 means unlimited) client- or
	// server-side.
	Fetch func(ctx context.Context, limit int64) ([]T, error)
}

// NewListCommand builds a cobra "list" command from cfg.
func NewListCommand[T any](cfg ListConfig[T]) *cobra.Command {
	opts := &ListOpts{}
	cmd := &cobra.Command{
		Use:     cfg.Use,
		Short:   cfg.Short,
		Example: cfg.Example,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			items, err := cfg.Fetch(cmd.Context(), opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.Setup(cmd.Flags(), cfg.DefaultFmt, cfg.LimitDefault, cfg.LimitUsage, cfg.Codecs...)
	if cfg.ExtraFlags != nil {
		cfg.ExtraFlags(cmd.Flags())
	}
	return cmd
}

// GetConfig configures a generic "get" command for the common case: fetch a
// single T (by the raw positional args) and encode it as-is.
type GetConfig[T any] struct {
	Use, Short string
	Long       string
	Example    string
	Args       cobra.PositionalArgs
	DefaultFmt string
	Codecs     []format.Codec
	Fetch      func(ctx context.Context, args []string) (T, error)
}

// NewGetCommand builds a cobra "get" command from cfg.
func NewGetCommand[T any](cfg GetConfig[T]) *cobra.Command {
	opts := &GetOpts{}
	cmd := &cobra.Command{
		Use:     cfg.Use,
		Short:   cfg.Short,
		Long:    cfg.Long,
		Example: cfg.Example,
		Args:    cfg.Args,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			item, err := cfg.Fetch(cmd.Context(), args)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), item)
		},
	}
	opts.Setup(cmd.Flags(), cfg.DefaultFmt, cfg.Codecs...)
	return cmd
}

// CreateConfig configures a generic file-based "create" command: read a T
// from -f/--filename (or stdin), optionally validate it, create it, and
// encode the result.
type CreateConfig[T any] struct {
	Use, Short    string
	Example       string
	DefaultFmt    string
	FilenameUsage string
	// Read parses the input file/stdin into a T. Most resources should pass
	// a closure over ReadYAMLOrJSONFile[T] or ReadJSONOrYAMLFile[T].
	Read func(path string, stdin io.Reader) (*T, error)
	// Validate runs after Read, before Create. Optional.
	Validate func(item *T) error
	Create   func(ctx context.Context, item T) (T, error)
	// OnSuccess runs after a successful Create, before encoding the result.
	// Optional — use it for a "Created X" cmdio.Success message.
	OnSuccess func(cmd *cobra.Command, created T)
}

// NewCreateCommand builds a cobra "create" command from cfg.
func NewCreateCommand[T any](cfg CreateConfig[T]) *cobra.Command {
	opts := &MutateOpts{}
	cmd := &cobra.Command{
		Use:     cfg.Use,
		Short:   cfg.Short,
		Example: cfg.Example,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.File == "" {
				return errFilenameRequired
			}
			item, err := cfg.Read(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if cfg.Validate != nil {
				if err := cfg.Validate(item); err != nil {
					return err
				}
			}
			created, err := cfg.Create(cmd.Context(), *item)
			if err != nil {
				return err
			}
			if cfg.OnSuccess != nil {
				cfg.OnSuccess(cmd, created)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.Setup(cmd.Flags(), cfg.DefaultFmt, cfg.FilenameUsage)
	return cmd
}

// UpdateConfig configures a generic file-based "update ID" command: read a T
// from -f/--filename (or stdin), optionally validate it, update it by the
// first positional argument, and encode the result.
type UpdateConfig[T any] struct {
	Use, Short    string
	Example       string
	Args          cobra.PositionalArgs
	DefaultFmt    string
	FilenameUsage string
	Read          func(path string, stdin io.Reader) (*T, error)
	Validate      func(item *T) error
	Update        func(ctx context.Context, id string, item T) (T, error)
	// OnSuccess runs after a successful Update, before encoding the result.
	// Optional — use it for an "Updated X" cmdio.Success message.
	OnSuccess func(cmd *cobra.Command, updated T)
}

// NewUpdateCommand builds a cobra "update ID" command from cfg.
func NewUpdateCommand[T any](cfg UpdateConfig[T]) *cobra.Command {
	opts := &MutateOpts{}
	cmd := &cobra.Command{
		Use:     cfg.Use,
		Short:   cfg.Short,
		Example: cfg.Example,
		Args:    cfg.Args,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.File == "" {
				return errFilenameRequired
			}
			item, err := cfg.Read(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if cfg.Validate != nil {
				if err := cfg.Validate(item); err != nil {
					return err
				}
			}
			updated, err := cfg.Update(cmd.Context(), args[0], *item)
			if err != nil {
				return err
			}
			if cfg.OnSuccess != nil {
				cfg.OnSuccess(cmd, updated)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.Setup(cmd.Flags(), cfg.DefaultFmt, cfg.FilenameUsage)
	return cmd
}

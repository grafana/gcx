//go:build !wasip1

// Package ext wires `gcx ext`: installing, updating and running extensions.
package ext

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/extensions"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
)

const disclosure = "Extensions are third-party programs that run with your permissions and have the same access as you, including to the credentials gcx has saved. Grafana Labs does not review or support them. Install only extensions you trust."

// Command returns the ext command group.
func Command() *cobra.Command {
	opts := &cmdconfig.Options{}
	cmd := &cobra.Command{
		Use:   "ext [NAME] [ARGS]...",
		Short: "Install and run gcx extensions",
		Long: disclosure + "\n\n" +
			"Run an installed extension with 'gcx ext NAME [ARGS]...'. gcx flags go before NAME; every argument after NAME is passed to the extension unchanged. " +
			"Commands the extension runs use the same context, configuration file and agent mode unless the extension overrides them.",
		Example: `  gcx ext install https://github.com/example/gcx-hello.git
  gcx ext list
  gcx ext hello --name world
  gcx --context prod ext hello`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return run(cmd, opts, args[0], args[1:])
		},
	}
	// Stop parsing flags at the extension name so the rest reach it unchanged.
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVar(&opts.ConfigFile, "config", "", "Path to the configuration file to use")

	cmd.AddCommand(installCommand(), listCommand(), uninstallCommand(), updateCommand())
	return cmd
}

func newStore() *extensions.Store {
	return &extensions.Store{
		Dir:        extensions.DefaultDir(),
		GCXVersion: version.Get(),
		HTTPClient: httputils.NewClient(httputils.ClientOpts{Timeout: 10 * time.Minute}),
	}
}

func run(cmd *cobra.Command, opts *cmdconfig.Options, name string, args []string) error {
	ext, err := newStore().Get(name)
	if errors.Is(err, extensions.ErrNotInstalled) {
		usage := gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf("unknown extension %q", name), err)
		usage.Suggestions = append(usage.Suggestions, "Run 'gcx ext list' to see installed extensions")
		return usage
	}
	if err != nil {
		return err
	}
	gcx, err := os.Executable()
	if err != nil {
		return err
	}
	code, err := ext.Run(cmd.Context(), extensions.RunOptions{
		Args:       args,
		GCX:        gcx,
		Context:    config.ContextNameFromCtx(cmd.Context()),
		ConfigFile: opts.ConfigFile,
		Stdout:     cmd.OutOrStdout(),
		Stderr:     cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return gcxerrors.NewAlreadyReportedError(code)
	}
	return nil
}

func installCommand() *cobra.Command {
	out := cmdio.Options{}
	var force bool
	cmd := &cobra.Command{
		Use:   "install SOURCE",
		Short: "Install an extension from a git repository or local directory",
		Long: disclosure + "\n\n" +
			"SOURCE is a git URL (https, ssh or file) or a local directory containing " + extensions.ManifestFile + ". " +
			"A repository is installed at its latest semantic version tag; the release for this OS and architecture is downloaded and its checksum verified. " +
			"A local directory is installed in place, so rebuilding its executable needs no reinstall. " +
			"In agent mode, --force is required.",
		Example: `  gcx ext install https://github.com/example/gcx-hello.git
  gcx ext install git@github.com:example/gcx-hello.git
  gcx ext install .
  gcx --agent ext install --force https://github.com/example/gcx-hello.git`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := out.Validate(); err != nil {
				return err
			}
			// An agent could be talked into installing an extension, which then runs
			// as the user, so it must confirm the install explicitly.
			if _, err := providers.CheckDestructiveBypass(force); errors.Is(err, providers.ErrAgentModeRequiresForce) {
				usage := gcxerrors.NewCommandUsageError(cmd, "installing an extension in agent mode requires --force", err)
				usage.Suggestions = append(usage.Suggestions, "Install only extensions the user asked for, then re-run with --force")
				return usage
			} else if err != nil {
				return err
			}
			ext, err := newStore().Install(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			cmdio.EmitWarn(cmd.ErrOrStderr(), disclosure)
			return out.Encode(cmd.OutOrStdout(), ext)
		},
	}
	bindIO(cmd, &out, textCodec(func(w io.Writer, ext *extensions.Extension) error {
		_, err := fmt.Fprintf(w, "Installed %s %s from %s\n", ext.Name, ext.Version, ext.Source)
		return err
	}))
	cmd.Flags().BoolVar(&force, "force", false, "Install in agent mode, where installing requires explicit confirmation")
	return cmd
}

func listCommand() *cobra.Command {
	out := cmdio.Options{}
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List installed extensions",
		Example: `  gcx ext list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := out.Validate(); err != nil {
				return err
			}
			exts, err := newStore().List()
			if err != nil {
				return err
			}
			return out.Encode(cmd.OutOrStdout(), exts)
		},
	}
	bindIO(cmd, &out, textCodec(func(w io.Writer, exts []extensions.Extension) error {
		t := style.NewTable("NAME", "VERSION", "SOURCE", "DESCRIPTION")
		for _, ext := range exts {
			t.Row(ext.Name, ext.Version, ext.Source, ext.Description)
		}
		return t.Render(w)
	}))
	return cmd
}

func uninstallCommand() *cobra.Command {
	out := cmdio.Options{}
	cmd := &cobra.Command{
		Use:     "uninstall NAME",
		Short:   "Uninstall an extension",
		Long:    "Uninstall an extension. The source directory of a local install is left untouched.",
		Example: `  gcx ext uninstall hello`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := out.Validate(); err != nil {
				return err
			}
			ext, err := newStore().Uninstall(args[0])
			if err != nil {
				return err
			}
			return out.Encode(cmd.OutOrStdout(), ext)
		},
	}
	bindIO(cmd, &out, textCodec(func(w io.Writer, ext *extensions.Extension) error {
		_, err := fmt.Fprintf(w, "Uninstalled %s\n", ext.Name)
		return err
	}))
	return cmd
}

func updateCommand() *cobra.Command {
	out := cmdio.Options{}
	cmd := &cobra.Command{
		Use:   "update [NAME]...",
		Short: "Update extensions to their latest release",
		Long: "Update the named extensions, or all of them, to the latest semantic version tag of the repository they were installed from. " +
			"Local installs are skipped: rebuild their executable instead. A failure to update one extension does not stop the others and exits with code 4.",
		Example: `  gcx ext update
  gcx ext update hello`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := out.Validate(); err != nil {
				return err
			}
			results, err := newStore().Update(cmd.Context(), args)
			if err != nil {
				return err
			}
			if err := out.Encode(cmd.OutOrStdout(), results); err != nil {
				return err
			}
			failed := 0
			for _, r := range results {
				if r.Status == extensions.StatusFailed {
					failed++
				}
			}
			if failed > 0 {
				// The results document is already on stdout; exit 4 reports the failures.
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure,
					gcxerrors.NewPartialFailureError("update", len(results), failed))
			}
			return nil
		},
	}
	bindIO(cmd, &out, textCodec(func(w io.Writer, results []extensions.UpdateResult) error {
		t := style.NewTable("NAME", "FROM", "TO", "STATUS", "ERROR")
		for _, r := range results {
			t.Row(r.Name, r.From, r.To, r.Status, r.Error)
		}
		return t.Render(w)
	}))
	return cmd
}

func bindIO(cmd *cobra.Command, opts *cmdio.Options, text format.Codec) {
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", text)
	opts.BindFlags(cmd.Flags())
}

// textCodec adapts a render function for one result type to a text codec.
func textCodec[T any](render func(io.Writer, T) error) format.Codec {
	return &typedTextCodec[T]{render: render}
}

type typedTextCodec[T any] struct {
	render func(io.Writer, T) error
}

func (c *typedTextCodec[T]) Format() format.Format { return "text" }

func (c *typedTextCodec[T]) Encode(w io.Writer, value any) error {
	v, ok := value.(T)
	if !ok {
		return fmt.Errorf("text codec: unsupported value %T", value)
	}
	return c.render(w, v)
}

func (c *typedTextCodec[T]) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

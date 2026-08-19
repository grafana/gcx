package ext

import (
	"errors"
	"fmt"
	goio "io"
	"os"

	"github.com/grafana/gcx/internal/extensions"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	appversion "github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
)

func newInstallCommand() *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &installTextCodec{})

	cmd := &cobra.Command{
		Use:   "install <source>",
		Short: "Install an extension from a local path, manifest URL, or git URL",
		Long: "Install a third-party extension.\n\n" +
			"gcx does not audit extensions. Installing one downloads and runs code " +
			"published by a third party with your full user permissions: review the " +
			"source and its publisher first.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			store, err := extensions.DefaultStore()
			if err != nil {
				return err
			}
			installed, err := store.Install(cmd.Context(), extensions.InstallOptions{
				Source:     args[0],
				GCXVersion: appversion.Get(),
				Progress:   cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), installed)
		},
	}
	opts.BindFlags(cmd.Flags())
	return cmd
}

func newListCommand() *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &listTextCodec{})

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed extensions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			store, err := extensions.DefaultStore()
			if err != nil {
				return err
			}
			items, err := store.List()
			if err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.BindFlags(cmd.Flags())
	return cmd
}

func newUninstallCommand() *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &uninstallTextCodec{})

	cmd := &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Remove an installed extension",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			store, err := extensions.DefaultStore()
			if err != nil {
				return err
			}
			if err := store.Uninstall(args[0]); err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), uninstallResult{Name: args[0], Uninstalled: true})
		},
	}
	opts.BindFlags(cmd.Flags())
	return cmd
}

// uninstallResult keeps `ext uninstall` inside the agent output contract: one
// JSON value on stdout rather than a bare human sentence.
type uninstallResult struct {
	Name        string `json:"name"`
	Uninstalled bool   `json:"uninstalled"`
}

type uninstallTextCodec struct{}

func (c *uninstallTextCodec) Format() format.Format { return "text" }

func (c *uninstallTextCodec) Encode(output goio.Writer, v any) error {
	r, ok := v.(uninstallResult)
	if !ok {
		return fmt.Errorf("unexpected payload %T", v)
	}
	fmt.Fprintf(output, "Uninstalled %s\n", r.Name)
	return nil
}

func (c *uninstallTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("ext uninstall text codec does not support decoding")
}

func newUpdateCommand() *cobra.Command {
	all := false
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &listTextCodec{})

	cmd := &cobra.Command{
		Use:   "update [name]",
		Short: "Reinstall extensions from the source they were installed from",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if len(args) == 0 && !all {
				return errors.New("specify an extension name or --all")
			}
			store, err := extensions.DefaultStore()
			if err != nil {
				return err
			}
			targets, err := store.List()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				one, err := store.Lookup(args[0])
				if err != nil {
					return err
				}
				targets = []extensions.Installed{*one}
			}
			updated := make([]extensions.Installed, 0, len(targets))
			for _, t := range targets {
				got, err := store.Install(cmd.Context(), extensions.InstallOptions{
					Source:     t.Source,
					GCXVersion: appversion.Get(),
					Progress:   cmd.ErrOrStderr(),
				})
				if err != nil {
					return fmt.Errorf("updating %s: %w", t.Name, err)
				}
				updated = append(updated, *got)
			}
			return opts.Encode(cmd.OutOrStdout(), updated)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Update every installed extension")
	opts.BindFlags(cmd.Flags())
	return cmd
}

type listTextCodec struct{}

func (c *listTextCodec) Format() format.Format { return "text" }

func (c *listTextCodec) Encode(output goio.Writer, v any) error {
	items, ok := v.([]extensions.Installed)
	if !ok {
		return fmt.Errorf("unexpected payload %T", v)
	}
	if len(items) == 0 {
		fmt.Fprintln(output, "No extensions installed. Install one with 'gcx ext install <source>'.")
		return nil
	}
	t := style.NewTable("NAME", "VERSION", "SOURCE", "DESCRIPTION")
	for _, e := range items {
		t.Row(e.Name, e.Version, e.Source, e.Description)
	}
	return t.Render(output)
}

func (c *listTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("ext list text codec does not support decoding")
}

type installTextCodec struct{}

func (c *installTextCodec) Format() format.Format { return "text" }

func (c *installTextCodec) Encode(output goio.Writer, v any) error {
	e, ok := v.(*extensions.Installed)
	if !ok {
		return fmt.Errorf("unexpected payload %T", v)
	}
	fmt.Fprintf(output, "Installed %s %s\n", e.Name, e.Version)
	fmt.Fprintf(output, "Run it with: gcx ext %s --help\n", e.Name)
	if _, err := os.Stat(e.Entrypoint); err == nil {
		fmt.Fprintf(output, "Entrypoint:  %s\n", e.Entrypoint)
	}
	return nil
}

func (c *installTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("ext install text codec does not support decoding")
}

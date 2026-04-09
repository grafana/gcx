package templates

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := sigilhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the templates command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "Browse reusable evaluator blueprints (global and tenant-scoped).",
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newVersionsCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Scope string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Scope, "scope", "", `Filter by scope: "global" or "tenant"`)
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List eval templates.",
		Example: `  # List all templates.
  gcx sigil templates list

  # Filter by scope.
  gcx sigil templates list --scope global`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			templates, err := client.List(cmd.Context(), opts.Scope)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), templates)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <template-id>",
		Short: "Get a single eval template.",
		Long: `Get the full template definition including config and output keys.

Templates are reusable evaluator blueprints. Export a template as YAML,
customize it, and create an evaluator with 'evaluators create -f'.`,
		Example: `  # Get a template's config and output keys.
  gcx sigil templates get my-template -o yaml > evaluator.yaml
  gcx sigil evaluators create -f evaluator.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			detail, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), detail)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- versions ---

type versionsOpts struct {
	IO cmdio.Options
}

func (o *versionsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &VersionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionsOpts{}
	cmd := &cobra.Command{
		Use:   "versions <template-id>",
		Short: "List version history for an eval template.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			versions, err := client.ListVersions(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), versions)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codecs ---

// TableCodec renders template list as a text table.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	templates, ok := v.([]eval.TemplateDefinition)
	if !ok {
		return errors.New("invalid data type for table codec: expected []TemplateDefinition")
	}

	var tb *style.TableBuilder
	if c.Wide {
		tb = style.NewTable("ID", "SCOPE", "KIND", "LATEST VERSION", "DESCRIPTION", "CREATED BY", "CREATED AT")
	} else {
		tb = style.NewTable("ID", "SCOPE", "KIND", "LATEST VERSION", "DESCRIPTION")
	}

	for _, t := range templates {
		desc := sigilhttp.Truncate(t.Description, 40)
		version := t.LatestVersion
		if version == "" {
			version = "-"
		}

		if c.Wide {
			createdBy := t.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			tb.Row(t.TemplateID, t.Scope, t.Kind, version, desc, createdBy, sigilhttp.FormatTime(t.CreatedAt))
		} else {
			tb.Row(t.TemplateID, t.Scope, t.Kind, version, desc)
		}
	}
	return tb.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// VersionsTableCodec renders template versions as a text table.
type VersionsTableCodec struct{}

func (c *VersionsTableCodec) Format() format.Format { return "table" }

func (c *VersionsTableCodec) Encode(w io.Writer, v any) error {
	versions, ok := v.([]eval.TemplateVersion)
	if !ok {
		return errors.New("invalid data type for table codec: expected []TemplateVersion")
	}

	t := style.NewTable("VERSION", "CHANGELOG", "CREATED BY", "CREATED AT")
	for _, ver := range versions {
		changelog := sigilhttp.Truncate(ver.Changelog, 50)
		createdBy := ver.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(ver.Version, changelog, createdBy, sigilhttp.FormatTime(ver.CreatedAt))
	}
	return t.Render(w)
}

func (c *VersionsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

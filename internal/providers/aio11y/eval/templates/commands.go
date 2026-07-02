package templates

import (
	"context"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromContext(ctx, loader)
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

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	var scope string
	return crudcmd.NewListCommand(crudcmd.ListConfig[eval.TemplateDefinition]{
		Use:   "list",
		Short: "List eval templates.",
		Example: `  # List all templates.
  gcx aio11y templates list

  # Filter by scope.
  gcx aio11y templates list --scope global`,
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of templates to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		ExtraFlags: func(flags *pflag.FlagSet) {
			flags.StringVar(&scope, "scope", "", `Filter by scope: "global" or "tenant"`)
		},
		Fetch: func(ctx context.Context, limit int64) ([]eval.TemplateDefinition, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.List(ctx, scope, int(limit))
		},
	})
}

// --- get ---

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*eval.TemplateDetail]{
		Use:   "get <template-id>",
		Short: "Get a single eval template.",
		Long: `Get the full template definition including config and output keys.

Templates are reusable evaluator blueprints. Export a template as YAML,
customize it, and create an evaluator with 'evaluators create -f'.`,
		Example: `  # Get a template's config and output keys.
  gcx aio11y templates get my-template -o yaml > evaluator.yaml
  gcx aio11y evaluators create -f evaluator.yaml`,
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*eval.TemplateDetail, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.Get(ctx, args[0])
		},
	})
}

// --- versions ---

func newVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[[]eval.TemplateVersion]{
		Use:        "versions <template-id>",
		Short:      "List version history for an eval template.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "table",
		Codecs:     []format.Codec{&VersionsTableCodec{}},
		Fetch: func(ctx context.Context, args []string) ([]eval.TemplateVersion, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.ListVersions(ctx, args[0])
		},
	})
}

// --- table codecs ---

// TableCodec renders template list as a text table.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, tmpl eval.TemplateDefinition) {
		desc := aio11yhttp.Truncate(tmpl.Description, 40)
		version := tmpl.LatestVersion
		if version == "" {
			version = "-"
		}

		if !c.Wide {
			t.Row(tmpl.TemplateID, tmpl.Scope, tmpl.Kind, version, desc)
			return
		}

		createdBy := tmpl.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(tmpl.TemplateID, tmpl.Scope, tmpl.Kind, version, desc, createdBy, aio11yhttp.FormatTime(tmpl.CreatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "TemplateDefinition", []string{"ID", "SCOPE", "KIND", "LATEST VERSION", "DESCRIPTION", "CREATED BY", "CREATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "TemplateDefinition", []string{"ID", "SCOPE", "KIND", "LATEST VERSION", "DESCRIPTION"}, row)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}

// VersionsTableCodec renders template versions as a text table.
type VersionsTableCodec struct{}

func (c *VersionsTableCodec) Format() format.Format { return "table" }

func (c *VersionsTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "TemplateVersion", []string{"VERSION", "CHANGELOG", "CREATED BY", "CREATED AT"}, func(t *style.TableBuilder, ver eval.TemplateVersion) {
		changelog := aio11yhttp.Truncate(ver.Changelog, 50)
		createdBy := ver.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(ver.Version, changelog, createdBy, aio11yhttp.FormatTime(ver.CreatedAt))
	})
}

func (c *VersionsTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}

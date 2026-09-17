package templates

import (
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
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
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, Table())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Scope, "scope", "", `Filter by scope: "global" or "tenant"`)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of templates to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List eval templates.",
		Example: `  # List all templates.
  gcx agento11y templates list

  # Filter by scope.
  gcx agento11y templates list --scope global`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			templates, err := client.List(cmd.Context(), opts.Scope, int(opts.Limit))
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
customize it, and create an evaluator with 'evaluators upsert -f'.`,
		Example: `  # Get a template's config and output keys.
  gcx agento11y templates get my-template -o yaml > evaluator.yaml
  gcx agento11y evaluators upsert -f evaluator.yaml`,
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
	cmdio.RegisterTable(&o.IO, VersionsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionsOpts{}
	cmd := &cobra.Command{
		Use:   "list-versions <template-id>",
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

func Table() cmdio.Table[eval.TemplateDefinition] {
	return cmdio.Table[eval.TemplateDefinition]{Columns: []cmdio.Column[eval.TemplateDefinition]{
		{Header: "ID", Content: func(r eval.TemplateDefinition) string { return r.TemplateID }},
		{Header: "SCOPE", Content: func(r eval.TemplateDefinition) string { return r.Scope }},
		{Header: "KIND", Content: func(r eval.TemplateDefinition) string { return r.Kind }},
		{Header: "LATEST VERSION", Content: func(r eval.TemplateDefinition) string {
			if r.LatestVersion == "" {
				return "-"
			}
			return r.LatestVersion
		}},
		{Header: "DESCRIPTION", Content: func(r eval.TemplateDefinition) string { return agento11yhttp.Truncate(r.Description, 40) }},
		{Header: "CREATED BY", Visible: cmdio.WideOnly, Content: func(r eval.TemplateDefinition) string {
			if r.CreatedBy == "" {
				return "-"
			}
			return r.CreatedBy
		}},
		{Header: "CREATED AT", Visible: cmdio.WideOnly, Content: func(r eval.TemplateDefinition) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
	}}
}

func VersionsTable() cmdio.Table[eval.TemplateVersion] {
	return cmdio.Table[eval.TemplateVersion]{Columns: []cmdio.Column[eval.TemplateVersion]{
		{Header: "VERSION", Content: func(r eval.TemplateVersion) string { return r.Version }},
		{Header: "CHANGELOG", Content: func(r eval.TemplateVersion) string { return agento11yhttp.Truncate(r.Changelog, 50) }},
		{Header: "CREATED BY", Content: func(r eval.TemplateVersion) string {
			if r.CreatedBy == "" {
				return "-"
			}
			return r.CreatedBy
		}},
		{Header: "CREATED AT", Content: func(r eval.TemplateVersion) string { return agento11yhttp.FormatTime(r.CreatedAt) }},
	}}
}

package alert

import (
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// templatesCommands returns the templates command group.
func templatesCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Short:   "Manage Grafana alerting notification templates.",
		Aliases: []string{"template"},
	}
	cmd.AddCommand(
		newTemplatesListCommand(loader),
		newTemplatesGetCommand(loader),
		newTemplatesUpsertCommand(loader),
		newTemplatesDeleteCommand(loader),
	)
	return cmd
}

type templatesListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *templatesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TemplatesTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newTemplatesListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &templatesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List notification templates.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			templates, err := client.ListTemplates(ctx)
			if err != nil {
				return err
			}
			templates = adapter.TruncateSlice(templates, opts.Limit)
			return opts.IO.Encode(cmd.OutOrStdout(), templates)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// TemplatesTableCodec renders notification templates as a tabular table.
type TemplatesTableCodec struct{}

func (c *TemplatesTableCodec) Format() format.Format { return "table" }

func (c *TemplatesTableCodec) Encode(w io.Writer, v any) error {
	templates, ok := v.([]NotificationTemplate)
	if !ok {
		return errors.New("invalid data type for table codec: expected []NotificationTemplate")
	}
	t := style.NewTable("NAME", "PROVENANCE", "LENGTH")
	for _, tmpl := range templates {
		t.Row(tmpl.Name, tmpl.Provenance, strconv.Itoa(len(tmpl.Template)))
	}
	return t.Render(w)
}

func (c *TemplatesTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type templatesGetOpts struct {
	IO cmdio.Options
}

func (o *templatesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func newTemplatesGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &templatesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get a notification template by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			tmpl, err := client.GetTemplate(ctx, args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), tmpl)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type templatesUpsertOpts struct {
	IO   cmdio.Options
	File string
}

func (o *templatesUpsertOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the template definition (JSON/YAML, use - for stdin)")
}

func newTemplatesUpsertCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &templatesUpsertOpts{}
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Create or update a notification template.",
		Long: `Create or update a notification template.

The provisioning API uses a single PUT endpoint keyed by template name,
so the same command handles both create and update.`,
		Aliases: []string{"create", "update", "apply"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var t NotificationTemplate
			if err := readProvisioningInput(opts.File, cmd.InOrStdin(), &t); err != nil {
				return err
			}
			if t.Name == "" {
				return errors.New("template name is required in the input payload")
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			saved, err := client.UpsertTemplate(ctx, t)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), saved)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type templatesDeleteOpts struct {
	Force bool
}

func (o *templatesDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

//nolint:dupl // Similar structure to contact-points and mute-timings delete commands is intentional
func newTemplatesDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &templatesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a notification template by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ok, err := confirmDestructive(cmd.InOrStdin(), cmd.OutOrStdout(), opts.Force,
				"Delete notification template "+args[0]+"?")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			if err := client.DeleteTemplate(ctx, args[0]); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted template %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

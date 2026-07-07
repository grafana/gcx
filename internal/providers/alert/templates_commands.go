package alert

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
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

func newTemplatesListCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewListCommand(crudcmd.ListConfig[NotificationTemplate]{
		Use:          "list",
		Short:        "List notification templates.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		Codecs:       []format.Codec{&TemplatesTableCodec{}},
		Fetch: func(ctx context.Context, limit int64) ([]NotificationTemplate, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			templates, err := client.ListTemplates(ctx)
			if err != nil {
				return nil, err
			}
			return adapter.TruncateSlice(templates, limit), nil
		},
	})
}

// TemplatesTableCodec renders notification templates as a tabular table.
type TemplatesTableCodec struct{}

func (c *TemplatesTableCodec) Format() format.Format { return "table" }

func (c *TemplatesTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "NotificationTemplate", []string{"NAME", "PROVENANCE", "LENGTH"}, func(t *style.TableBuilder, tmpl NotificationTemplate) {
		t.Row(tmpl.Name, tmpl.Provenance, strconv.Itoa(len(tmpl.Template)))
	})
}

func (c *TemplatesTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}

func newTemplatesGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*NotificationTemplate]{
		Use:        "get NAME",
		Short:      "Get a notification template by name.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "json",
		Fetch: func(ctx context.Context, args []string) (*NotificationTemplate, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return client.GetTemplate(ctx, args[0])
		},
	})
}

func newTemplatesUpsertCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &crudcmd.MutateOpts{}
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
			t, err := crudcmd.ReadYAMLOrJSONFile[NotificationTemplate](opts.File, cmd.InOrStdin())
			if err != nil {
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
			saved, err := client.UpsertTemplate(ctx, *t)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), saved)
		},
	}
	opts.Setup(cmd.Flags(), "json", "File containing the template definition (JSON/YAML, use - for stdin)")
	return cmd
}

func newTemplatesDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete NAME",
		Short: "Delete a notification template by name.",
		Args:  cobra.ExactArgs(1),
		Confirm: func(args []string) string {
			return "Delete notification template " + args[0] + "?"
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return client.DeleteTemplate(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted template " + id },
	})
}

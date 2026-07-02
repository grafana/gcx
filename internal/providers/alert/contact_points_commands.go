package alert

import (
	"context"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

// contactPointsCommands returns the contact-points command group.
func contactPointsCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "contact-points",
		Short:   "Manage Grafana alerting contact points.",
		Aliases: []string{"contact-point"},
	}
	cmd.AddCommand(
		newContactPointsListCommand(loader),
		newContactPointsGetCommand(loader),
		newContactPointsCreateCommand(loader),
		newContactPointsUpdateCommand(loader),
		newContactPointsDeleteCommand(loader),
		newContactPointsExportCommand(loader),
	)
	return cmd
}

func newContactPointsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewListCommand(crudcmd.ListConfig[ContactPoint]{
		Use:          "list",
		Short:        "List alerting contact points.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		Codecs:       []format.Codec{&ContactPointsTableCodec{}},
		Fetch: func(ctx context.Context, limit int64) ([]ContactPoint, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			points, err := client.ListContactPoints(ctx)
			if err != nil {
				return nil, err
			}
			return adapter.TruncateSlice(points, limit), nil
		},
	})
}

// ContactPointsTableCodec renders contact points as a tabular table.
type ContactPointsTableCodec struct{}

func (c *ContactPointsTableCodec) Format() format.Format { return "table" }

func (c *ContactPointsTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "ContactPoint", []string{"UID", "NAME", "TYPE", "PROVENANCE"}, func(t *style.TableBuilder, p ContactPoint) {
		t.Row(p.UID, p.Name, p.Type, p.Provenance)
	})
}

func (c *ContactPointsTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}

func newContactPointsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*ContactPoint]{
		Use:        "get UID",
		Short:      "Get a single contact point by UID.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "json",
		Fetch: func(ctx context.Context, args []string) (*ContactPoint, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return client.GetContactPoint(ctx, args[0])
		},
	})
}

const contactPointFilenameUsage = "File containing the contact point definition (JSON/YAML, use - for stdin)"

func newContactPointsCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewCreateCommand(crudcmd.CreateConfig[ContactPoint]{
		Use:           "create",
		Short:         "Create a new contact point.",
		DefaultFmt:    "json",
		FilenameUsage: contactPointFilenameUsage,
		Read:          crudcmd.ReadYAMLOrJSONFile[ContactPoint],
		Create: func(ctx context.Context, cp ContactPoint) (ContactPoint, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return ContactPoint{}, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return ContactPoint{}, err
			}
			created, err := client.CreateContactPoint(ctx, cp)
			if err != nil {
				return ContactPoint{}, err
			}
			return *created, nil
		},
	})
}

func newContactPointsUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewUpdateCommand(crudcmd.UpdateConfig[ContactPoint]{
		Use:           "update UID",
		Short:         "Update an existing contact point by UID.",
		Args:          cobra.ExactArgs(1),
		DefaultFmt:    "json",
		FilenameUsage: contactPointFilenameUsage,
		Read:          crudcmd.ReadYAMLOrJSONFile[ContactPoint],
		Update: func(ctx context.Context, id string, cp ContactPoint) (ContactPoint, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return ContactPoint{}, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return ContactPoint{}, err
			}
			updated, err := client.UpdateContactPoint(ctx, id, cp)
			if err != nil {
				return ContactPoint{}, err
			}
			return *updated, nil
		},
	})
}

func newContactPointsDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete UID",
		Short: "Delete a contact point by UID.",
		Args:  cobra.ExactArgs(1),
		Confirm: func(args []string) string {
			return "Delete contact point " + args[0] + "?"
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
			return func(id string) error { return client.DeleteContactPoint(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted contact point " + id },
	})
}

type contactPointsExportOpts struct {
	Format string
}

func newContactPointsExportCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contactPointsExportOpts{}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export all contact points in provisioning format.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateExportFormat(opts.Format); err != nil {
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
			data, err := client.ExportContactPoints(ctx, opts.Format)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "yaml", "Export format: yaml, json, or hcl")
	return cmd
}

package alert

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

type contactPointsListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *contactPointsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ContactPointsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newContactPointsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contactPointsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alerting contact points.",
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
			points, err := client.ListContactPoints(ctx)
			if err != nil {
				return err
			}
			points = adapter.TruncateSlice(points, opts.Limit)
			return opts.IO.Encode(cmd.OutOrStdout(), points)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ContactPointsTableCodec renders contact points as a tabular table.
type ContactPointsTableCodec struct{}

func (c *ContactPointsTableCodec) Format() format.Format { return "table" }

func (c *ContactPointsTableCodec) Encode(w io.Writer, v any) error {
	points, ok := v.([]ContactPoint)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ContactPoint")
	}
	t := style.NewTable("UID", "NAME", "TYPE", "PROVENANCE")
	for _, p := range points {
		t.Row(p.UID, p.Name, p.Type, p.Provenance)
	}
	return t.Render(w)
}

func (c *ContactPointsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

type contactPointsGetOpts struct {
	IO cmdio.Options
}

func (o *contactPointsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func newContactPointsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contactPointsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get UID",
		Short: "Get a single contact point by UID.",
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
			cp, err := client.GetContactPoint(ctx, args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), cp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type contactPointsMutateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *contactPointsMutateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the contact point definition (JSON/YAML, use - for stdin)")
}

func newContactPointsCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contactPointsMutateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new contact point.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var cp ContactPoint
			if err := readProvisioningInput(opts.File, cmd.InOrStdin(), &cp); err != nil {
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
			created, err := client.CreateContactPoint(ctx, cp)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newContactPointsUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contactPointsMutateOpts{}
	cmd := &cobra.Command{
		Use:   "update UID",
		Short: "Update an existing contact point by UID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var cp ContactPoint
			if err := readProvisioningInput(opts.File, cmd.InOrStdin(), &cp); err != nil {
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
			updated, err := client.UpdateContactPoint(ctx, args[0], cp)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type contactPointsDeleteOpts struct {
	Force bool
}

func (o *contactPointsDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

//nolint:dupl // Similar structure to mute-timings and templates delete commands is intentional
func newContactPointsDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &contactPointsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete UID",
		Short: "Delete a contact point by UID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ok, err := confirmDestructive(cmd.InOrStdin(), cmd.OutOrStdout(), opts.Force,
				"Delete contact point "+args[0]+"?")
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
			if err := client.DeleteContactPoint(ctx, args[0]); err != nil {
				return err
			}
			cmdio.Success(cmd.OutOrStdout(), "Deleted contact point %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
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

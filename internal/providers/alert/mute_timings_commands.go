package alert

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// muteTimingsCommands returns the mute-timings command group.
func muteTimingsCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mute-timings",
		Short:   "Manage Grafana alerting mute timings.",
		Aliases: []string{"mute-timing"},
	}
	cmd.AddCommand(
		newMuteTimingsListCommand(loader),
		newMuteTimingsGetCommand(loader),
		newMuteTimingsCreateCommand(loader),
		newMuteTimingsUpdateCommand(loader),
		newMuteTimingsDeleteCommand(loader),
		newMuteTimingsExportCommand(loader),
	)
	return cmd
}

type muteTimingsListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *muteTimingsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &MuteTimingsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newMuteTimingsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &muteTimingsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List mute timings.",
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
			timings, err := client.ListMuteTimings(ctx)
			if err != nil {
				return err
			}
			timings = adapter.TruncateSlice(timings, opts.Limit)
			return opts.IO.Encode(cmd.OutOrStdout(), timings)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// MuteTimingsTableCodec renders mute timings as a tabular table.
type MuteTimingsTableCodec struct{}

func (c *MuteTimingsTableCodec) Format() format.Format { return "table" }

func (c *MuteTimingsTableCodec) Encode(w io.Writer, v any) error {
	timings, ok := v.([]MuteTiming)
	if !ok {
		return errors.New("invalid data type for table codec: expected []MuteTiming")
	}
	t := style.NewTable("NAME", "INTERVALS", "SUMMARY")
	for _, mt := range timings {
		t.Row(mt.Name, strconv.Itoa(len(mt.TimeIntervals)), summarizeIntervals(mt.TimeIntervals))
	}
	return t.Render(w)
}

func (c *MuteTimingsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}

func summarizeIntervals(intervals []TimeInterval) string {
	if len(intervals) == 0 {
		return ""
	}
	parts := make([]string, 0, len(intervals[0].Weekdays)+len(intervals[0].Times))
	parts = append(parts, intervals[0].Weekdays...)
	for _, t := range intervals[0].Times {
		parts = append(parts, t.Start+"-"+t.End)
	}
	return strings.Join(parts, ",")
}

type muteTimingsGetOpts struct {
	IO cmdio.Options
}

func (o *muteTimingsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func newMuteTimingsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &muteTimingsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get a mute timing by name.",
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
			mt, err := client.GetMuteTiming(ctx, args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), mt)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type muteTimingsMutateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *muteTimingsMutateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the mute timing definition (JSON/YAML, use - for stdin)")
}

//nolint:dupl // Similar structure to contact-points create command is intentional
func newMuteTimingsCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &muteTimingsMutateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new mute timing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var mt MuteTiming
			if err := readProvisioningInput(opts.File, cmd.InOrStdin(), &mt); err != nil {
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
			created, err := client.CreateMuteTiming(ctx, mt)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newMuteTimingsUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &muteTimingsMutateOpts{}
	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update an existing mute timing by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var mt MuteTiming
			if err := readProvisioningInput(opts.File, cmd.InOrStdin(), &mt); err != nil {
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
			updated, err := client.UpdateMuteTiming(ctx, args[0], mt)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newMuteTimingsDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a mute timing by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			if err := client.DeleteMuteTiming(ctx, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted mute timing %s\n", args[0])
			return nil
		},
	}
	return cmd
}

type muteTimingsExportOpts struct {
	Format string
	Name   string
}

func newMuteTimingsExportCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &muteTimingsExportOpts{}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export mute timings in provisioning format.",
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
			var (
				data   []byte
				expErr error
			)
			if opts.Name != "" {
				data, expErr = client.ExportMuteTiming(ctx, opts.Name, opts.Format)
			} else {
				data, expErr = client.ExportMuteTimings(ctx, opts.Format)
			}
			if expErr != nil {
				return expErr
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "yaml", "Export format: yaml, json, or hcl")
	cmd.Flags().StringVar(&opts.Name, "name", "", "Export a single mute timing by name (default: all)")
	return cmd
}

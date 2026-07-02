package alert

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
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

func newMuteTimingsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewListCommand(crudcmd.ListConfig[MuteTiming]{
		Use:          "list",
		Short:        "List mute timings.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		Codecs:       []format.Codec{&MuteTimingsTableCodec{}},
		Fetch: func(ctx context.Context, limit int64) ([]MuteTiming, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			timings, err := client.ListMuteTimings(ctx)
			if err != nil {
				return nil, err
			}
			return adapter.TruncateSlice(timings, limit), nil
		},
	})
}

// MuteTimingsTableCodec renders mute timings as a tabular table.
type MuteTimingsTableCodec struct{}

func (c *MuteTimingsTableCodec) Format() format.Format { return "table" }

func (c *MuteTimingsTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "MuteTiming", []string{"NAME", "INTERVALS", "SUMMARY"}, func(t *style.TableBuilder, mt MuteTiming) {
		t.Row(mt.Name, strconv.Itoa(len(mt.TimeIntervals)), summarizeIntervals(mt.TimeIntervals))
	})
}

func (c *MuteTimingsTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
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

func newMuteTimingsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*MuteTiming]{
		Use:        "get NAME",
		Short:      "Get a mute timing by name.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "json",
		Fetch: func(ctx context.Context, args []string) (*MuteTiming, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return client.GetMuteTiming(ctx, args[0])
		},
	})
}

const muteTimingFilenameUsage = "File containing the mute timing definition (JSON/YAML, use - for stdin)"

func newMuteTimingsCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewCreateCommand(crudcmd.CreateConfig[MuteTiming]{
		Use:           "create",
		Short:         "Create a new mute timing.",
		DefaultFmt:    "json",
		FilenameUsage: muteTimingFilenameUsage,
		Read:          crudcmd.ReadYAMLOrJSONFile[MuteTiming],
		Create: func(ctx context.Context, mt MuteTiming) (MuteTiming, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return MuteTiming{}, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return MuteTiming{}, err
			}
			created, err := client.CreateMuteTiming(ctx, mt)
			if err != nil {
				return MuteTiming{}, err
			}
			return *created, nil
		},
	})
}

func newMuteTimingsUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewUpdateCommand(crudcmd.UpdateConfig[MuteTiming]{
		Use:           "update NAME",
		Short:         "Update an existing mute timing by name.",
		Args:          cobra.ExactArgs(1),
		DefaultFmt:    "json",
		FilenameUsage: muteTimingFilenameUsage,
		Read:          crudcmd.ReadYAMLOrJSONFile[MuteTiming],
		Update: func(ctx context.Context, id string, mt MuteTiming) (MuteTiming, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return MuteTiming{}, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return MuteTiming{}, err
			}
			updated, err := client.UpdateMuteTiming(ctx, id, mt)
			if err != nil {
				return MuteTiming{}, err
			}
			return *updated, nil
		},
	})
}

func newMuteTimingsDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete NAME",
		Short: "Delete a mute timing by name.",
		Args:  cobra.ExactArgs(1),
		Confirm: func(args []string) string {
			return "Delete mute timing " + args[0] + "?"
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
			return func(id string) error { return client.DeleteMuteTiming(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted mute timing " + id },
	})
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

package reports

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// Commands returns the reports command group with CRUD subcommands.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reports",
		Short:   "Manage SLO reports.",
		Aliases: []string{"report"},
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newPushCommand(loader),
		newPullCommand(loader),
		newDeleteCommand(loader),
		newStatusCommand(loader),
		newTimelineCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &reportTableCodec{})
	o.IO.RegisterCustomCodec("wide", &reportTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SLO reports.",
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

			rpts, err := client.List(ctx)
			if err != nil {
				return err
			}

			// Table codec operates on raw []Report for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get/pull and round-trip support.
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), rpts)
			}

			var objs []unstructured.Unstructured
			for _, report := range rpts {
				res, err := ToResource(report, restCfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert report %s to resource: %w", report.UUID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// reportTableCodec renders reports as a tabular table.
type reportTableCodec struct {
	Wide bool
}

func (c *reportTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *reportTableCodec) Encode(w io.Writer, v any) error {
	rpts, ok := v.([]Report)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Report")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("UUID", "NAME", "TIME_SPAN", "SLOS", "SLO_UUIDS")
	} else {
		t = style.NewTable("UUID", "NAME", "TIME_SPAN", "SLOS")
	}

	for _, report := range rpts {
		timeSpan := mapTimeSpan(report.TimeSpan)
		sloCount := len(report.ReportDefinition.Slos)

		if c.Wide {
			sloUUIDs := make([]string, 0, sloCount)
			for _, s := range report.ReportDefinition.Slos {
				sloUUIDs = append(sloUUIDs, s.SloUUID)
			}
			t.Row(report.UUID, report.Name, timeSpan, strconv.Itoa(sloCount), strings.Join(sloUUIDs, ","))
		} else {
			t.Row(report.UUID, report.Name, timeSpan, strconv.Itoa(sloCount))
		}
	}

	return t.Render(w)
}

func (c *reportTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// mapTimeSpan converts API timeSpan values to human-readable labels.
func mapTimeSpan(timeSpan string) string {
	switch timeSpan {
	case "weeklySundayToSunday":
		return "weekly"
	case "calendarMonth":
		return "monthly"
	case "calendarYear":
		return "yearly"
	default:
		return timeSpan
	}
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get UUID",
		Short: "Get a single SLO report.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uuid := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			report, err := client.Get(ctx, uuid)
			if err != nil {
				return err
			}

			res, err := ToResource(*report, restCfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert report to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// pull command
// ---------------------------------------------------------------------------

type pullOpts struct {
	OutputDir string
}

func (o *pullOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.OutputDir, "output-dir", "d", ".", "Directory to write SLO report files to")
}

func newPullCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull SLO reports to disk.",
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

			rpts, err := client.List(ctx)
			if err != nil {
				return err
			}

			outputDir := filepath.Join(opts.OutputDir, "Report")
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
			}

			codec := format.NewYAMLCodec()

			for _, report := range rpts {
				res, err := ToResource(report, restCfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert report %s to resource: %w", report.UUID, err)
				}

				filePath := filepath.Join(outputDir, report.UUID+".yaml")
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", filePath, err)
				}

				obj := res.ToUnstructured()
				if err := codec.Encode(f, &obj); err != nil {
					f.Close()
					return fmt.Errorf("failed to write report %s: %w", report.UUID, err)
				}
				f.Close()
			}

			cmdio.Success(cmd.OutOrStdout(), "Pulled %d SLO reports to %s/", len(rpts), outputDir)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// push command
// ---------------------------------------------------------------------------

type pushOpts struct {
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without making them")
}

func newPushCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use:   "push FILE...",
		Short: "Push SLO reports from files.",
		Args:  cobra.MinimumNArgs(1),
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

			yamlCodec := format.NewYAMLCodec()

			for _, filePath := range args {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read file %s: %w", filePath, err)
				}

				// Decode YAML into an unstructured object
				var obj unstructured.Unstructured
				if err := yamlCodec.Decode(strings.NewReader(string(data)), &obj); err != nil {
					return fmt.Errorf("failed to parse %s: %w", filePath, err)
				}

				res, err := resources.FromUnstructured(&obj)
				if err != nil {
					return fmt.Errorf("failed to build resource from %s: %w", filePath, err)
				}

				report, err := FromResource(res)
				if err != nil {
					return fmt.Errorf("failed to convert resource to report from %s: %w", filePath, err)
				}

				if opts.DryRun {
					cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would push report %q (uuid=%s)", report.Name, report.UUID)
					continue
				}

				if err := upsertReport(ctx, cmd, client, report); err != nil {
					return err
				}
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// upsertReport creates or updates a report depending on whether it already exists.
// If report.UUID is set, it checks the server; a 404 means create, otherwise update.
// If report.UUID is empty, it always creates.
func upsertReport(ctx context.Context, cmd *cobra.Command, client *Client, report *Report) error {
	if report.UUID == "" {
		resp, err := client.Create(ctx, report)
		if err != nil {
			return fmt.Errorf("failed to create report %s: %w", report.Name, err)
		}
		cmdio.Success(cmd.OutOrStdout(), "Created %s (uuid=%s)", report.Name, resp.UUID)
		return nil
	}

	_, getErr := client.Get(ctx, report.UUID)
	switch {
	case getErr == nil:
		// Report exists — update.
		if err := client.Update(ctx, report.UUID, report); err != nil {
			return fmt.Errorf("failed to update report %s: %w", report.UUID, err)
		}
		cmdio.Success(cmd.OutOrStdout(), "Updated %s", report.Name)
		return nil

	case errors.Is(getErr, ErrNotFound):
		// Report not found — create.
		resp, err := client.Create(ctx, report)
		if err != nil {
			return fmt.Errorf("failed to create report %s: %w", report.Name, err)
		}
		cmdio.Success(cmd.OutOrStdout(), "Created %s (uuid=%s)", report.Name, resp.UUID)
		return nil

	default:
		// Any other error (auth, network, server) — propagate.
		return fmt.Errorf("failed to check report %s: %w", report.UUID, getErr)
	}
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete UUID...",
		Short: "Delete SLO reports.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if !opts.Force {
				fmt.Fprintf(cmd.OutOrStdout(), "Delete %d report(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}

				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			for _, uuid := range args {
				if err := client.Delete(ctx, uuid); err != nil {
					return fmt.Errorf("failed to delete report %s: %w", uuid, err)
				}
				cmdio.Success(cmd.OutOrStdout(), "Deleted %s", uuid)
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

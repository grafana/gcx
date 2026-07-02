package reports

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
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

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &crudcmd.ListOpts{}
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

			rpts = adapter.TruncateSlice(rpts, opts.Limit)

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
	opts.Setup(cmd.Flags(), "table", 50, "Maximum number of items to return (0 for all)", &reportTableCodec{}, &reportTableCodec{Wide: true})
	return cmd
}

// reportTableCodec renders reports as a tabular table.
type reportTableCodec struct {
	Wide bool
}

func (c *reportTableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *reportTableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, report Report) {
		timeSpan := mapTimeSpan(report.TimeSpan)
		sloCount := len(report.ReportDefinition.Slos)

		if c.Wide {
			sloUUIDs := make([]string, 0, sloCount)
			for _, s := range report.ReportDefinition.Slos {
				sloUUIDs = append(sloUUIDs, s.SloUUID)
			}
			t.Row(report.UUID, report.Name, timeSpan, strconv.Itoa(sloCount), strings.Join(sloUUIDs, ","))
			return
		}
		t.Row(report.UUID, report.Name, timeSpan, strconv.Itoa(sloCount))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "Report", []string{"UUID", "NAME", "TIME_SPAN", "SLOS", "SLO_UUIDS"}, row)
	}
	return crudcmd.EncodeTable(w, v, "Report", []string{"UUID", "NAME", "TIME_SPAN", "SLOS"}, row)
}

func (c *reportTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
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

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*unstructured.Unstructured]{
		Use:        "get UUID",
		Short:      "Get a single SLO report.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*unstructured.Unstructured, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}

			report, err := client.Get(ctx, args[0])
			if err != nil {
				return nil, err
			}

			res, err := ToResource(*report, restCfg.Namespace)
			if err != nil {
				return nil, fmt.Errorf("failed to convert report to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return &obj, nil
		},
	})
}

// ---------------------------------------------------------------------------
// pull command
// ---------------------------------------------------------------------------

func newPullCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewPullCommand(crudcmd.PullConfig[Report]{
		Use:         "pull",
		Short:       "Pull SLO reports to disk.",
		OutputUsage: "Directory to write SLO report files to",
		SubDir:      "Report",
		Noun:        "SLO reports",
		Fetch: func(ctx context.Context) ([]Report, string, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, "", err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, "", err
			}
			rpts, err := client.List(ctx)
			if err != nil {
				return nil, "", err
			}
			return rpts, restCfg.Namespace, nil
		},
		ToResource: ToResource,
		ID:         func(report Report) string { return report.UUID },
	})
}

// ---------------------------------------------------------------------------
// push command
// ---------------------------------------------------------------------------

func newPushCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewPushCommand(crudcmd.PushConfig[Report]{
		Use:          "push FILE...",
		Short:        "Push SLO reports from files.",
		FromResource: FromResource,
		NewUpsert: func(ctx context.Context) (func(cmd *cobra.Command, item *Report) error, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return func(cmd *cobra.Command, report *Report) error {
				return crudcmd.Upsert(ctx, *report, reportUpsertConfig(cmd, client))
			}, nil
		},
		Name: func(r Report) string { return r.Name },
		ID:   func(r Report) string { return r.UUID },
	})
}

// reportUpsertConfig adapts the raw report Client to the generic
// create-or-update-by-probing-404 flow shared with other push commands.
func reportUpsertConfig(cmd *cobra.Command, client *Client) crudcmd.UpsertConfig[Report] {
	return crudcmd.UpsertConfig[Report]{
		HasID: func(r Report) bool { return r.UUID != "" },
		ID:    func(r Report) string { return r.UUID },
		Name:  func(r Report) string { return r.Name },
		Get: func(ctx context.Context, id string) error {
			_, err := client.Get(ctx, id)
			return err
		},
		IsNotFound: func(err error) bool { return errors.Is(err, ErrNotFound) },
		Create: func(ctx context.Context, r Report) (Report, error) {
			resp, err := client.Create(ctx, &r)
			if err != nil {
				return Report{}, err
			}
			r.UUID = resp.UUID
			return r, nil
		},
		Update: func(ctx context.Context, id string, r Report) error {
			return client.Update(ctx, id, &r)
		},
		OnCreated: func(created Report) {
			cmdio.Success(cmd.OutOrStdout(), "Created %s (uuid=%s)", created.Name, created.UUID)
		},
		OnUpdated: func(r Report) {
			cmdio.Success(cmd.OutOrStdout(), "Updated %s", r.Name)
		},
	}
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete UUID...",
		Short: "Delete SLO reports.",
		Args:  cobra.MinimumNArgs(1),
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d report(s)?", len(args))
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
			return func(uuid string) error { return client.Delete(ctx, uuid) }, nil
		},
		Success: func(uuid string) string { return "Deleted " + uuid },
	})
}

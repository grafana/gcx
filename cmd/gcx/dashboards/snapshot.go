package dashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/dashboards"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

type snapshotOpts struct {
	Width       int
	Height      int
	Theme       string
	From        string
	To          string
	Window      string
	Tz          string
	PanelID     int
	OrgID       int
	OutputDir   string
	Concurrency int
	Vars        map[string]string
}

func (opts *snapshotOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&opts.Width, "width", 0, "Width of the rendered image in pixels (default: 1920 for dashboard, 800 for panel)")
	flags.IntVar(&opts.Height, "height", 0, "Height of the rendered image in pixels (default: -1/full-page for dashboard, 600 for panel)")
	flags.StringVar(&opts.Theme, "theme", "dark", "Grafana theme (light or dark)")
	flags.StringVar(&opts.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.Window, "window", "", "Time window shorthand (e.g. '1h', '7d'); expands to --from now-{window} --to now; mutually exclusive with --from/--to")
	flags.StringVar(&opts.Tz, "tz", "", "Timezone (e.g. 'UTC', 'America/New_York')")
	flags.IntVar(&opts.PanelID, "panel", 0, "Panel ID to render a single panel instead of the full dashboard")
	flags.IntVar(&opts.OrgID, "org-id", 1, "Grafana organization ID")
	flags.StringVar(&opts.OutputDir, "output-dir", ".", "Directory to write PNG files to (created if it does not exist)")
	flags.IntVar(&opts.Concurrency, "concurrency", 10, "Maximum number of concurrent render requests")
	flags.StringToStringVar(&opts.Vars, "var", nil, "Dashboard template variable overrides (e.g. --var cluster=prod --var datasource=prometheus)")
}

func (opts *snapshotOpts) Validate() error {
	if opts.Window != "" && (opts.From != "" || opts.To != "") {
		return errors.New("--window is mutually exclusive with --from and --to")
	}

	if opts.Window != "" {
		opts.From = "now-" + opts.Window
		opts.To = "now"
	}

	if opts.Theme != "light" && opts.Theme != "dark" {
		return fmt.Errorf("--theme must be \"light\" or \"dark\", got %q", opts.Theme)
	}

	// Apply default dimensions based on whether a specific panel is requested.
	if opts.Width == 0 {
		if opts.PanelID != 0 {
			opts.Width = 800
		} else {
			opts.Width = 1920
		}
	}
	if opts.Height == 0 {
		if opts.PanelID != 0 {
			opts.Height = 600
		} else {
			opts.Height = -1 // full page height — renderer captures entire dashboard
		}
	}

	return nil
}

func snapshotCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &snapshotOpts{}

	cmd := &cobra.Command{
		Use:   "snapshot <uid> [uid...]",
		Short: "Render dashboard snapshots as PNG images",
		Long:  "Render one or more Grafana dashboards or individual panels as PNG images using the Grafana Image Renderer.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "my-dashboard-uid --width 1920",
		},
		Example: `
  # Snapshot a full dashboard
  gcx dashboards snapshot my-dashboard-uid

  # Snapshot a specific panel
  gcx dashboards snapshot my-dashboard-uid --panel 42

  # Snapshot with custom dimensions and time range
  gcx dashboards snapshot my-dashboard-uid --width 1000 --height 500 --theme light --from now-1h --to now

  # Snapshot using a time window shorthand
  gcx dashboards snapshot my-dashboard-uid --window 6h

  # Snapshot multiple dashboards to a specific directory
  gcx dashboards snapshot uid1 uid2 uid3 --output-dir ./snapshots

  # Snapshot with dashboard template variable overrides
  gcx dashboards snapshot my-dashboard-uid --var cluster=prod --var datasource=prometheus`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			client, err := dashboards.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create render client: %w", err)
			}

			// results and errs are indexed by args position; no mutex needed since
			// each goroutine writes to a unique index.
			results := make([]dashboards.SnapshotResult, len(args))
			errs := make([]error, len(args))

			// Use a plain errgroup (no derived context) so that a single render
			// failure does not cancel in-flight renders for other UIDs.
			g := new(errgroup.Group)
			g.SetLimit(opts.Concurrency)

			for i, uid := range args {
				g.Go(func() error {
					// Reject UIDs containing path separators to prevent directory traversal
					// when constructing the output filename.
					if strings.ContainsAny(uid, "/\\") || filepath.Base(uid) != uid {
						errs[i] = fmt.Errorf("dashboard UID %q contains invalid path characters", uid)
						return nil
					}

					req := dashboards.RenderRequest{
						UID:     uid,
						PanelID: opts.PanelID,
						OrgID:   opts.OrgID,
						Width:   opts.Width,
						Height:  opts.Height,
						Theme:   opts.Theme,
						From:    opts.From,
						To:      opts.To,
						Tz:      opts.Tz,
						Vars:    opts.Vars,
					}

					png, err := client.Render(ctx, req)
					if err != nil {
						errs[i] = fmt.Errorf("failed to render %q: %w", uid, err)
						return nil
					}

					var filename string
					if opts.PanelID != 0 {
						filename = fmt.Sprintf("%s-panel-%d.png", uid, opts.PanelID)
					} else {
						filename = uid + ".png"
					}

					filePath, err := filepath.Abs(filepath.Join(opts.OutputDir, filename))
					if err != nil {
						errs[i] = fmt.Errorf("failed to resolve output path: %w", err)
						return nil
					}

					if _, statErr := os.Stat(filePath); statErr == nil {
						slog.Debug("overwriting existing snapshot", "path", filePath)
					}

					if err := os.WriteFile(filePath, png, 0o600); err != nil {
						errs[i] = fmt.Errorf("failed to write %q: %w", filePath, err)
						return nil
					}

					var panelID *int
					if opts.PanelID != 0 {
						p := opts.PanelID
						panelID = &p
					}

					results[i] = dashboards.SnapshotResult{
						UID:        uid,
						PanelID:    panelID,
						FilePath:   filePath,
						Width:      opts.Width,
						Height:     opts.Height,
						Theme:      opts.Theme,
						RenderedAt: time.Now(),
						FileSize:   int64(len(png)),
					}
					return nil
				})
			}

			_ = g.Wait()

			// Collect successful results and render errors.
			var successResults []dashboards.SnapshotResult
			var renderErrs []error
			for i, r := range results {
				if r.UID != "" {
					successResults = append(successResults, r)
				}
				if errs[i] != nil {
					renderErrs = append(renderErrs, errs[i])
				}
			}

			// Output whatever succeeded before surfacing errors.
			if agent.IsAgentMode() {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(successResults); err != nil {
					return err
				}
				return errors.Join(renderErrs...)
			}

			if err := renderSnapshotTable(cmd.OutOrStdout(), successResults); err != nil {
				return err
			}
			return errors.Join(renderErrs...)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

func renderSnapshotTable(w interface{ Write(b []byte) (int, error) }, results []dashboards.SnapshotResult) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "UID\tPANEL\tFILE\tSIZE")

	for _, r := range results {
		panelStr := ""
		if r.PanelID != nil {
			panelStr = strconv.Itoa(*r.PanelID)
		}

		sizeStr := fmt.Sprintf("%d B", r.FileSize)
		if r.FileSize >= 1024*1024 {
			sizeStr = fmt.Sprintf("%.1f MB", float64(r.FileSize)/(1024*1024))
		} else if r.FileSize >= 1024 {
			sizeStr = fmt.Sprintf("%.1f KB", float64(r.FileSize)/1024)
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.UID, panelStr, r.FilePath, sizeStr)
	}

	return tw.Flush()
}

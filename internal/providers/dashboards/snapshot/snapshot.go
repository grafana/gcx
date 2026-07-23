// Package snapshot provides the `gcx dashboards snapshot` command, which renders
// Grafana dashboards or individual panels as PNG images via the Image Renderer.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/dashboards"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// GrafanaConfigLoader is the subset of the config loader used here.
// Defined as a local interface so the command can be tested with a stub
// (narrow coupling; the concrete *providers.ConfigLoader satisfies the interface).
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

type snapshotOpts struct {
	IO          cmdio.Options
	Width       int
	Height      int
	Theme       string
	From        string
	To          string
	Since       string
	Tz          string
	PanelID     int
	OrgID       int
	OutputDir   string
	Concurrency int
	Vars        map[string]string
}

func (opts *snapshotOpts) setup(flags *pflag.FlagSet) {
	// The snapshot result is a snapshotReceipt document (files-on-disk
	// receipt) through the codec system: the default table codec prints the
	// familiar NAME/PANEL/FILE/SIZE table; agent mode and explicit
	// -o json/yaml get the structured document with enumerated failures.
	opts.IO.RegisterCustomCodec("table", &snapshotTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.IntVar(&opts.Width, "width", 0, "Width of the rendered image in pixels (default: 1920 for dashboard, 800 for panel)")
	flags.IntVar(&opts.Height, "height", 0, "Height of the rendered image in pixels (default: -1/full-page for dashboard, 600 for panel)")
	flags.StringVar(&opts.Theme, "theme", "dark", "Grafana theme (light or dark)")
	flags.StringVar(&opts.From, "from", "", "Start time (relative like 'now-1h', RFC3339, Unix seconds, or Unix milliseconds)")
	flags.StringVar(&opts.To, "to", "", "End time (relative like 'now', RFC3339, Unix seconds, or Unix milliseconds)")
	flags.StringVar(&opts.Since, "since", "", "Duration before now (e.g. '1h', '7d'); expands to --from now-{since} --to now; mutually exclusive with --from/--to")
	flags.StringVar(&opts.Tz, "tz", "", "Timezone (e.g. 'UTC', 'America/New_York')")
	flags.IntVar(&opts.PanelID, "panel", 0, "Panel number to render a single panel instead of the full dashboard")
	flags.IntVar(&opts.OrgID, "org-id", 1, "Grafana organization number")
	flags.StringVar(&opts.OutputDir, "output-dir", ".", "Directory to write PNG files to (created if it does not exist)")
	flags.IntVar(&opts.Concurrency, "concurrency", 10, "Maximum number of concurrent render requests")
	flags.StringToStringVar(&opts.Vars, "var", nil, "Dashboard template variable overrides (e.g. --var cluster=prod --var datasource=prometheus)")
}

func (opts *snapshotOpts) Validate() error {
	if opts.Since != "" && (opts.From != "" || opts.To != "") {
		return errors.New("--since is mutually exclusive with --from and --to")
	}

	if opts.Since != "" {
		opts.From = "now-" + opts.Since
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

	return opts.IO.Validate()
}

// snapshotReceipt is the finite result document for `dashboards snapshot`: a
// files-on-disk receipt in the spirit of cmdio.ArtifactReceipt. It is a
// bespoke shape (with the required collision-resistant discriminators)
// because its per-file entries carry render metadata — panel, dimensions,
// theme, render time — that the generic cmdio.ArtifactFile cannot express.
type snapshotReceipt struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	Dir           string `json:"dir" yaml:"dir"`
	// Format is the on-disk content format of the written files.
	Format string `json:"format" yaml:"format"`
	// Files enumerates the PNGs written, one entry per rendered target.
	Files []dashboards.SnapshotResult `json:"files" yaml:"files"`
	// Summary counts every requested target exactly once.
	Summary cmdio.MutationSummary `json:"summary" yaml:"summary"`
	// Failures is always present — [] when nothing failed — so consumers
	// never need a nil check before ranging.
	Failures []cmdio.MutationFailure `json:"failures" yaml:"failures"`
}

// Discriminator values for the snapshot receipt shape.
const (
	snapshotReceiptType          = "gcx.dashboards.snapshot"
	snapshotReceiptSchemaVersion = "1"
)

// newSnapshotReceipt returns a snapshotReceipt with the discriminators set
// and the always-serialized slices initialized.
func newSnapshotReceipt(dir string) snapshotReceipt {
	return snapshotReceipt{
		Type:          snapshotReceiptType,
		SchemaVersion: snapshotReceiptSchemaVersion,
		Action:        "rendered",
		Dir:           dir,
		Format:        "png",
		Files:         []dashboards.SnapshotResult{},
		Failures:      []cmdio.MutationFailure{},
	}
}

// snapshotTableCodec is the human "table" codec for snapshotReceipt values:
// it renders exactly the NAME/PANEL/FILE/SIZE table of successful renders the
// command has always printed, keeping default human stdout byte-identical.
// Failures are enumerated in the structured formats and surfaced on stderr.
type snapshotTableCodec struct{}

func (c *snapshotTableCodec) Format() format.Format { return "table" }

func (c *snapshotTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

func (c *snapshotTableCodec) Encode(w io.Writer, value any) error {
	receipt, ok := value.(snapshotReceipt)
	if !ok {
		return errors.New("invalid data type for snapshot table codec: expected snapshotReceipt")
	}
	return renderSnapshotTable(w, receipt.Files)
}

// Commands returns the snapshot cobra subcommand, wired to the given config loader.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	opts := &snapshotOpts{}

	cmd := &cobra.Command{
		Use:   "snapshot <name> [name...]",
		Short: "Render dashboard snapshots as PNG images",
		Long:  "Render one or more Grafana dashboards or individual panels as PNG images using the Grafana Image Renderer.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "my-dashboard-name --width 1920",
		},
		Example: `
  # Snapshot a full dashboard
  gcx dashboards snapshot my-dashboard-name

  # Snapshot a specific panel
  gcx dashboards snapshot my-dashboard-name --panel 42

  # Snapshot with custom dimensions and time range
  gcx dashboards snapshot my-dashboard-name --width 1000 --height 500 --theme light --from now-1h --to now

  # Snapshot using a duration shorthand
  gcx dashboards snapshot my-dashboard-name --since 6h

  # Snapshot multiple dashboards to a specific directory
  gcx dashboards snapshot name1 name2 name3 --output-dir ./snapshots

  # Snapshot with dashboard template variable overrides
  gcx dashboards snapshot my-dashboard-name --var cluster=prod --var datasource=prometheus`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
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
			// failure does not cancel in-flight renders for other names.
			g := new(errgroup.Group)
			g.SetLimit(opts.Concurrency)

			for i, name := range args {
				g.Go(func() error {
					// Reject names containing path separators to prevent directory traversal
					// when constructing the output filename.
					if strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
						errs[i] = fmt.Errorf("dashboard name %q contains invalid path characters", name)
						return nil
					}

					req := dashboards.RenderRequest{
						UID:     name,
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
						errs[i] = fmt.Errorf("failed to render %q: %w", name, err)
						return nil
					}

					var filename string
					if opts.PanelID != 0 {
						filename = fmt.Sprintf("%s-panel-%d.png", name, opts.PanelID)
					} else {
						filename = name + ".png"
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
						UID:        name,
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

			// Collect successful results and render errors into the receipt.
			receipt := newSnapshotReceipt(opts.OutputDir)
			var renderErrs []error
			for i, r := range results {
				if r.UID != "" {
					receipt.Files = append(receipt.Files, r)
				}
				if errs[i] != nil {
					renderErrs = append(renderErrs, errs[i])
					receipt.Failures = append(receipt.Failures, cmdio.MutationFailure{
						Target: cmdio.MutationTarget{Kind: "Dashboard", Name: args[i]},
						Error:  errs[i].Error(),
					})
				}
			}
			receipt.Summary = cmdio.MutationSummary{
				Succeeded: len(receipt.Files),
				Failed:    len(receipt.Failures),
			}

			// Total failure: no receipt — a success-shaped document with
			// zero files would be misleading, and exit 4 would misreport a
			// complete failure as partial. The raw error takes the standard
			// path (one gcx.error document in agent mode, exit 1), matching
			// the batch cohort's zero-success convention.
			if len(renderErrs) > 0 && len(receipt.Files) == 0 {
				return errors.Join(renderErrs...)
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), receipt); err != nil {
				return err
			}

			if len(renderErrs) > 0 {
				// The receipt (with enumerated failures) is already on
				// stdout — surface the detail on stderr for humans (the
				// table shows successes only) and carry exit 4 via
				// EmittedError without a second stdout document.
				for _, failure := range receipt.Failures {
					cmdio.EmitWarn(cmd.ErrOrStderr(), failure.Error)
				}
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure,
					errors.Join(renderErrs...))
			}
			return nil
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

func renderSnapshotTable(w io.Writer, results []dashboards.SnapshotResult) error {
	t := style.NewTable("NAME", "PANEL", "FILE", "SIZE")

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

		t.Row(r.UID, panelStr, r.FilePath, sizeStr)
	}

	return t.Render(w)
}

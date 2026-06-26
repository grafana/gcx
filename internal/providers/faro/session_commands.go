package faro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// FormatText is the output format name for the session table codec.
const FormatText = format.Format("text")

// SessionTableRow holds pre-computed data for one recording row.
type SessionTableRow struct {
	RecordingID       string        `json:"recording_id"`
	Status            string        `json:"status"`
	Duration          time.Duration `json:"-"`
	DurationHuman     string        `json:"duration"`
	Segments          int           `json:"segments"`
	InactivityPeriods int           `json:"inactivity_periods"`
}

// SessionTableCodec renders []SessionTableRow as a table.
type SessionTableCodec struct{}

// Format returns the output format name.
func (c *SessionTableCodec) Format() format.Format { return FormatText }

// Encode writes session table rows to the writer as a table.
func (c *SessionTableCodec) Encode(w io.Writer, v any) error {
	rows, ok := v.([]SessionTableRow)
	if !ok {
		return fmt.Errorf("invalid data type for session table codec: expected []SessionTableRow, got %T", v)
	}

	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "No recordings found.")
		return err
	}

	t := style.NewTable("RECORDING ID", "STATUS", "DURATION", "SEGMENTS", "INACTIVITY PERIODS")
	for _, r := range rows {
		t.Row(r.RecordingID, r.Status, r.DurationHuman, strconv.Itoa(r.Segments), strconv.Itoa(r.InactivityPeriods))
	}
	return t.Render(w)
}

// Decode is not supported for text format.
func (c *SessionTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// resolveFirstRecordingID lists recordings (limit 1) and returns the first recording's ID.
func resolveFirstRecordingID(ctx context.Context, client *Client, appID, sessionID string) (string, error) {
	resp, err := client.ListRecordings(ctx, appID, sessionID, 1)
	if err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return "", errors.New("no recordings found for session")
	}

	log := logging.FromContext(ctx)
	log.Debug("Resolved first recording ID", "recording_id", resp.Items[0].ID)
	return resp.Items[0].ID, nil
}

// ---------------------------------------------------------------------------
// show-session command
// ---------------------------------------------------------------------------

type showSessionOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *showSessionOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &SessionTableCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of recordings to return (0 for unlimited)")
}

func newShowSessionCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &showSessionOpts{}
	cmd := &cobra.Command{
		Use:   "show-session <app-name> <session-id>",
		Short: "Show recordings for a Frontend Observability session.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			appID := resolveAppID(args[0])
			sessionID := args[1]

			resp, err := client.ListRecordings(ctx, appID, sessionID, opts.Limit)
			if err != nil {
				return err
			}

			rows := make([]SessionTableRow, len(resp.Items))
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(10)

			for i, item := range resp.Items {
				g.Go(func() error {
					manifest, err := client.GetManifest(gctx, appID, sessionID, item.ID)
					if err != nil {
						return fmt.Errorf("fetching manifest for recording %s: %w", item.ID, err)
					}

					dur := item.EndTS.Sub(item.StartTS)
					rows[i] = SessionTableRow{
						RecordingID:       item.ID,
						Status:            item.Status,
						Duration:          dur,
						DurationHuman:     dur.Truncate(time.Second).String(),
						Segments:          len(manifest.Segments),
						InactivityPeriods: len(manifest.InactivityPeriods),
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), rows)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Event type name mappings
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals // Static lookup table for rrweb event types.
var eventTypeNames = map[int]string{
	0: "DomContentLoaded",
	1: "Load",
	2: "FullSnapshot",
	3: "IncrementalSnapshot",
	4: "Meta",
	5: "Custom",
	6: "Plugin",
}

// EventTypeName returns a human-readable name for an rrweb event type code.
func EventTypeName(t int) string {
	if name, ok := eventTypeNames[t]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", t)
}

//nolint:gochecknoglobals // Static lookup table for rrweb incremental source types.
var incrementalSourceNames = map[int]string{
	0: "Mutation", 1: "MouseMove", 2: "MouseInteraction", 3: "Scroll",
	4: "ViewportResize", 5: "Input", 6: "TouchMove", 7: "MediaInteraction",
	8: "StyleSheetRule", 9: "CanvasMutation", 10: "Font", 11: "Log",
	12: "Drag", 13: "StyleDeclaration", 14: "Selection",
}

// IncrementalSourceName returns a human-readable name for an rrweb incremental snapshot source.
func IncrementalSourceName(s int) string {
	if name, ok := incrementalSourceNames[s]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", s)
}

// ---------------------------------------------------------------------------
// Segment summary codec
// ---------------------------------------------------------------------------

// EventSummaryRow holds pre-computed data for one event row in a segment summary.
type EventSummaryRow struct {
	Index     int       `json:"index"`
	Timestamp time.Time `json:"timestamp"`
	TypeName  string    `json:"type_name"`
	Source    string    `json:"source"`
}

// SegmentSummaryCodec renders []EventSummaryRow as a table.
type SegmentSummaryCodec struct{}

// Format returns the output format name.
func (c *SegmentSummaryCodec) Format() format.Format { return FormatText }

// Encode writes event summary rows to the writer as a table.
func (c *SegmentSummaryCodec) Encode(w io.Writer, v any) error {
	rows, ok := v.([]EventSummaryRow)
	if !ok {
		return fmt.Errorf("invalid data type for segment summary codec: expected []EventSummaryRow, got %T", v)
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "No events in segment.")
		return err
	}
	t := style.NewTable("INDEX", "TIMESTAMP", "TYPE", "SOURCE")
	for _, r := range rows {
		t.Row(strconv.Itoa(r.Index), r.Timestamp.UTC().Format(time.RFC3339), r.TypeName, r.Source)
	}
	return t.Render(w)
}

// Decode is not supported for text format.
func (c *SegmentSummaryCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// EventsToSummaryRows converts a slice of RRWebEvent to summary rows for display.
func EventsToSummaryRows(events []RRWebEvent) []EventSummaryRow {
	rows := make([]EventSummaryRow, len(events))
	for i, e := range events {
		typeName := EventTypeName(e.Type)
		source := "-"
		if e.Type == 3 {
			var data struct {
				Source int `json:"source"`
			}
			if json.Unmarshal(e.Data, &data) == nil {
				source = IncrementalSourceName(data.Source)
			}
		}
		rows[i] = EventSummaryRow{
			Index:     i,
			Timestamp: time.UnixMilli(e.Timestamp).UTC(),
			TypeName:  typeName,
			Source:    source,
		}
	}
	return rows
}

// ---------------------------------------------------------------------------
// show-segment command
// ---------------------------------------------------------------------------

type showSegmentOpts struct {
	IO          cmdio.Options
	Save        string
	Raw         bool
	RecordingID string
}

func (o *showSegmentOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Save, "save", "", "Write full event JSON to a file")
	flags.BoolVar(&o.Raw, "raw", false, "Output full raw RRWeb event JSON instead of summary table")
	flags.StringVar(&o.RecordingID, "recording-id", "", "Recording ID to use (defaults to the first recording)")
	o.IO.RegisterCustomCodec("text", &SegmentSummaryCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newShowSegmentCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &showSegmentOpts{}
	cmd := &cobra.Command{
		Use:   "show-segment <app-name> <session-id> <segment-id>",
		Short: "Show events for a session recording segment.",
		Example: `  # Show event summary for segment 0.
  gcx frontend apps show-segment my-web-app-42 abc-session-123 0

  # Save full event JSON to a file.
  gcx frontend apps show-segment my-web-app-42 abc-session-123 0 --save events.json

  # Output raw event JSON.
  gcx frontend apps show-segment my-web-app-42 abc-session-123 0 --raw`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			appID := resolveAppID(args[0])
			sessionID := args[1]
			segmentID := args[2]

			recordingID := opts.RecordingID
			if recordingID == "" {
				recordingID, err = resolveFirstRecordingID(ctx, client, appID, sessionID)
				if err != nil {
					return err
				}
			}

			segment, err := client.GetSegment(ctx, appID, sessionID, recordingID, segmentID)
			if err != nil {
				return err
			}

			// Warn if segment has dependency.
			manifest, manifestErr := client.GetManifest(ctx, appID, sessionID, recordingID)
			if manifestErr == nil {
				segID, _ := strconv.ParseInt(segmentID, 10, 64)
				for _, s := range manifest.Segments {
					if s.ID == segID && s.RequiresSegmentID != nil {
						cmdio.Warning(cmd.ErrOrStderr(), "Segment %s depends on segment %d (full snapshot). Events may not be interpretable without it.", segmentID, *s.RequiresSegmentID)
						break
					}
				}
			}

			if opts.Save != "" {
				data, err := json.MarshalIndent(segment.Events, "", "  ")
				if err != nil {
					return fmt.Errorf("marshaling events: %w", err)
				}
				if err := os.WriteFile(opts.Save, data, 0o600); err != nil {
					return fmt.Errorf("writing events to %s: %w", opts.Save, err)
				}
				cmdio.Success(cmd.ErrOrStderr(), "Saved %d events to %s", len(segment.Events), opts.Save)
				return nil
			}

			if opts.Raw {
				data, err := json.MarshalIndent(segment.Events, "", "  ")
				if err != nil {
					return fmt.Errorf("marshaling events: %w", err)
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}

			rows := EventsToSummaryRows(segment.Events)
			return opts.IO.Encode(cmd.OutOrStdout(), rows)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

package faro

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const replayFetchConcurrency = 10

func fetchReplayManifests(ctx context.Context, client *Client, appID, sessionID string, recordings []RecordingListItem) ([]*RecordingManifestResponse, error) {
	manifests := make([]*RecordingManifestResponse, len(recordings))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(replayFetchConcurrency)
	for i, recording := range recordings {
		g.Go(func() error {
			manifest, err := client.GetManifest(gctx, appID, sessionID, recording.ID)
			if err != nil {
				return fmt.Errorf("fetching replay manifest %s: %w", recording.ID, err)
			}
			if manifest.ID != recording.ID || manifest.SessionID != sessionID {
				return fmt.Errorf("replay manifest identity does not match session %s recording %s", sessionID, recording.ID)
			}
			manifests[i] = manifest
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return manifests, nil
}

func fetchReplaySegmentBatch(ctx context.Context, client *Client, appID, sessionID, recordingID string, metadata []ManifestSegment) ([]*RecordingSegmentResponse, error) {
	segments := make([]*RecordingSegmentResponse, len(metadata))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(replayFetchConcurrency)
	for i, segmentMeta := range metadata {
		g.Go(func() error {
			segment, err := client.GetSegment(gctx, appID, sessionID, recordingID, strconv.FormatInt(segmentMeta.ID, 10))
			if err != nil {
				return fmt.Errorf("fetching replay segment %d of recording %s: %w", segmentMeta.ID, recordingID, err)
			}
			if segment.RecordingID != recordingID {
				return fmt.Errorf("replay segment %d belongs to a different recording", segmentMeta.ID)
			}
			segments[i] = segment
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return segments, nil
}

type sessionsGetReplayOpts struct {
	IO    cmdio.Options
	App   string
	Save  string
	flags *pflag.FlagSet
}

func (o *sessionsGetReplayOpts) setup(flags *pflag.FlagSet) {
	o.IO.PinDefaultFormat("json")
	o.IO.HideFormat("agents")
	o.IO.BindFlags(flags)
	o.flags = flags
	_ = flags.MarkHidden("json")
	_ = flags.MarkHidden("jq")
	flags.StringVar(&o.App, "app", "", "Frontend Observability app slug-id or numeric id (required)")
	flags.StringVar(&o.Save, "save", "", "Path for the complete session replay in the selected -o format (required)")
}

func (o *sessionsGetReplayOpts) Validate() error {
	o.App = strings.TrimSpace(o.App)
	o.Save = strings.TrimSpace(o.Save)
	if o.App == "" {
		return errors.New("--app is required")
	}
	if o.Save == "" {
		return errors.New("--save is required")
	}
	saveDir := filepath.Dir(o.Save)
	info, err := os.Stat(saveDir)
	if err != nil {
		return fmt.Errorf("replay output directory %q: %w", saveDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("replay output directory %q is not a directory", saveDir)
	}
	switch o.IO.OutputFormat {
	case "json", "yaml":
	default:
		return fmt.Errorf("output format %q cannot be used with get-replay: use -o json or -o yaml", o.IO.OutputFormat)
	}
	if o.flags != nil {
		if f := o.flags.Lookup("json"); f != nil && f.Changed {
			return errors.New("--json cannot be used with get-replay: the selected fields would not be written to the replay file; use -o json or -o yaml")
		}
		if f := o.flags.Lookup("jq"); f != nil && f.Changed {
			return errors.New("--jq cannot be used with get-replay: the transformed output would not be written to the replay file; use -o json or -o yaml")
		}
	}
	return o.IO.Validate()
}

type replayArtifactReceipt struct {
	cmdio.ArtifactReceipt

	EventCount int    `json:"event_count"`
	ReplayURL  string `json:"replay_url"`
}

func newSessionsGetReplayCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &sessionsGetReplayOpts{}
	cmd := &cobra.Command{
		Use:   "get-replay <session-id>",
		Short: "Save all replays for a Frontend Observability session.",
		Long: `Save every recording in a session to one file. JSON is the default format;
use -o yaml to write YAML. Each recording contains its complete rrweb event
stream, assembled from its segments in order. Recording boundaries are preserved
because separate recordings can overlap in time.`,
		Example: `  # Save the replay for a session to a private JSON file.
  gcx frontend sessions get-replay abc-session-123 --app my-web-app-42 --save replay.json

  # Save it as YAML instead.
  gcx frontend sessions get-replay abc-session-123 --app my-web-app-42 --save replay.yaml -o yaml`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				return errors.New("expected one non-empty session ID")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			appID, err := parseReplayAppID(opts.App)
			if err != nil {
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
			sessionID := strings.TrimSpace(args[0])
			recordings, err := client.ListRecordings(ctx, appID, sessionID)
			if err != nil {
				return err
			}
			if len(recordings) == 0 {
				return fmt.Errorf("no replay found for session %s with app ID %s (the app may not exist or have no replay)", sessionID, appID)
			}
			count, err := saveSessionReplayEvents(ctx, client, appID, sessionID, recordings, opts.Save, opts.IO.OutputFormat)
			if err != nil {
				return err
			}
			replayURL := sessionReplayURL(cfg.GrafanaURL, appID, sessionID)
			receipt := replayArtifactReceipt{
				ArtifactReceipt: cmdio.NewArtifactReceipt("get-replay", opts.IO.OutputFormat),
				EventCount:      count,
				ReplayURL:       replayURL,
			}
			receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: opts.Save, Kind: "session-replay", Count: len(recordings)})
			receipt.Summary = cmdio.MutationSummary{Succeeded: 1}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Wrote %d events from %d recordings to %s\nReplay: %s\n", count, len(recordings), opts.Save, replayURL)
				return err
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func sessionReplayURL(host, appID, sessionID string) string {
	return strings.TrimRight(host, "/") + "/a/grafana-sessionreplay-app/app/" +
		url.PathEscape(appID) + "/session/" + url.PathEscape(sessionID)
}

func writeReplayBundleHeader(out io.Writer, appID, sessionID, outputFormat string) error {
	appJSON, err := json.Marshal(appID)
	if err != nil {
		return err
	}
	sessionJSON, err := json.Marshal(sessionID)
	if err != nil {
		return err
	}
	if outputFormat == "yaml" {
		_, err = fmt.Fprintf(out, "app_id: %s\nsession_id: %s\nrecordings:\n", appJSON, sessionJSON)
		return err
	}
	_, err = fmt.Fprintf(out, "{\"app_id\":%s,\"session_id\":%s,\"recordings\":[\n", appJSON, sessionJSON)
	return err
}

func writeReplayRecordingHeader(out io.Writer, recordingID string, outputFormat string, first bool) error {
	idJSON, err := json.Marshal(recordingID)
	if err != nil {
		return err
	}
	if outputFormat == "yaml" {
		_, err = fmt.Fprintf(out, "  - id: %s\n    events:\n", idJSON)
		return err
	}
	if !first {
		if _, err := io.WriteString(out, ",\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(out, "{\"id\":%s,\"events\":[\n", idJSON)
	return err
}

func writeReplayEvent(out io.Writer, encoder *json.Encoder, event RRWebEvent, outputFormat string, first bool) error {
	if outputFormat != "yaml" {
		if !first {
			if _, err := io.WriteString(out, ",\n"); err != nil {
				return err
			}
		}
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("encoding replay event: %w", err)
		}
		return nil
	}
	// JSON values are valid YAML 1.2 flow values. Indent each event as a block
	// sequence item so the file remains valid YAML without buffering the bundle.
	if _, err := io.WriteString(out, "      - "); err != nil {
		return err
	}
	if err := encoder.Encode(event); err != nil {
		return fmt.Errorf("encoding replay event: %w", err)
	}
	return nil
}

func writeReplayRecordingFooter(out io.Writer, outputFormat string, eventCount int) error {
	if outputFormat == "yaml" {
		if eventCount == 0 {
			_, err := io.WriteString(out, "      []\n")
			return err
		}
		return nil
	}
	_, err := io.WriteString(out, "]}")
	return err
}

func writeReplayBundleFooter(out io.Writer, outputFormat string) error {
	if outputFormat == "yaml" {
		return nil
	}
	_, err := io.WriteString(out, "]}\n")
	return err
}

// saveSessionReplayEvents bundles every recording, preserving boundaries and
// manifest segment order. The destination stays intact if any read fails.
func saveSessionReplayEvents(ctx context.Context, client *Client, appID, sessionID string, recordings []RecordingListItem, path, outputFormat string) (int, error) {
	manifests, err := fetchReplayManifests(ctx, client, appID, sessionID, recordings)
	if err != nil {
		return 0, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".gcx-replay-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("creating replay file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	out := bufio.NewWriterSize(tmp, 64*1024)
	if err := writeReplayBundleHeader(out, appID, sessionID, outputFormat); err != nil {
		return 0, err
	}
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	count := 0
	for i, recording := range recordings {
		manifest := manifests[i]
		if err := writeReplayRecordingHeader(out, recording.ID, outputFormat, i == 0); err != nil {
			return 0, err
		}
		recordingCount := 0
		// Keep each batch bounded so only ten segment payloads are held in
		// memory at once, even when a recording manifest is very large.
		for start := 0; start < len(manifest.Segments); start += replayFetchConcurrency {
			end := min(start+replayFetchConcurrency, len(manifest.Segments))
			segments, err := fetchReplaySegmentBatch(ctx, client, appID, sessionID, recording.ID, manifest.Segments[start:end])
			if err != nil {
				return 0, err
			}
			for _, segment := range segments {
				for _, event := range segment.Events {
					if err := writeReplayEvent(out, encoder, event, outputFormat, recordingCount == 0); err != nil {
						return 0, err
					}
					recordingCount++
					count++
				}
			}
		}
		if err := writeReplayRecordingFooter(out, outputFormat, recordingCount); err != nil {
			return 0, err
		}
	}
	if err := writeReplayBundleFooter(out, outputFormat); err != nil {
		return 0, err
	}
	if err := out.Flush(); err != nil {
		return 0, fmt.Errorf("flushing replay file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return 0, fmt.Errorf("installing replay file: %w", err)
	}
	return count, nil
}

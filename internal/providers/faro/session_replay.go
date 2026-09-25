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
	App  string
	Save string
}

func (o *sessionsGetReplayOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.App, "app", "", "Frontend Observability app slug-id or numeric id (required)")
	flags.StringVar(&o.Save, "save", "", "Path for the complete session replay JSON (required)")
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
	return nil
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
		Long: `Save every recording in a session to one JSON file. Each recording
contains its complete rrweb event stream, assembled from its segments in order.
Recording boundaries are preserved because separate recordings can overlap in time.`,
		Example: `  # Save the replay for a session to a private JSON file.
  gcx frontend sessions get-replay abc-session-123 --app my-web-app-42 --save replay.json`,
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
			count, err := saveSessionReplayEvents(ctx, client, appID, sessionID, recordings, opts.Save)
			if err != nil {
				return err
			}
			replayURL := sessionReplayURL(cfg.GrafanaURL, appID, sessionID)
			receipt := replayArtifactReceipt{
				ArtifactReceipt: cmdio.NewArtifactReceipt("get-replay", "json"),
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

// saveSessionReplayEvents bundles every recording, preserving boundaries and
// manifest segment order. The destination stays intact if any read fails.
func saveSessionReplayEvents(ctx context.Context, client *Client, appID, sessionID string, recordings []RecordingListItem, path string) (int, error) {
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
	if err := tmp.Chmod(0o600); err != nil {
		return 0, err
	}
	out := bufio.NewWriterSize(tmp, 64*1024)
	sessionJSON, err := json.Marshal(sessionID)
	if err != nil {
		return 0, err
	}
	appJSON, err := json.Marshal(appID)
	if err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(out, "{\"app_id\":%s,\"session_id\":%s,\"recordings\":[\n", appJSON, sessionJSON); err != nil {
		return 0, err
	}
	encoder := json.NewEncoder(out)
	count := 0
	for i, recording := range recordings {
		manifest := manifests[i]
		if i > 0 {
			if _, err := io.WriteString(out, ",\n"); err != nil {
				return 0, err
			}
		}
		idJSON, err := json.Marshal(recording.ID)
		if err != nil {
			return 0, err
		}
		if _, err := fmt.Fprintf(out, "{\"id\":%s,\"events\":[\n", idJSON); err != nil {
			return 0, err
		}
		recordingCount := 0
		for start := 0; start < len(manifest.Segments); start += replayFetchConcurrency {
			end := min(start+replayFetchConcurrency, len(manifest.Segments))
			segments, err := fetchReplaySegmentBatch(ctx, client, appID, sessionID, recording.ID, manifest.Segments[start:end])
			if err != nil {
				return 0, err
			}
			for _, segment := range segments {
				for _, event := range segment.Events {
					if recordingCount > 0 {
						if _, err := io.WriteString(out, ",\n"); err != nil {
							return 0, err
						}
					}
					if err := encoder.Encode(event); err != nil {
						return 0, fmt.Errorf("encoding replay event: %w", err)
					}
					recordingCount++
					count++
				}
			}
		}
		if _, err := io.WriteString(out, "]}"); err != nil {
			return 0, err
		}
	}
	if _, err := io.WriteString(out, "]}\n"); err != nil {
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

package faro

import (
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
)

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

	RecordingCount int    `json:"recording_count"`
	EventCount     int    `json:"event_count"`
	ReplayURL      string `json:"replay_url"`
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
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			appID := resolveAppID(opts.App)
			sessionID := strings.TrimSpace(args[0])
			list, err := client.ListRecordings(ctx, appID, sessionID, 0)
			if err != nil {
				return err
			}
			if len(list.Items) == 0 {
				return fmt.Errorf("no replay found for session %s", sessionID)
			}
			count, err := saveSessionReplayEvents(ctx, client, appID, sessionID, list.Items, opts.Save)
			if err != nil {
				return err
			}
			replayURL := sessionReplayURL(cfg.Host, appID, sessionID)
			receipt := replayArtifactReceipt{
				ArtifactReceipt: cmdio.NewArtifactReceipt("get-replay", "json"),
				RecordingCount:  len(list.Items),
				EventCount:      count,
				ReplayURL:       replayURL,
			}
			receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: opts.Save, Kind: "session-replay"})
			receipt.Summary = cmdio.MutationSummary{Succeeded: 1}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Wrote %d events from %d recordings to %s\nReplay: %s\n", count, len(list.Items), opts.Save, replayURL)
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
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gcx-replay-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("creating replay file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0o600); err != nil {
		return 0, err
	}
	sessionJSON, err := json.Marshal(sessionID)
	if err != nil {
		return 0, err
	}
	appJSON, err := json.Marshal(appID)
	if err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(tmp, "{\"app_id\":%s,\"session_id\":%s,\"recordings\":[\n", appJSON, sessionJSON); err != nil {
		return 0, err
	}
	encoder := json.NewEncoder(tmp)
	count := 0
	seen := make(map[string]struct{}, len(recordings))
	for i, recording := range recordings {
		if recording.ID == "" {
			return 0, errors.New("replay list contains a recording without an ID")
		}
		if _, exists := seen[recording.ID]; exists {
			return 0, fmt.Errorf("replay list repeats recording %s", recording.ID)
		}
		seen[recording.ID] = struct{}{}
		manifest, err := client.GetManifest(ctx, appID, sessionID, recording.ID)
		if err != nil {
			return 0, fmt.Errorf("fetching replay manifest %s: %w", recording.ID, err)
		}
		if manifest.ID != recording.ID || manifest.SessionID != sessionID {
			return 0, fmt.Errorf("replay manifest identity does not match session %s recording %s", sessionID, recording.ID)
		}
		if i > 0 {
			if _, err := io.WriteString(tmp, ",\n"); err != nil {
				return 0, err
			}
		}
		idJSON, err := json.Marshal(recording.ID)
		if err != nil {
			return 0, err
		}
		if _, err := fmt.Fprintf(tmp, "{\"id\":%s,\"events\":[\n", idJSON); err != nil {
			return 0, err
		}
		recordingCount := 0
		for _, metadata := range manifest.Segments {
			segment, err := client.GetSegment(ctx, appID, sessionID, recording.ID, strconv.FormatInt(metadata.ID, 10))
			if err != nil {
				return 0, fmt.Errorf("fetching replay segment %d of recording %s: %w", metadata.ID, recording.ID, err)
			}
			if segment.RecordingID != recording.ID {
				return 0, fmt.Errorf("replay segment %d belongs to a different recording", metadata.ID)
			}
			for _, event := range segment.Events {
				if recordingCount > 0 {
					if _, err := io.WriteString(tmp, ",\n"); err != nil {
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
		if _, err := io.WriteString(tmp, "]}"); err != nil {
			return 0, err
		}
	}
	if count == 0 {
		return 0, errors.New("replay contains no events")
	}
	if _, err := io.WriteString(tmp, "]}\n"); err != nil {
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return 0, fmt.Errorf("installing replay file: %w", err)
	}
	return count, nil
}

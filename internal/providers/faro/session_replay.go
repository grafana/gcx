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
	flags.StringVar(&o.Save, "save", "", "Path for the complete replay event JSON (required)")
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
		Short: "Save a playable replay for a Frontend Observability session.",
		Long: `Save the recording the Session Replay viewer opens by default as one
rrweb event JSON file. All of that recording's segments are included in order.
The returned viewer URL pins the selected recording, so later session activity
cannot change which replay it opens.`,
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
			list, err := client.ListRecordings(ctx, appID, sessionID, 1)
			if err != nil {
				return err
			}
			if len(list.Items) == 0 {
				return fmt.Errorf("no replay found for session %s", sessionID)
			}
			recordingID := list.Items[0].ID
			manifest, err := client.GetManifest(ctx, appID, sessionID, recordingID)
			if err != nil {
				return err
			}
			if manifest.ID != recordingID || manifest.SessionID != sessionID {
				return fmt.Errorf("replay manifest identity does not match session %s", sessionID)
			}
			count, err := saveReplayEvents(ctx, client, appID, sessionID, recordingID, manifest.Segments, opts.Save)
			if err != nil {
				return err
			}
			replayURL := pinnedReplayURL(cfg.Host, appID, sessionID, recordingID)
			receipt := replayArtifactReceipt{
				ArtifactReceipt: cmdio.NewArtifactReceipt("get-replay", "json"),
				EventCount:      count,
				ReplayURL:       replayURL,
			}
			receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: opts.Save, Kind: "rrweb-events"})
			receipt.Summary = cmdio.MutationSummary{Succeeded: 1}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Wrote %d replay events to %s\nReplay: %s\n", count, opts.Save, replayURL)
				return err
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func pinnedReplayURL(host, appID, sessionID, recordingID string) string {
	return strings.TrimRight(host, "/") + "/a/grafana-sessionreplay-app/app/" +
		url.PathEscape(appID) + "/session/" + url.PathEscape(sessionID) +
		"?recording_id=" + url.QueryEscape(recordingID)
}

// saveReplayEvents follows the viewer's manifest order and writes one playable
// rrweb event array. A temporary file keeps an existing destination intact if
// any segment cannot be fetched or encoded.
func saveReplayEvents(ctx context.Context, client *Client, appID, sessionID, recordingID string, segments []ManifestSegment, path string) (int, error) {
	if len(segments) == 0 {
		return 0, errors.New("replay manifest has no segments")
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
	if _, err := io.WriteString(tmp, "[\n"); err != nil {
		return 0, err
	}
	encoder := json.NewEncoder(tmp)
	count := 0
	for _, metadata := range segments {
		segment, err := client.GetSegment(ctx, appID, sessionID, recordingID, strconv.FormatInt(metadata.ID, 10))
		if err != nil {
			return 0, fmt.Errorf("fetching replay segment %d: %w", metadata.ID, err)
		}
		if segment.RecordingID != recordingID {
			return 0, fmt.Errorf("replay segment %d belongs to a different recording", metadata.ID)
		}
		for _, event := range segment.Events {
			if count > 0 {
				if _, err := io.WriteString(tmp, ",\n"); err != nil {
					return 0, err
				}
			}
			if err := encoder.Encode(event); err != nil {
				return 0, fmt.Errorf("encoding replay event: %w", err)
			}
			count++
		}
	}
	if count == 0 {
		return 0, errors.New("replay contains no events")
	}
	if _, err := io.WriteString(tmp, "]\n"); err != nil {
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

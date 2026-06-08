package faro

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

//go:embed session_player.html
var sessionPlayerHTMLTemplate string

// ValidateEventStream checks that events start with Meta (4) followed by
// FullSnapshot (2). Writes warnings to w.
func ValidateEventStream(events []RRWebEvent, w io.Writer) {
	if len(events) == 0 {
		_, _ = fmt.Fprintln(w, "Warning: event stream is empty.")
		return
	}
	if events[0].Type != 4 {
		_, _ = fmt.Fprintln(w, "Warning: event stream does not start with a Meta event (type 4). Replay may render a blank page.")
	}
	hasFullSnapshot := false
	for _, e := range events {
		if e.Type == 2 {
			hasFullSnapshot = true
			break
		}
	}
	if !hasFullSnapshot {
		_, _ = fmt.Fprintln(w, "Warning: event stream contains no FullSnapshot event (type 2). Replay may render a blank page.")
	}
}

// playerState holds the last known player state reported by the browser.
type playerState struct {
	mu    sync.RWMutex
	state json.RawMessage
}

func (ps *playerState) set(data json.RawMessage) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.state = data
}

func (ps *playerState) get() json.RawMessage {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.state == nil {
		return json.RawMessage(`{"timestamp":0,"playing":false,"duration":0}`)
	}
	return ps.state
}

//nolint:gochecknoglobals // Stateless upgrader reused across WebSocket connections.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
	},
}

// SessionPlayerServer manages the local replay server.
type SessionPlayerServer struct {
	events    json.RawMessage
	indexHTML []byte
	state     *playerState
	wsMu      sync.Mutex
	wsConn    *websocket.Conn
}

// NewSessionPlayerServer creates a session player server with the given events JSON.
func NewSessionPlayerServer(eventsJSON json.RawMessage, playerVersion string) (*SessionPlayerServer, error) {
	tmpl, err := template.New("player").Parse(sessionPlayerHTMLTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing player template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"PlayerVersion": playerVersion}); err != nil {
		return nil, fmt.Errorf("rendering player template: %w", err)
	}
	return &SessionPlayerServer{
		events:    eventsJSON,
		indexHTML: buf.Bytes(),
		state:     &playerState{},
	}, nil
}

func (s *SessionPlayerServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.indexHTML)
}

func (s *SessionPlayerServer) handleEvents(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.events)
}

func (s *SessionPlayerServer) handleState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(s.state.get())
}

func (s *SessionPlayerServer) handleControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var msg struct {
			Timestamp int64 `json:"timestamp,omitempty"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&msg)
		}
		cmd := map[string]any{"action": action}
		if action == "goto" {
			cmd["timestamp"] = msg.Timestamp
		}
		data, err := json.Marshal(cmd)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.wsMu.Lock()
		conn := s.wsConn
		if conn != nil {
			_ = conn.WriteMessage(websocket.TextMessage, data)
		}
		s.wsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func (s *SessionPlayerServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.wsMu.Lock()
	s.wsConn = conn
	s.wsMu.Unlock()
	defer func() {
		s.wsMu.Lock()
		if s.wsConn == conn {
			s.wsConn = nil
		}
		s.wsMu.Unlock()
		_ = conn.Close()
	}()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		s.state.set(message)
	}
}

// Handler returns the HTTP handler for the session player server.
func (s *SessionPlayerServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/goto", s.handleControl("goto"))
	mux.HandleFunc("POST /api/play", s.handleControl("play"))
	mux.HandleFunc("POST /api/pause", s.handleControl("pause"))
	mux.HandleFunc("GET /ws", s.handleWS)
	return mux
}

// ---------------------------------------------------------------------------
// play-session command
// ---------------------------------------------------------------------------

type playSessionOpts struct {
	Port          int
	Open          bool
	PlayerVersion string
}

func (o *playSessionOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.Port, "port", 0, "Port to serve on (0 = random available port)")
	flags.BoolVar(&o.Open, "open", false, "Open a browser automatically")
	flags.StringVar(&o.PlayerVersion, "player-version", "2.0.0-alpha.17", "rrweb-player version to load from CDN")
}

func newPlaySessionCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &playSessionOpts{}
	cmd := &cobra.Command{
		Use:   "play-session <app-name> <session-id>",
		Short: "Download and replay a session locally.",
		Long:  "Downloads all segments for a session and serves a local rrweb-player. Provides a control API for agent-driven playback via WebSocket.",
		Example: `  # Play a session (prints local URL).
  gcx frontend apps play-session my-web-app-42 abc-session-123

  # Open browser automatically.
  gcx frontend apps play-session my-web-app-42 abc-session-123 --open

  # Use a specific port.
  gcx frontend apps play-session my-web-app-42 abc-session-123 --port 8080`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			stderr := cmd.ErrOrStderr()

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

			recordingID, err := resolveFirstRecordingID(ctx, client, appID, sessionID)
			if err != nil {
				return err
			}
			cmdio.Info(stderr, "Using recording %s", recordingID)

			manifest, err := client.GetManifest(ctx, appID, sessionID, recordingID)
			if err != nil {
				return err
			}
			if len(manifest.Segments) == 0 {
				return fmt.Errorf("recording %s has no segments", recordingID)
			}

			cmdio.Info(stderr, "Downloading %d segments...", len(manifest.Segments))

			allSegments := make([]*RecordingSegmentResponse, len(manifest.Segments))
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(10)

			for i, seg := range manifest.Segments {
				g.Go(func() error {
					segResp, err := client.GetSegment(gctx, appID, sessionID, recordingID, strconv.FormatInt(seg.ID, 10))
					if err != nil {
						return fmt.Errorf("downloading segment %d: %w", seg.ID, err)
					}
					allSegments[i] = segResp
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				return err
			}

			var allEvents []RRWebEvent
			for _, seg := range allSegments {
				allEvents = append(allEvents, seg.Events...)
			}

			ValidateEventStream(allEvents, stderr)

			eventsJSON, err := json.Marshal(allEvents)
			if err != nil {
				return fmt.Errorf("marshaling events: %w", err)
			}

			srv, err := NewSessionPlayerServer(eventsJSON, opts.PlayerVersion)
			if err != nil {
				return err
			}

			var lc net.ListenConfig
			listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
			if err != nil {
				return fmt.Errorf("starting server: %w", err)
			}

			addr, ok := listener.Addr().(*net.TCPAddr)
			if !ok {
				return fmt.Errorf("unexpected listener address type: %T", listener.Addr())
			}
			localURL := fmt.Sprintf("http://localhost:%d", addr.Port)
			cmdio.Success(stderr, "Serving session replay at %s", localURL)
			cmdio.Info(stderr, "Press Ctrl-C to stop.")

			if opts.Open {
				openBrowser(localURL)
			}

			httpServer := &http.Server{
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
			}

			sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			go func() {
				<-sigCtx.Done()
				_ = httpServer.Close()
			}()

			log := logging.FromContext(ctx)
			if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				log.Error("Server error", "error", err)
				return err
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(context.Background(), "open", url)
	case "windows":
		cmd = exec.CommandContext(context.Background(), "cmd", "/c", "start", url)
	default:
		cmd = exec.CommandContext(context.Background(), "xdg-open", url)
	}
	_ = cmd.Start()
}

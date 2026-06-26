package faro

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logfmt/logfmt"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SessionListRow holds data for one session in the list output.
type SessionListRow struct {
	SessionID string `json:"session_id"`
	Browser   string `json:"browser"`
	AppName   string `json:"app_name"`
	LastSeen  string `json:"last_seen"`
}

// SessionListCodec renders []SessionListRow as a table.
type SessionListCodec struct{}

func (c *SessionListCodec) Format() format.Format { return FormatText }

func (c *SessionListCodec) Encode(w io.Writer, v any) error {
	rows, ok := v.([]SessionListRow)
	if !ok {
		return fmt.Errorf("invalid data type for session list codec: expected []SessionListRow, got %T", v)
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "No sessions with recordings found.")
		return err
	}
	t := style.NewTable("SESSION ID", "BROWSER", "APP NAME", "LAST SEEN")
	for _, r := range rows {
		t.Row(r.SessionID, r.Browser, r.AppName, r.LastSeen)
	}
	return t.Render(w)
}

func (c *SessionListCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

type listSessionsOpts struct {
	IO         cmdio.Options
	Datasource string
	Since      string
	Limit      int
}

func (o *listSessionsOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Loki datasource UID (auto-discovered if omitted)")
	flags.StringVar(&o.Since, "since", "1h", "How far back to search (e.g., 1h, 24h, 7d)")
	flags.IntVar(&o.Limit, "limit", 1000, "Maximum log lines to scan")
	o.IO.RegisterCustomCodec("text", &SessionListCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newListSessionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listSessionsOpts{}
	cmd := &cobra.Command{
		Use:   "list-sessions <app-name>",
		Short: "List sessions that have replay recordings.",
		Long:  "Queries Loki for faro.session_recording.started events to discover sessions with replay data.",
		Example: `  # List sessions with replays in the last hour.
  gcx frontend apps list-sessions my-web-app-42

  # Search the last 24 hours.
  gcx frontend apps list-sessions my-web-app-42 --since 24h

  # Use a specific Loki datasource.
  gcx frontend apps list-sessions my-web-app-42 -d P8E80F9AEF21F6940`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				return err
			}
			cfgCtx := fullCfg.Contexts[fullCfg.CurrentContext]

			dsUID, _, err := dsquery.ResolveValidateAndSaveDatasource(
				ctx, loader, opts.Datasource, cfgCtx, cfg, "loki",
			)
			if err != nil {
				return fmt.Errorf("resolving Loki datasource: %w", err)
			}

			lokiClient, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("creating Loki client: %w", err)
			}

			appID := resolveAppID(args[0])
			escapedAppID := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(appID)
			query := fmt.Sprintf(`{app_id="%s"} | logfmt | event_name=%sfaro.session_recording.started%s`, escapedAppID, "`", "`")

			since, err := parseDuration(opts.Since)
			if err != nil {
				return fmt.Errorf("invalid --since value: %w", err)
			}

			now := time.Now()
			req := loki.QueryRequest{
				Query: query,
				Start: now.Add(-since),
				End:   now,
				Limit: opts.Limit,
			}

			resp, err := lokiClient.Query(ctx, dsUID, req)
			if err != nil {
				return fmt.Errorf("querying Loki: %w", err)
			}

			rows := ExtractSessionRows(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), rows)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ExtractSessionRows parses Loki stream results into deduplicated session rows.
// When a session appears multiple times, the most recent entry wins.
// Results are sorted by last seen time (most recent first).
func ExtractSessionRows(resp *loki.QueryResponse) []SessionListRow {
	type sessionInfo struct {
		browser  string
		appName  string
		lastSeen time.Time
	}

	sessions := make(map[string]*sessionInfo)

	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			if len(entry) < 2 {
				continue
			}
			fields := parseLogfmtLine(entry[1])
			sid := fields["session_id"]
			if sid == "" {
				continue
			}

			ts := parseNanoTimestamp(entry[0])
			browser := fields["browser_name"]
			if v := fields["browser_version"]; v != "" {
				browser += " " + v
			}

			existing, ok := sessions[sid]
			if !ok || ts.After(existing.lastSeen) {
				sessions[sid] = &sessionInfo{
					browser:  browser,
					appName:  fields["app_name"],
					lastSeen: ts,
				}
			}
		}
	}

	rows := make([]SessionListRow, 0, len(sessions))
	for sid, info := range sessions {
		rows = append(rows, SessionListRow{
			SessionID: sid,
			Browser:   info.browser,
			AppName:   info.appName,
			LastSeen:  info.lastSeen.UTC().Format(time.RFC3339),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].LastSeen != rows[j].LastSeen {
			return rows[i].LastSeen > rows[j].LastSeen
		}
		return rows[i].SessionID < rows[j].SessionID
	})

	return rows
}

func parseLogfmtLine(line string) map[string]string {
	fields := make(map[string]string)
	d := logfmt.NewDecoder(strings.NewReader(line))
	for d.ScanRecord() {
		for d.ScanKeyval() {
			fields[string(d.Key())] = string(d.Value())
		}
	}
	return fields
}

func parseNanoTimestamp(s string) time.Time {
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// parseDuration parses durations like "1h", "24h", "7d".
func parseDuration(s string) (time.Duration, error) {
	if dayStr, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(dayStr)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

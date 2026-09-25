package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// replaySessionListRow holds data for one regular session ID with replay recordings.
type replaySessionListRow struct {
	SessionID string `json:"session_id"`
	Browser   string `json:"browser"`
	AppName   string `json:"app_name"`
	LastSeen  string `json:"last_seen"`
}

func replaySessionTable() cmdio.Table[replaySessionListRow] {
	return cmdio.Table[replaySessionListRow]{
		Columns: []cmdio.Column[replaySessionListRow]{
			{Header: "SESSION ID", Content: func(r replaySessionListRow) string { return r.SessionID }},
			{Header: "BROWSER", Content: func(r replaySessionListRow) string { return r.Browser }},
			{Header: "APP NAME", Content: func(r replaySessionListRow) string { return r.AppName }},
			{Header: "LAST SEEN", Content: func(r replaySessionListRow) string { return r.LastSeen }},
		},
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No session replays found.")
			return err
		},
	}
}

type listReplaySessionsOpts struct {
	IO            cmdio.Options
	Datasource    string
	Since         string
	Limit         int
	sinceDuration time.Duration
}

func parseReplayAppID(name string) (string, error) {
	appID := resolveAppID(name)
	if _, err := pinot.FormatSQLInt(appID); err != nil {
		return "", fmt.Errorf("invalid app id %q: expected a numeric ID or slug-id", name)
	}
	return appID, nil
}

func (o *listReplaySessionsOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Loki or Pinot datasource UID (Loki auto-discovered if omitted)")
	flags.StringVar(&o.Since, "since", "1h", "How far back to search (e.g., 1h, 24h, 7d)")
	flags.IntVar(&o.Limit, "limit", 1000, "Maximum replay-start events to scan (Loki caps at 1000; not the number of sessions)")
	cmdio.RegisterTableAs(&o.IO, replaySessionTable(), cmdio.FormatText)
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(flags)
}

func (o *listReplaySessionsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit <= 0 {
		return errors.New("--limit must be positive")
	}
	since, err := shared.ParseDuration(o.Since)
	if err != nil {
		return fmt.Errorf("invalid --since value: %w", err)
	}
	if since <= 0 {
		return errors.New("--since must be positive")
	}
	o.sinceDuration = since
	return nil
}

func newListReplaySessionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listReplaySessionsOpts{}
	cmd := &cobra.Command{
		Use:   "list-replay-sessions <slug-id>",
		Short: "List Frontend Observability sessions that have replay recordings.",
		Long:  "Discovers regular session IDs that have replay recordings by querying Loki or Pinot for faro.session_recording.started events. This does not list all Frontend Observability sessions. The default datasource is Loki; pass a Pinot datasource UID with -d to query Pinot.",
		Example: `  # List regular session IDs with replay recordings in the last hour.
  gcx frontend apps list-replay-sessions my-web-app-42

  # Search the last 24 hours.
  gcx frontend apps list-replay-sessions my-web-app-42 --since 24h

  # Use a specific Loki or Pinot datasource.
  gcx frontend apps list-replay-sessions my-web-app-42 -d P8E80F9AEF21F6940`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			appID, err := parseReplayAppID(args[0])
			if err != nil {
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

			now := time.Now()
			start := now.Add(-opts.sinceDuration)
			var rows []replaySessionListRow
			var atLimit bool
			isLoki := opts.Datasource == ""
			if opts.Datasource == "" {
				dsUID, _, resolveErr := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, "", cfgCtx, cfg, datasourceLoki)
				if resolveErr != nil {
					return fmt.Errorf("resolving Loki datasource: %w", resolveErr)
				}
				rows, atLimit, err = queryLokiReplaySessions(ctx, cfg, dsUID, appID, start, now, opts.Limit)
			} else {
				dsType, typeErr := dsquery.GetDatasourceType(ctx, cfg, opts.Datasource)
				if typeErr != nil {
					return typeErr
				}
				kind, kindErr := sessionKindFromDatasourceType(dsType)
				if kindErr != nil {
					return kindErr
				}
				switch kind {
				case datasourceLoki:
					isLoki = true
					rows, atLimit, err = queryLokiReplaySessions(ctx, cfg, opts.Datasource, appID, start, now, opts.Limit)
				case datasourcePinot:
					rows, atLimit, err = queryPinotReplaySessions(ctx, cfg, opts.Datasource, appID, start, now, opts.Limit)
				}
			}
			if err != nil {
				return err
			}
			if atLimit {
				if opts.Limit >= lokiEventsPageSize && isLoki {
					cmdio.Warning(cmd.ErrOrStderr(), "Result may be incomplete after scanning %d replay-start events; Loki may clamp larger --limit values. Narrow --since or use Pinot.", lokiEventsPageSize)
				} else {
					cmdio.Warning(cmd.ErrOrStderr(), "Result may be incomplete after scanning --limit %d replay-start events; increase --limit and retry.", opts.Limit)
				}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), rows)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func queryLokiReplaySessions(ctx context.Context, cfg config.NamespacedRESTConfig, uid, appID string, start, end time.Time, limit int) ([]replaySessionListRow, bool, error) {
	client, err := loki.NewClient(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("creating Loki client: %w", err)
	}
	effectiveLimit := min(limit, lokiEventsPageSize)
	query := lokiReplayDiscoveryQuery(appID)
	resp, err := client.Query(ctx, uid, loki.QueryRequest{Query: query, Start: start, End: end, Limit: effectiveLimit})
	if err != nil {
		return nil, false, fmt.Errorf("querying Loki: %w", err)
	}
	events := 0
	for _, stream := range resp.Data.Result {
		events += len(stream.Values)
	}
	return extractReplaySessionRows(resp), events >= effectiveLimit, nil
}

func queryPinotReplaySessions(ctx context.Context, cfg config.NamespacedRESTConfig, uid, appID string, start, end time.Time, limit int) ([]replaySessionListRow, bool, error) {
	client, err := pinot.NewClient(cfg)
	if err != nil {
		return nil, false, fmt.Errorf("creating Pinot client: %w", err)
	}
	query, err := pinotReplayStartsQuery(appID, cfg.GrafanaURL, limit)
	if err != nil {
		return nil, false, err
	}
	resp, err := client.Query(ctx, uid, pinot.QueryRequest{RawSQL: query, TableName: pinotEventsTable(cfg.GrafanaURL), Start: start, End: end})
	if err != nil {
		return nil, false, fmt.Errorf("querying Pinot: %w", err)
	}
	rows, err := extractPinotReplaySessionRows(resp)
	return rows, resp != nil && len(resp.Rows) >= limit, err
}

func extractPinotReplaySessionRows(resp *querysql.QueryResponse) ([]replaySessionListRow, error) {
	rows := make([]replaySessionListRow, 0)
	if resp == nil || len(resp.Rows) == 0 {
		return rows, nil
	}
	columns := make(map[string]int, len(resp.Columns))
	for i, column := range resp.Columns {
		columns[column.Name] = i
	}
	for _, name := range []string{"session_id", "last_seen", "browser_name", "browser_version", "app_name"} {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("pinot replay sessions response missing %s column", name)
		}
	}
	seen := make(map[string]struct{}, len(resp.Rows))
	for _, row := range resp.Rows {
		if len(row) < len(resp.Columns) {
			return nil, errors.New("pinot replay sessions response has an incomplete row")
		}
		sessionID, _ := row[columns["session_id"]].(string)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		lastSeen, ok := pinotInt64(row[columns["last_seen"]])
		if !ok || lastSeen <= 0 {
			return nil, fmt.Errorf("pinot replay session %s has invalid timestamp", sessionID)
		}
		stringCell := func(name string) string {
			value, _ := row[columns[name]].(string)
			return value
		}
		browser := strings.TrimSpace(stringCell("browser_name") + " " + stringCell("browser_version"))
		rows = append(rows, replaySessionListRow{
			SessionID: sessionID,
			Browser:   browser,
			AppName:   stringCell("app_name"),
			LastSeen:  time.UnixMilli(lastSeen).UTC().Format(time.RFC3339),
		})
		seen[sessionID] = struct{}{}
	}
	return rows, nil
}

// extractReplaySessionRows parses Loki stream results into deduplicated replay session rows.
// When a session appears multiple times, the most recent entry wins.
// Results are sorted by last seen time (most recent first).
func extractReplaySessionRows(resp *loki.QueryResponse) []replaySessionListRow {
	type sessionInfo struct {
		browser  string
		appName  string
		lastSeen time.Time
	}

	sessions := make(map[string]*sessionInfo)

	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			fields := parseLogfmt(entry.Line)
			sid := fields["session_id"]
			if sid == "" {
				continue
			}

			ts, _ := parseLokiUnixNano(entry.Timestamp)
			browser := fields["browser_name"]
			if v := fields["browser_version"]; v != "" {
				browser = strings.TrimSpace(browser + " " + v)
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

	rows := make([]replaySessionListRow, 0, len(sessions))
	for sid, info := range sessions {
		rows = append(rows, replaySessionListRow{
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

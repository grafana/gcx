package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const pinotReplaySessionsSafetyCap = pinotJourneyMaxPages * pinotJourneyPageSize

const replayLokiDiscoveryMaxPages = 100

const replayLokiSessionsSafetyCap = replayLokiDiscoveryMaxPages * lokiEventsPageSize

// replaySessionListRow holds data for one regular session ID with replay recordings.
type replaySessionListRow struct {
	SessionID string `json:"session_id"`
	Browser   string `json:"browser"`
	AppName   string `json:"app_name"`
	LastSeen  string `json:"last_seen"`
}

type replaySessionListResult struct {
	AppID    string                 `json:"app_id" yaml:"app_id"`
	Items    []replaySessionListRow `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta        `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

func (replaySessionListResult) ListItemsKey() string { return "items" }

type replaySessionTableCodec struct{ table format.Codec }

func (c replaySessionTableCodec) Format() format.Format { return c.table.Format() }

func (c replaySessionTableCodec) Decode(r io.Reader, v any) error { return c.table.Decode(r, v) }

func (c replaySessionTableCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(replaySessionListResult)
	if !ok {
		return fmt.Errorf("replay session table: expected replaySessionListResult, got %T", v)
	}
	if len(result.Items) == 0 {
		_, err := fmt.Fprintf(w, "No session replays found for app ID %s.\n", result.AppID)
		return err
	}
	return c.table.Encode(w, result.Items)
}

func replaySessionTable() cmdio.Table[replaySessionListRow] {
	return cmdio.Table[replaySessionListRow]{
		Columns: []cmdio.Column[replaySessionListRow]{
			{Header: "SESSION ID", Content: func(r replaySessionListRow) string { return r.SessionID }},
			{Header: "BROWSER", Content: func(r replaySessionListRow) string { return r.Browser }},
			{Header: "APP NAME", Content: func(r replaySessionListRow) string { return r.AppName }},
			{Header: "LAST SEEN", Content: func(r replaySessionListRow) string { return r.LastSeen }},
		},
	}
}

type listReplaySessionsOpts struct {
	dsquery.TimeRangeOpts

	IO            cmdio.Options
	Datasource    string
	Limit         int
	datasourceSet bool
}

func parseReplayAppID(name string) (string, error) {
	appID := resolveAppID(name)
	if _, err := strconv.ParseInt(appID, 10, 64); err != nil {
		return "", fmt.Errorf("invalid app id %q: expected a numeric ID or slug-id, e.g. my-web-app-42 or 42 (find it with: gcx frontend apps list)", name)
	}
	return appID, nil
}

func (o *listReplaySessionsOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Loki or Pinot datasource UID (Loki auto-discovered if omitted)")
	o.SetupTimeFlags(flags)
	o.IO.RegisterCustomCodec(cmdio.FormatText, replaySessionTableCodec{table: replaySessionTable().Codec(cmdio.FormatText)})
	o.IO.DefaultFormat(cmdio.FormatText)
	flags.IntVar(&o.Limit, "limit", 1000, fmt.Sprintf("Maximum sessions to return. 0 reads up to %d Loki replay-start events or %d Pinot sessions", replayLokiSessionsSafetyCap, pinotReplaySessionsSafetyCap))
	o.IO.BindFlags(flags)
}

func (o *listReplaySessionsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 0 {
		return fmt.Errorf("invalid --limit %d: must be >= 0 (0 reads up to %d Loki replay-start events or %d Pinot sessions)", o.Limit, replayLokiSessionsSafetyCap, pinotReplaySessionsSafetyCap)
	}
	o.Datasource = strings.TrimSpace(o.Datasource)
	if o.datasourceSet && o.Datasource == "" {
		return errors.New("--datasource cannot be empty")
	}
	if err := o.ValidateTimeRange(); err != nil {
		return err
	}
	if !o.IsRange() {
		return errors.New("--since or --from/--to is required")
	}
	return nil
}

func newListReplaySessionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listReplaySessionsOpts{}
	cmd := &cobra.Command{
		Use:   "list-replay-sessions <slug-id-or-numeric-id>",
		Short: "List Frontend Observability sessions that have replay recordings.",
		Long:  fmt.Sprintf("Discovers regular session IDs that have replay recordings by querying Loki or Pinot for faro.session_recording.started events. This does not list all Frontend Observability sessions. The default datasource is Loki; pass a Pinot datasource UID with -d to query Pinot. Loki scans at most %d replay-start events and applies a 60s timeout per query. An empty result means no replay-start event was found for the app ID and time window; this command does not verify that the app exists. JSON output has an items envelope and includes list_meta when more sessions are available.", replayLokiSessionsSafetyCap),
		Example: `  # List regular session IDs with replay recordings in the last hour.
  gcx frontend apps list-replay-sessions my-web-app-42 --since 1h

  # Search the last 24 hours.
  gcx frontend apps list-replay-sessions my-web-app-42 --since 24h

  # Search an absolute time range.
  gcx frontend apps list-replay-sessions my-web-app-42 --from 2026-09-01T00:00:00Z --to 2026-09-02T00:00:00Z

  # Use a specific Loki or Pinot datasource.
  gcx frontend apps list-replay-sessions my-web-app-42 -d P8E80F9AEF21F6940`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.datasourceSet = cmd.Flags().Changed("datasource")
			if err := opts.Validate(); err != nil {
				return err
			}
			appID, err := parseReplayAppID(args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			start, end, err := opts.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}
			var rows []replaySessionListRow
			var meta *cmdio.ListMeta
			if opts.Datasource == "" {
				dsUID, _, resolveErr := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, "", cfgCtx, cfg, datasourceLoki)
				if resolveErr != nil {
					return fmt.Errorf("resolving Loki datasource: %w", resolveErr)
				}
				rows, meta, err = queryLokiReplaySessions(ctx, cfg, dsUID, appID, start, end, opts.Limit)
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
					rows, meta, err = queryLokiReplaySessions(ctx, cfg, opts.Datasource, appID, start, end, opts.Limit)
				case datasourcePinot:
					rows, meta, err = queryPinotReplaySessions(ctx, cfg, opts.Datasource, appID, start, end, opts.Limit)
				default:
					return fmt.Errorf("unsupported replay datasource kind %q", kind)
				}
			}
			if err != nil {
				return err
			}
			if rows == nil {
				rows = []replaySessionListRow{}
			}
			result := replaySessionListResult{AppID: appID, Items: rows}
			result.ListMeta = cmdio.AttachListMeta(meta, os.Args)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), result.ListMeta)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func queryLokiReplaySessions(ctx context.Context, cfg config.NamespacedRESTConfig, uid, appID string, start, end time.Time, limit int) ([]replaySessionListRow, *cmdio.ListMeta, error) {
	client, err := loki.NewClient(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("creating Loki client: %w", err)
	}
	resp, stopped, capped, err := fetchLokiReplayDiscoveryPages(ctx, client, uid, lokiReplayDiscoveryQuery(appID), start, end, sessionLokiQueryTimeout, replaySessionLimitStop(limit))
	if err != nil {
		return nil, nil, wrapLokiReplayDiscoveryErr(err)
	}
	rows := extractReplaySessionRows(resp)
	if capped {
		page, meta := rows, cmdio.PagedListMeta(len(rows), limit, true, replayLokiSessionsSafetyCap)
		return page, meta, nil
	}
	if stopped {
		page, meta := cmdio.TruncatePagedList(rows, limit)
		return page, meta, nil
	}
	page, meta := cmdio.TruncateCompleteList(rows, limit)
	return page, meta, nil
}

func fetchLokiReplayDiscoveryPages(ctx context.Context, client lokiQuerier, uid, query string, start, end time.Time, timeout time.Duration, stop func(*loki.QueryResponse) bool) (*loki.QueryResponse, bool, bool, error) {
	return fetchLokiEventPagesUntilWithPageCap(ctx, client, uid, query, start, end, timeout, stop, replayLokiDiscoveryMaxPages)
}

func wrapLokiReplayDiscoveryErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("loki replay discovery timed out after %s (scan did not finish; this is not an empty result); try a narrower --from/--to or a Pinot datasource UID (-d)", sessionLokiQueryTimeout)
	}
	return fmt.Errorf("querying Loki: %w", err)
}

func replaySessionLimitStop(limit int) func(*loki.QueryResponse) bool {
	seen := make(map[string]struct{})
	return func(page *loki.QueryResponse) bool {
		for _, stream := range page.Data.Result {
			for _, entry := range stream.Values {
				if _, valid := parseLokiUnixNano(entry.Timestamp); !valid {
					continue
				}
				if sessionID := parseLogfmt(entry.Line)["session_id"]; sessionID != "" {
					seen[sessionID] = struct{}{}
				}
			}
		}
		return limit > 0 && len(seen) > limit
	}
}

func queryPinotReplaySessions(ctx context.Context, cfg config.NamespacedRESTConfig, uid, appID string, start, end time.Time, limit int) ([]replaySessionListRow, *cmdio.ListMeta, error) {
	client, err := pinot.NewClient(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("creating Pinot client: %w", err)
	}
	return fetchPinotReplaySessions(ctx, client, uid, appID, cfg.GrafanaURL, start, end, limit)
}

func fetchPinotReplaySessions(ctx context.Context, client pinotQuerier, uid, appID, serverURL string, start, end time.Time, limit int) ([]replaySessionListRow, *cmdio.ListMeta, error) {
	pageSize := pinotJourneyPageSize
	if limit > 0 && limit < pageSize {
		pageSize = limit + 1
	}
	rows := make([]replaySessionListRow, 0)
	seen := make(map[string]struct{})
	for pageNum := range pinotJourneyMaxPages {
		offset := pageNum * pageSize
		query, err := pinotReplayStartsQuery(appID, serverURL, pageSize, offset)
		if err != nil {
			return nil, nil, err
		}
		resp, err := client.Query(ctx, uid, pinot.QueryRequest{RawSQL: query, TableName: pinotEventsTable(serverURL), Start: start, End: end})
		if err != nil {
			return nil, nil, fmt.Errorf("querying Pinot: %w", err)
		}
		page, err := extractPinotReplaySessionRows(resp, seen)
		if err != nil {
			return nil, nil, err
		}
		if len(page) == 0 && resp != nil && len(resp.Rows) == pageSize {
			return nil, nil, fmt.Errorf("pinot replay sessions page %d repeated without new sessions", pageNum+1)
		}
		rows = append(rows, page...)
		if limit > 0 && len(rows) > limit {
			items, meta := cmdio.TruncatePagedList(rows, limit)
			return items, meta, nil
		}
		if resp == nil || len(resp.Rows) < pageSize {
			return rows, nil, nil
		}
	}
	return rows, cmdio.PagedListMeta(len(rows), limit, true, pinotReplaySessionsSafetyCap), nil
}

func extractPinotReplaySessionRows(resp *querysql.QueryResponse, seen map[string]struct{}) ([]replaySessionListRow, error) {
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

			ts, valid := parseLokiUnixNano(entry.Timestamp)
			if !valid {
				continue
			}
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

	type timedRow struct {
		row      replaySessionListRow
		lastSeen time.Time
	}
	ordered := make([]timedRow, 0, len(sessions))
	for sid, info := range sessions {
		ordered = append(ordered, timedRow{
			row: replaySessionListRow{
				SessionID: sid,
				Browser:   info.browser,
				AppName:   info.appName,
				LastSeen:  info.lastSeen.UTC().Format(time.RFC3339),
			},
			lastSeen: info.lastSeen,
		})
	}

	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].lastSeen.Equal(ordered[j].lastSeen) {
			return ordered[i].lastSeen.After(ordered[j].lastSeen)
		}
		return ordered[i].row.SessionID < ordered[j].row.SessionID
	})
	rows := make([]replaySessionListRow, len(ordered))
	for i := range ordered {
		rows[i] = ordered[i].row
	}
	return rows
}

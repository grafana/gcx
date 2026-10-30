package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// replaySessionListRow holds data for one regular session ID with replay recordings.
type replaySessionListRow struct {
	SessionID        string `json:"session_id"`
	Browser          string `json:"browser"`
	AppName          string `json:"app_name"`
	RecordingStarted string `json:"recording_started"`
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
			{Header: "RECORDING STARTED", Content: func(r replaySessionListRow) string { return r.RecordingStarted }},
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
	appID, _ := adapter.ExtractIDFromSlug(name)
	if _, valid := adapter.ExtractInt64IDFromSlug(name); !valid {
		return "", fmt.Errorf("invalid app id %q: expected a numeric ID or slug-id, e.g. my-web-app-42 or 42 (find it with: gcx frontend apps list)", name)
	}
	return appID, nil
}

func (o *listReplaySessionsOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Loki or Pinot datasource UID (Loki auto-discovered if omitted)")
	o.SetupTimeFlags(flags)
	o.IO.RegisterCustomCodec(cmdio.FormatText, replaySessionTableCodec{table: replaySessionTable().Codec(cmdio.FormatText)})
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindListLimit(flags, &o.Limit, "sessions", 1000)
	o.IO.BindFlags(flags)
}

func (o *listReplaySessionsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
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
		Long:  "Discovers regular session IDs that have replay recordings by querying Loki or Pinot for faro.session_recording.started events. RECORDING STARTED is the latest replay recording start event observed in the selected time range, not general session activity. This does not list all Frontend Observability sessions. Loki is auto-discovered when -d is omitted; pass a Loki or Pinot UID with -d to choose one. Loki applies a 60s timeout per query. An empty result means no replay-start event was found for the app ID and time window; this command does not verify that the app exists. JSON output has an items envelope and includes list_meta when more sessions are available.",
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
					return fmt.Errorf("unsupported replay session datasource kind %q", kind)
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
	accumulator := replaySessionAccumulator{sessions: make(map[string]replaySessionInfo)}
	pageSize := lokiEventsPageSize
	stop := func() bool {
		return limit > 0 && len(accumulator.sessions) > limit
	}
	_, stopped, err := fetchLokiEventPagesUntil(ctx, client, uid, lokiReplayDiscoveryQuery(appID), start, end, sessionLokiQueryTimeout, pageSize, stop, accumulator.addPage)
	if err != nil {
		return nil, nil, wrapLokiReplayDiscoveryErr(err)
	}
	rows := accumulator.rows()
	if stopped {
		page, meta := cmdio.TruncatePagedList(rows, limit)
		return page, meta, nil
	}
	page, meta := cmdio.TruncateCompleteList(rows, limit)
	return page, meta, nil
}

func wrapLokiReplayDiscoveryErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("loki replay discovery timed out after %s (scan did not finish; this is not an empty result); try a narrower --from/--to or a Pinot datasource UID (-d)", sessionLokiQueryTimeout)
	}
	return fmt.Errorf("querying Loki: %w", err)
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
	for pageNum := 0; ; pageNum++ {
		offset := pageNum * pageSize
		query, err := pinotReplayStartsQuery(appID, serverURL, pageSize, offset)
		if err != nil {
			return nil, nil, err
		}
		resp, err := client.Query(ctx, uid, pinot.QueryRequest{RawSQL: query, TableName: pinotEventsTable(serverURL), Start: start, End: end})
		if err != nil {
			return nil, nil, fmt.Errorf("querying Pinot: %w", err)
		}
		page, hasUsableRows, err := extractPinotReplaySessionRows(resp, seen)
		if err != nil {
			return nil, nil, err
		}
		if len(page) == 0 && resp != nil && len(resp.Rows) == pageSize {
			if !hasUsableRows {
				return nil, nil, fmt.Errorf("pinot replay sessions page %d contained no usable sessions (session IDs were empty or recording start times invalid)", pageNum+1)
			}
			return nil, nil, fmt.Errorf("pinot replay sessions page %d repeated without new sessions", pageNum+1)
		}
		rows = append(rows, page...)
		if resp == nil || len(resp.Rows) < pageSize {
			items, meta := cmdio.TruncateCompleteList(rows, limit)
			return items, meta, nil
		}
		if limit > 0 && len(rows) > limit {
			items, meta := cmdio.TruncatePagedList(rows, limit)
			return items, meta, nil
		}
	}
}

func extractPinotReplaySessionRows(resp *querysql.QueryResponse, seen map[string]struct{}) ([]replaySessionListRow, bool, error) {
	rows := make([]replaySessionListRow, 0)
	if resp == nil || len(resp.Rows) == 0 {
		return rows, false, nil
	}
	columns := make(map[string]int, len(resp.Columns))
	for i, column := range resp.Columns {
		columns[column.Name] = i
	}
	for _, name := range []string{"session_id", "recording_started", "browser_name", "browser_version", "app_name"} {
		if _, ok := columns[name]; !ok {
			return nil, false, fmt.Errorf("pinot replay sessions response missing %s column", name)
		}
	}
	hasUsableRows := false
	for _, row := range resp.Rows {
		if len(row) < len(resp.Columns) {
			return nil, false, errors.New("pinot replay sessions response has an incomplete row")
		}
		sessionID, _ := row[columns["session_id"]].(string)
		if sessionID == "" {
			continue
		}
		recordingStarted, ok := pinotInt64(row[columns["recording_started"]])
		if !ok || recordingStarted <= 0 {
			continue
		}
		hasUsableRows = true
		if _, ok := seen[sessionID]; ok {
			continue
		}
		stringCell := func(name string) string {
			value, _ := row[columns[name]].(string)
			return value
		}
		browser := strings.TrimSpace(stringCell("browser_name") + " " + stringCell("browser_version"))
		rows = append(rows, replaySessionListRow{
			SessionID:        sessionID,
			Browser:          browser,
			AppName:          stringCell("app_name"),
			RecordingStarted: time.UnixMilli(recordingStarted).UTC().Format(time.RFC3339),
		})
		seen[sessionID] = struct{}{}
	}
	return rows, hasUsableRows, nil
}

type replaySessionInfo struct {
	row              replaySessionListRow
	recordingStarted time.Time
}

type replaySessionAccumulator struct {
	sessions map[string]replaySessionInfo
}

func (a *replaySessionAccumulator) addPage(resp *loki.QueryResponse) {
	if resp == nil {
		return
	}
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
			if existing, ok := a.sessions[sid]; ok && !ts.After(existing.recordingStarted) {
				continue
			}
			a.sessions[sid] = replaySessionInfo{
				row: replaySessionListRow{
					SessionID:        sid,
					Browser:          browser,
					AppName:          fields["app_name"],
					RecordingStarted: ts.UTC().Format(time.RFC3339),
				},
				recordingStarted: ts,
			}
		}
	}
}

func (a *replaySessionAccumulator) rows() []replaySessionListRow {
	ordered := make([]replaySessionInfo, 0, len(a.sessions))
	for _, info := range a.sessions {
		ordered = append(ordered, info)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].recordingStarted.Equal(ordered[j].recordingStarted) {
			return ordered[i].recordingStarted.After(ordered[j].recordingStarted)
		}
		return ordered[i].row.SessionID < ordered[j].row.SessionID
	})
	rows := make([]replaySessionListRow, len(ordered))
	for i := range ordered {
		rows[i] = ordered[i].row
	}
	return rows
}

package faro

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const (
	errorsSortCount    = "count"
	errorsSortSessions = "sessions"
	errorsSortNewest   = "newest"

	// Without --new-since the first-seen list is read over all time; cap it.
	firstSeenMaxPagesAll = 5
	// With --new-since paging stops early; this is only a safety cap.
	firstSeenMaxPagesSince = 50

	trendUp   = "up"
	trendDown = "down"
	trendFlat = "flat"

	// errorsListGroupLimit is the plugin's cap on groups read from Pinot.
	// total_groups counts at most this many; groups_capped reports a hit.
	errorsListGroupLimit = 1000
)

// errorsListSQL ports the plugin's Errors table (report §6.3, §4.9): one row
// per (exceptionHash, exceptionType) with a prev/curr split at the range
// midpoint for the trend.
const errorsListSQL = `SELECT
  exceptionHash AS hash,
  exceptionType AS type,
  FIRSTWITHTIME(exceptionValueTemplate, "timestamp", 'STRING') AS value_template,
  count(*) AS "count",
  DISTINCTCOUNT(sessionId) FILTER (WHERE sessionId <> '' AND sessionId <> 'null') AS affected_sessions,
  DISTINCTCOUNT(pageId) FILTER (WHERE pageId <> '' AND pageId <> 'null') AS affected_pages,
  SUM(CASE WHEN "timestamp" < {{MID_MS}} THEN 1 ELSE 0 END) AS prev_count,
  SUM(CASE WHEN "timestamp" >= {{MID_MS}} THEN 1 ELSE 0 END) AS curr_count,
  max("timestamp") AS last_seen
FROM faro_pinot_exceptions_v1
WHERE appId = {{APP_ID}}
  AND $__timeFilter("timestamp")
  AND exceptionHash <> '' AND exceptionHash <> 'null' AND exceptionHash <> '0'
  AND exceptionType <> '' AND exceptionType <> 'null'{{FILTERS}}
GROUP BY hash, type
ORDER BY "count" DESC
LIMIT {{GROUP_LIMIT}}`

const (
	noteHashZero     = "exceptionHash '0' means the fingerprinter had no input; those rows are excluded."
	noteNullString   = "The 'null' string is a sentinel the SDK sends for missing values; filter it like empty."
	noteTimeFilter   = `$__timeFilter("timestamp") is expanded by the datasource from --since/--from/--to.`
	noteTimeFilter30 = `$__timeFilter("timestamp_30s") filters on the 30-second bucketed column, as the plugin does.`
)

type firstSeenSource interface {
	ListErrorFirstSeen(ctx context.Context, appID string, stopBefore time.Time, maxPages int) ([]ErrorFirstSeen, bool, error)
	GetErrorFirstSeen(ctx context.Context, appID, hash string) (*ErrorFirstSeen, error)
}

type firstSeenInfo struct {
	At       time.Time `json:"at"`
	GitHash  *string   `json:"git_hash"`
	BundleID *string   `json:"bundle_id"`
}

func newFirstSeenInfo(rec *ErrorFirstSeen) *firstSeenInfo {
	if rec == nil {
		return nil
	}
	return &firstSeenInfo{At: rec.FirstSeenAt.UTC(), GitHash: rec.GitHash, BundleID: rec.BundleID}
}

type errorTrend struct {
	Prev      int64  `json:"prev"`
	Curr      int64  `json:"curr"`
	Direction string `json:"direction"`
}

func trendDirection(prev, curr int64) string {
	switch {
	case float64(curr) > float64(prev)*1.2:
		return trendUp
	case float64(curr) < float64(prev)*0.8:
		return trendDown
	default:
		return trendFlat
	}
}

type errorGroup struct {
	Hash             string         `json:"hash"`
	Type             string         `json:"type"`
	Template         string         `json:"template"`
	Count            int64          `json:"count"`
	AffectedSessions int64          `json:"affected_sessions"`
	AffectedPages    int64          `json:"affected_pages"`
	Trend            errorTrend     `json:"trend"`
	LastSeen         *time.Time     `json:"last_seen"`
	FirstSeen        *firstSeenInfo `json:"first_seen"`
}

type errorsListResult struct {
	App         frontendApp   `json:"app"`
	Range       frontendRange `json:"range"`
	Errors      []errorGroup  `json:"errors"`
	TotalGroups int           `json:"total_groups"`
	// GroupsCapped is true when Pinot returned errorsListGroupLimit groups,
	// so total_groups is a floor and rare groups may be missing.
	GroupsCapped bool       `json:"groups_capped"`
	Next         []string   `json:"next,omitempty"`
	SQL          []sqlEntry `json:"sql,omitempty"`
}

type errorsListOpts struct {
	frontendOpts
	filterOpts

	NewSince string
	Sort     string
	Limit    int

	newSince time.Time
}

func (o *errorsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec(cmdio.FormatTable, newRowsTableCodec(cmdio.FormatTable, errorGroupTable(),
		func(r *errorsListResult) []errorGroup { return r.Errors },
		func(r *errorsListResult) []sqlEntry { return r.SQL }))
	o.IO.RegisterCustomCodec(cmdio.FormatWide, newRowsTableCodec(cmdio.FormatWide, errorGroupTable(),
		func(r *errorsListResult) []errorGroup { return r.Errors },
		func(r *errorsListResult) []sqlEntry { return r.SQL }))
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(flags)

	o.frontendOpts.setup(flags, true)
	o.setupSQLFlag(flags)
	setupFilterFlags(flags, &o.filterOpts, true)
	flags.StringVar(&o.NewSince, "new-since", "", "Only error groups first seen at or after this time (RFC3339, Unix timestamp, relative like now-6h, or a duration like 6h)")
	flags.StringVar(&o.Sort, "sort", errorsSortCount, "Sort order: count, sessions, or newest (first seen, unknown last)")
	flags.IntVar(&o.Limit, "limit", frontendListDefaultLimit, fmt.Sprintf("Maximum number of error groups to return (1-%d)", frontendListMaxLimit))
}

// setupFilterFlags registers the shared exact-match filters. Values are
// compared verbatim with the stored column, so the usage text tells agents
// where to copy them from.
func setupFilterFlags(flags *pflag.FlagSet, f *filterOpts, withEnvironment bool) {
	flags.StringVar(&f.Page, "page", "", "Only this page: the exact pageId as stored, e.g. /d/*/home (copy from pages list or errors get breakdown.page), not a URL")
	flags.StringVar(&f.Version, "version", "", "Only this app version: the exact appVersion as stored (copy from errors get breakdown.app_version)")
	flags.StringVar(&f.Browser, "browser", "", "Only this browser: the exact browserName as stored, e.g. Chrome (copy from errors get breakdown.browser)")
	flags.StringVar(&f.OS, "os", "", "Only this operating system: the exact osName as stored, e.g. \"Mac OS\" (copy from errors get breakdown.os)")
	if withEnvironment {
		flags.StringVar(&f.Environment, "environment", "", "Only this app environment: the exact appEnvironment as stored, e.g. production")
	}
}

func (o *errorsListOpts) Validate(now time.Time) error {
	if err := o.frontendOpts.Validate(); err != nil {
		return err
	}
	o.filterOpts = o.trimmed()
	switch o.Sort {
	case errorsSortCount, errorsSortSessions, errorsSortNewest:
	default:
		return fmt.Errorf("--sort must be %s, %s, or %s, got %q", errorsSortCount, errorsSortSessions, errorsSortNewest, o.Sort)
	}
	if err := validateListLimit(o.Limit); err != nil {
		return err
	}
	if s := strings.TrimSpace(o.NewSince); s != "" {
		t, err := parseNewSince(s, now)
		if err != nil {
			return err
		}
		o.newSince = t
	}
	return nil
}

// parseNewSince accepts a duration (6h → now-6h) or anything ParseTime does.
func parseNewSince(s string, now time.Time) (time.Time, error) {
	if d, err := dsquery.ParseDuration(s); err == nil && d > 0 {
		return now.Add(-d), nil
	}
	t, err := dsquery.ParseTime(s, now)
	if err != nil || t.IsZero() {
		return time.Time{}, fmt.Errorf("invalid --new-since %q: use RFC3339, a Unix timestamp, now-<dur>, or a duration like 6h", s)
	}
	return t, nil
}

func buildErrorsListStatement(appID int64, start, end time.Time, filters filterOpts, cmdline string) (statement, error) {
	mid := start.UnixMilli() + (end.UnixMilli()-start.UnixMilli())/2
	return newStatement("errors", tableExceptions, cmdline, errorsListSQL, []sqlParam{
		intParam("APP_ID", appID),
		intParam("MID_MS", mid),
		intParam("GROUP_LIMIT", errorsListGroupLimit),
		rawParam("FILTERS", filters.clauses()),
	}, noteHashZero, noteNullString, noteTimeFilter)
}

// errorsListInput is the resolved input to fetchErrorsList.
type errorsListInput struct {
	app      frontendApp
	dsUID    string
	start    time.Time
	end      time.Time
	stmt     statement
	newSince time.Time
	sort     string
	limit    int
}

type errorsListFetch struct {
	result          *errorsListResult
	firstSeenCapped bool
}

func fetchErrorsList(ctx context.Context, pq pinotQuerier, fs firstSeenSource, in errorsListInput) (*errorsListFetch, error) {
	appID := formatInt(in.app.ID)

	var parsed []errorGroup
	var records []ErrorFirstSeen
	var capped bool

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		out, err := runStatements(gctx, pq, in.dsUID, in.start, in.end, []statement{in.stmt})
		if err != nil {
			return err
		}
		parsed = parseErrorGroups(out[0])
		return nil
	})
	g.Go(func() error {
		maxPages := firstSeenMaxPagesAll
		if !in.newSince.IsZero() {
			maxPages = firstSeenMaxPagesSince
		}
		var err error
		records, capped, err = fs.ListErrorFirstSeen(gctx, appID, in.newSince, maxPages)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	byHash := make(map[string]*ErrorFirstSeen, len(records))
	for i := range records {
		byHash[records[i].ErrorHash] = &records[i]
	}

	groups := make([]errorGroup, 0, len(parsed))
	for _, g := range parsed {
		g.FirstSeen = newFirstSeenInfo(byHash[g.Hash])
		if !in.newSince.IsZero() && (g.FirstSeen == nil || g.FirstSeen.At.Before(in.newSince)) {
			continue
		}
		groups = append(groups, g)
	}

	sortErrorGroups(groups, in.sort)
	total := len(groups)
	if len(groups) > in.limit {
		groups = groups[:in.limit]
	}

	return &errorsListFetch{
		result: &errorsListResult{
			App:          in.app,
			Range:        frontendRange{From: in.start.UTC(), To: in.end.UTC()},
			Errors:       groups,
			TotalGroups:  total,
			GroupsCapped: len(parsed) >= errorsListGroupLimit,
		},
		firstSeenCapped: capped,
	}, nil
}

func parseErrorGroups(resp *querysql.QueryResponse) []errorGroup {
	rows := responseRows(resp)
	out := make([]errorGroup, 0, len(rows))
	for _, row := range rows {
		prev, curr := cellInt(row, "prev_count"), cellInt(row, "curr_count")
		out = append(out, errorGroup{
			Hash:             cellString(row, "hash"),
			Type:             cellString(row, "type"),
			Template:         cellString(row, "value_template"),
			Count:            cellInt(row, "count"),
			AffectedSessions: cellInt(row, "affected_sessions"),
			AffectedPages:    cellInt(row, "affected_pages"),
			Trend:            errorTrend{Prev: prev, Curr: curr, Direction: trendDirection(prev, curr)},
			LastSeen:         msTime(cellInt(row, "last_seen")),
		})
	}
	return out
}

func sortErrorGroups(groups []errorGroup, by string) {
	slices.SortStableFunc(groups, func(a, b errorGroup) int {
		switch by {
		case errorsSortSessions:
			if a.AffectedSessions != b.AffectedSessions {
				return cmpDesc(a.AffectedSessions, b.AffectedSessions)
			}
		case errorsSortNewest:
			switch {
			case a.FirstSeen == nil && b.FirstSeen == nil:
			case a.FirstSeen == nil:
				return 1
			case b.FirstSeen == nil:
				return -1
			case !a.FirstSeen.At.Equal(b.FirstSeen.At):
				return b.FirstSeen.At.Compare(a.FirstSeen.At)
			}
		}
		if a.Count != b.Count {
			return cmpDesc(a.Count, b.Count)
		}
		return strings.Compare(a.Hash, b.Hash)
	})
}

func cmpDesc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func errorGroupTable() cmdio.Table[errorGroup] {
	return cmdio.Table[errorGroup]{
		Columns: []cmdio.Column[errorGroup]{
			{Header: "HASH", Content: func(g errorGroup) string { return g.Hash }},
			{Header: "TYPE", Content: func(g errorGroup) string { return cmdio.OrDash(g.Type) }},
			{Header: "TEMPLATE", Content: func(g errorGroup) string { return cmdio.OrDash(oneLine(g.Template)) }},
			{Header: "COUNT", Content: func(g errorGroup) string { return formatInt(g.Count) }},
			{Header: "SESSIONS", Content: func(g errorGroup) string { return formatInt(g.AffectedSessions) }},
			{Header: "PAGES", Visible: cmdio.WideOnly, Content: func(g errorGroup) string { return formatInt(g.AffectedPages) }},
			{Header: "TREND", Content: func(g errorGroup) string { return g.Trend.Direction }},
			{Header: "FIRST SEEN", Content: func(g errorGroup) string {
				if g.FirstSeen == nil {
					return "-"
				}
				return formatTimePtr(&g.FirstSeen.At)
			}},
			{Header: "GIT HASH", Visible: cmdio.WideOnly, Content: func(g errorGroup) string {
				if g.FirstSeen == nil || g.FirstSeen.GitHash == nil {
					return "-"
				}
				return *g.FirstSeen.GitHash
			}},
			{Header: "LAST SEEN", Content: func(g errorGroup) string { return formatTimePtr(g.LastSeen) }},
		},
	}
}

func newErrorsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List error groups for a Frontend Observability app.",
		Long: `List error groups (exceptionHash + exceptionType) for a Frontend Observability
app from Pinot, joined with the first-seen record (time, git hash, bundle) the
Faro API keeps for each group.

Each group reports its event count, affected sessions and pages, a trend that
compares the first and second half of the window (up/down when the change is
over 20%), and when it was last seen. Groups the Faro API has no first-seen
record for show first_seen: null.

Pinot returns at most 1000 groups, ordered by count. total_groups counts the
groups returned, after --new-since; groups_capped is true when the cap was
hit, in which case rare groups may be missing and a filter flag or a shorter
window narrows the set. The filter flags match stored values exactly.

--new-since keeps only groups first seen at or after a time, which answers
"what broke since my deploy". --sort newest orders by first seen.

The window defaults to the last 24 hours and cannot exceed 30 days (Pinot
retention). --sql adds the PinotQL that ran; each statement runs unchanged
with gcx frontend query.`,
		Example: `  # Top errors in the last 24 hours
  gcx frontend errors list --app checkout-web

  # What broke since a deploy
  gcx frontend errors list --app checkout-web --new-since 2026-10-07T09:00:00Z

  # Errors on one page in one release, with the SQL that ran
  gcx frontend errors list --app 187 --page /cart --version 4.18.0 --sql`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(time.Now()); err != nil {
				return err
			}
			ctx := cmd.Context()
			t, err := opts.resolve(ctx, loader)
			if err != nil {
				return err
			}

			cmdline := commandLine(append([]string{"gcx frontend errors list --app", t.appID()}, append(opts.sinceArg(), opts.flagArgs()...)...)...)
			stmt, err := buildErrorsListStatement(t.app.ID, t.start, t.end, opts.filterOpts, cmdline)
			if err != nil {
				return err
			}

			fetched, err := fetchErrorsList(ctx, t.pinot, t.faro, errorsListInput{
				app: t.app, dsUID: t.dsUID, start: t.start, end: t.end, stmt: stmt,
				newSince: opts.newSince, sort: opts.Sort, limit: opts.Limit,
			})
			if err != nil {
				return err
			}
			if fetched.firstSeenCapped {
				cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("first-seen records were read only for the newest %d pages; older groups may show first_seen: null. Use --new-since to narrow.", firstSeenPagesUsed(opts.newSince)))
			}
			if fetched.result.GroupsCapped {
				cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("Pinot returned the maximum of %d error groups, so total_groups is a floor and rare groups may be missing; narrow with a filter flag or a shorter --since", errorsListGroupLimit))
			}

			result := fetched.result
			if len(result.Errors) > 0 {
				result.Next = []string{commandLine(append([]string{"gcx frontend errors get", result.Errors[0].Hash, "--app", t.appID(), "-d", t.dsUID}, opts.sinceArg()...)...)}
			}
			if opts.ShowSQL {
				result.SQL = sqlEntries(stmt)
			}

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}, opts.Share, opts.exploreLink(t, stmt, "errors list"))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func firstSeenPagesUsed(newSince time.Time) int {
	if newSince.IsZero() {
		return firstSeenMaxPagesAll
	}
	return firstSeenMaxPagesSince
}

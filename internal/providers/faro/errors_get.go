package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

const (
	errorsGetDefaultInstances = 3
	errorsGetMaxInstances     = 50
	sparklineBuckets          = 30
	sparklineMinBucketMS      = 30_000
	maxNextSessions           = 2
)

var errorHashRE = regexp.MustCompile(`^[0-9]+$`)

// The WHERE every errors get statement shares.
const errorsGetWhere = `WHERE appId = {{APP_ID}}
  AND exceptionHash = {{HASH}}
  AND $__timeFilter("timestamp")`

const errorsGetStatsSQL = `SELECT
  FIRSTWITHTIME(exceptionType, "timestamp", 'STRING') AS type,
  FIRSTWITHTIME(exceptionValueTemplate, "timestamp", 'STRING') AS value_template,
  count(*) AS total_events,
  DISTINCTCOUNT(userId) FILTER (WHERE userId <> '' AND userId <> 'null') AS affected_users,
  DISTINCTCOUNT(pageId) FILTER (WHERE pageId <> '' AND pageId <> 'null') AS affected_pages,
  DISTINCTCOUNT(sessionId) FILTER (WHERE sessionId <> '' AND sessionId <> 'null') AS affected_sessions,
  min("timestamp") AS first_seen_in_range,
  max("timestamp") AS last_seen
FROM faro_pinot_exceptions_v1
` + errorsGetWhere

const errorsGetSparklineSQL = `SELECT
  DATETIMECONVERT("timestamp", '1:MILLISECONDS:EPOCH', '1:MILLISECONDS:EPOCH', '{{BUCKET_MS}}:MILLISECONDS') AS "time",
  count(*) AS cnt
FROM faro_pinot_exceptions_v1
` + errorsGetWhere + `
GROUP BY "time"
ORDER BY "time"
LIMIT 1000`

const errorsGetStacktraceSQL = `SELECT exceptionStacktrace AS stacktrace
FROM faro_pinot_exceptions_v1
` + errorsGetWhere + `
  AND exceptionStacktrace <> '' AND exceptionStacktrace <> 'null'
ORDER BY "timestamp" DESC
LIMIT 100`

const errorsGetExamplesSQL = `SELECT exceptionValue AS value, count(*) AS cnt
FROM faro_pinot_exceptions_v1
` + errorsGetWhere + `
  AND exceptionValue <> '' AND exceptionValue <> 'null'
GROUP BY value
ORDER BY cnt DESC
LIMIT 3`

// {{DIM}} is a column name from errorBreakdownDims(), never user input.
const errorsGetBreakdownSQL = `SELECT {{DIM}} AS value, count(*) AS cnt
FROM faro_pinot_exceptions_v1
` + errorsGetWhere + `
  AND {{DIM}} <> '' AND {{DIM}} <> 'null'
GROUP BY value
ORDER BY cnt DESC
LIMIT 5`

const errorsGetInstancesSQL = `SELECT
  "timestamp",
  sessionId AS session_id,
  CASE WHEN userId = 'null' THEN '' ELSE userId END AS user_id,
  CASE WHEN pageId = 'null' THEN '' ELSE pageId END AS page_id,
  appVersion AS app_version,
  browserName AS browser_name
FROM faro_pinot_exceptions_v1
` + errorsGetWhere + `
ORDER BY "timestamp" DESC
LIMIT {{INSTANCES}}`

type breakdownDim struct{ name, column string }

// errorBreakdownDims are the errors get breakdown dimensions, in output order.
func errorBreakdownDims() []breakdownDim {
	return []breakdownDim{
		{"app_version", "appVersion"},
		{"browser", "browserName"},
		{"page", "pageId"},
		{"os", "osName"},
	}
}

type errorCounts struct {
	Events   int64 `json:"events"`
	Sessions int64 `json:"sessions"`
	Users    int64 `json:"users"`
	Pages    int64 `json:"pages"`
}

type errorSparkline struct {
	BucketMS int64     `json:"bucket_ms"`
	Start    time.Time `json:"start"`
	Counts   []int64   `json:"counts"`
}

type errorStacktrace struct {
	Symbolicated bool         `json:"symbolicated"`
	Frames       []stackFrame `json:"frames"`
	Hint         string       `json:"hint,omitempty"`
}

type valueCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type errorBreakdown struct {
	AppVersion []valueCount `json:"app_version"`
	Browser    []valueCount `json:"browser"`
	Page       []valueCount `json:"page"`
	OS         []valueCount `json:"os"`
}

type errorInstance struct {
	At         *time.Time `json:"at"`
	SessionID  string     `json:"session_id"`
	UserID     string     `json:"user_id"`
	Page       string     `json:"page"`
	AppVersion string     `json:"app_version"`
	Browser    string     `json:"browser"`
}

type errorDetail struct {
	App              frontendApp     `json:"app"`
	Range            frontendRange   `json:"range"`
	Hash             string          `json:"hash"`
	Type             string          `json:"type"`
	Template         string          `json:"template"`
	ExampleValues    []string        `json:"example_values"`
	Counts           errorCounts     `json:"counts"`
	FirstSeen        *firstSeenInfo  `json:"first_seen"`
	FirstSeenInRange *time.Time      `json:"first_seen_in_range"`
	LastSeen         *time.Time      `json:"last_seen"`
	Sparkline        errorSparkline  `json:"sparkline"`
	Stacktrace       errorStacktrace `json:"stacktrace"`
	Breakdown        errorBreakdown  `json:"breakdown"`
	Instances        []errorInstance `json:"instances"`
	Next             []string        `json:"next,omitempty"`
	SQL              []sqlEntry      `json:"sql,omitempty"`
}

// errorsGetStatements is the fixed set of statements errors get runs.
type errorsGetStatements struct {
	stats, sparkline, stacktrace, examples, instances statement
	breakdowns                                        []statement
	bucketMS                                          int64
}

func (s errorsGetStatements) all() []statement {
	out := []statement{s.stats, s.sparkline, s.stacktrace, s.examples}
	out = append(out, s.breakdowns...)
	return append(out, s.instances)
}

func sparklineBucketMS(start, end time.Time) int64 {
	b := end.Sub(start).Milliseconds() / sparklineBuckets
	if b < sparklineMinBucketMS {
		return sparklineMinBucketMS
	}
	return b
}

func buildErrorsGetStatements(appID int64, hash string, start, end time.Time, instances int, cmdline string) (errorsGetStatements, error) {
	base := []sqlParam{intParam("APP_ID", appID), stringParam("HASH", hash)}
	with := func(extra ...sqlParam) []sqlParam { return append(append([]sqlParam{}, base...), extra...) }

	var s errorsGetStatements
	var err error
	if s.stats, err = newStatement("stats", tableExceptions, cmdline, errorsGetStatsSQL, base, noteNullString, noteTimeFilter); err != nil {
		return s, err
	}
	s.bucketMS = sparklineBucketMS(start, end)
	if s.sparkline, err = newStatement("sparkline", tableExceptions, cmdline, errorsGetSparklineSQL,
		with(intParam("BUCKET_MS", s.bucketMS)),
		"Buckets are aligned to the Unix epoch; empty buckets are absent and filled with 0 by gcx.", noteTimeFilter); err != nil {
		return s, err
	}
	if s.stacktrace, err = newStatement("stacktrace", tableExceptions, cmdline, errorsGetStacktraceSQL, base,
		"Stored frames are already source-mapped when a source map was uploaded; gcx keeps the longest of these traces.", noteNullString); err != nil {
		return s, err
	}
	if s.examples, err = newStatement("example_values", tableExceptions, cmdline, errorsGetExamplesSQL, base,
		"exceptionValue is the raw message; exceptionValueTemplate is the normalised form used for grouping."); err != nil {
		return s, err
	}
	for _, d := range errorBreakdownDims() {
		st, err := newStatement("breakdown_"+d.name, tableExceptions, cmdline, errorsGetBreakdownSQL,
			with(rawParam("DIM", d.column)), noteNullString)
		if err != nil {
			return s, err
		}
		s.breakdowns = append(s.breakdowns, st)
	}
	if s.instances, err = newStatement("instances", tableExceptions, cmdline, errorsGetInstancesSQL,
		with(intParam("INSTANCES", int64(instances))), noteNullString); err != nil {
		return s, err
	}
	return s, nil
}

type errorsGetInput struct {
	app   frontendApp
	dsUID string
	start time.Time
	end   time.Time
	hash  string
	stmts errorsGetStatements
}

var errErrorNotFound = errors.New("no events for this error hash in the time range")

func fetchErrorDetail(ctx context.Context, pq pinotQuerier, fs firstSeenSource, in errorsGetInput) (*errorDetail, error) {
	var resps []*querysql.QueryResponse
	var firstSeen *ErrorFirstSeen

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		resps, err = runStatements(gctx, pq, in.dsUID, in.start, in.end, in.stmts.all())
		return err
	})
	g.Go(func() error {
		var err error
		firstSeen, err = fs.GetErrorFirstSeen(gctx, formatInt(in.app.ID), in.hash)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	stats := responseRows(resps[0])
	if len(stats) == 0 || cellInt(stats[0], "total_events") == 0 {
		return nil, errorNotFoundError(in, firstSeen)
	}
	st := stats[0]

	d := &errorDetail{
		App:      in.app,
		Range:    frontendRange{From: in.start.UTC(), To: in.end.UTC()},
		Hash:     in.hash,
		Type:     cellString(st, "type"),
		Template: cellString(st, "value_template"),
		Counts: errorCounts{
			Events:   cellInt(st, "total_events"),
			Sessions: cellInt(st, "affected_sessions"),
			Users:    cellInt(st, "affected_users"),
			Pages:    cellInt(st, "affected_pages"),
		},
		FirstSeen:        newFirstSeenInfo(firstSeen),
		FirstSeenInRange: msTime(cellInt(st, "first_seen_in_range")),
		LastSeen:         msTime(cellInt(st, "last_seen")),
		Sparkline:        buildSparkline(resps[1], in.start, in.end, in.stmts.bucketMS),
		Stacktrace:       buildStacktrace(resps[2], in.app),
		ExampleValues:    []string{},
		Instances:        []errorInstance{},
	}

	for _, row := range responseRows(resps[3]) {
		if v := cellString(row, "value"); v != "" {
			d.ExampleValues = append(d.ExampleValues, v)
		}
	}

	dims := len(errorBreakdownDims())
	breakdowns := make([][]valueCount, dims)
	for i := range dims {
		vcs := []valueCount{}
		for _, row := range responseRows(resps[4+i]) {
			vcs = append(vcs, valueCount{Value: cellString(row, "value"), Count: cellInt(row, "cnt")})
		}
		breakdowns[i] = vcs
	}
	d.Breakdown = errorBreakdown{AppVersion: breakdowns[0], Browser: breakdowns[1], Page: breakdowns[2], OS: breakdowns[3]}

	for _, row := range responseRows(resps[4+dims]) {
		d.Instances = append(d.Instances, errorInstance{
			At:         msTime(cellInt(row, "timestamp")),
			SessionID:  cellString(row, "session_id"),
			UserID:     cellString(row, "user_id"),
			Page:       cellString(row, "page_id"),
			AppVersion: cellString(row, "app_version"),
			Browser:    cellString(row, "browser_name"),
		})
	}
	return d, nil
}

// errorNotFoundError explains an empty stats result. The Faro first-seen
// record, fetched alongside the Pinot statements, tells whether the group
// exists at all, which decides whether widening the window can help.
func errorNotFoundError(in errorsGetInput, firstSeen *ErrorFirstSeen) error {
	span := in.end.Sub(in.start)
	wider := "7d"
	if span >= 7*24*time.Hour {
		wider = "30d"
	}
	appID := formatInt(in.app.ID)
	e := &gcxerrors.DetailedError{
		Summary: fmt.Sprintf("Error group %s has no events between %s and %s", in.hash,
			in.start.UTC().Format(time.RFC3339), in.end.UTC().Format(time.RFC3339)),
		Parent: errErrorNotFound,
	}
	switch {
	case firstSeen != nil:
		e.Details = fmt.Sprintf("The Faro API has a first-seen record for this group (first seen %s), so the hash is valid for app %s but the group did not occur in the window.",
			firstSeen.FirstSeenAt.UTC().Format(time.RFC3339), appID)
		if span < frontendMaxRange {
			e.Suggestions = append(e.Suggestions, fmt.Sprintf("Widen the window: gcx frontend errors get %s --app %s --since %s", in.hash, appID, wider))
		}
	default:
		e.Details = fmt.Sprintf("The Faro API has no first-seen record for this hash either; check that the hash belongs to app %s.", appID)
	}
	e.Suggestions = append(e.Suggestions, "Pick a current group: gcx frontend errors list --app "+appID)
	return e
}

func buildSparkline(resp *querysql.QueryResponse, start, end time.Time, bucketMS int64) errorSparkline {
	first := start.UnixMilli() / bucketMS * bucketMS
	last := end.UnixMilli() / bucketMS * bucketMS
	n := int((last-first)/bucketMS) + 1
	counts := make([]int64, n)
	for _, row := range responseRows(resp) {
		ts := cellInt(row, "time")
		i := int((ts - first) / bucketMS)
		if ts < first || i >= n {
			continue
		}
		counts[i] += cellInt(row, "cnt")
	}
	return errorSparkline{BucketMS: bucketMS, Start: time.UnixMilli(first).UTC(), Counts: counts}
}

func buildStacktrace(resp *querysql.QueryResponse, app frontendApp) errorStacktrace {
	var texts []string
	for _, row := range responseRows(resp) {
		if s := cellString(row, "stacktrace"); s != "" {
			texts = append(texts, s)
		}
	}
	frames := longestStacktrace(texts)
	if frames == nil {
		frames = []stackFrame{}
	}
	st := errorStacktrace{Frames: frames, Symbolicated: framesSymbolicated(frames)}
	if !st.Symbolicated {
		st.Hint = fmt.Sprintf("frames look minified or unsymbolicated: source maps (or native debug symbols) may be missing for this build. Check source map uploads with: gcx frontend apps list-sourcemaps %d", app.ID)
	}
	return st
}

type errorsGetOpts struct {
	frontendOpts

	Instances int
}

func (o *errorsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec(cmdio.FormatText, errorDetailTextCodec{})
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(flags)

	o.frontendOpts.setup(flags, true)
	o.setupSQLFlag(flags)
	flags.IntVar(&o.Instances, "instances", errorsGetDefaultInstances, fmt.Sprintf("Number of most recent occurrences to return (1-%d)", errorsGetMaxInstances))
}

func (o *errorsGetOpts) Validate() error {
	if err := o.frontendOpts.Validate(); err != nil {
		return err
	}
	if o.Instances < 1 || o.Instances > errorsGetMaxInstances {
		return fmt.Errorf("--instances must be between 1 and %d, got %d", errorsGetMaxInstances, o.Instances)
	}
	return nil
}

func newErrorsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <hash>",
		Short: "Show one error group: stack frames, first-seen release, breakdowns, and recent occurrences.",
		Long: `Show everything needed to fix one Frontend Observability error group.

<hash> is the decimal exceptionHash from gcx frontend errors list. The output
includes the type and message template, example messages, event, session, user
and page counts, a sparkline, the first-seen record from the Faro API (time,
git hash, bundle id; null when none exists), breakdowns by app version,
browser, page and OS, the most recent occurrences, and the longest recent stack
trace parsed into frames.

The sparkline has about 30 buckets of bucket_ms each, aligned to the Unix
epoch; sparkline.start is the start of the first bucket and can precede
range.from. When the group has no events in the window the command fails and
says whether the hash exists at all.

Each frame has function, file, line, col and in_app (false for node_modules,
browser extensions, native and <anonymous> frames). symbolicated is false when
no in-app frame has a function name and a line above 1, which usually means
source maps were not uploaded for the bundle.

The window defaults to the last 24 hours and cannot exceed 30 days. --sql adds
the PinotQL statements that ran.`,
		Example: `  # Everything about one error group
  gcx frontend errors get 13273781603667425932 --app checkout-web

  # Look further back, with more occurrences and the SQL
  gcx frontend errors get 13273781603667425932 --app 187 --since 7d --instances 10 --sql -o json`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if !errorHashRE.MatchString(strings.TrimSpace(args[0])) {
				return fmt.Errorf("error hash must be the decimal exceptionHash from gcx frontend errors list, got %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			hash := strings.TrimSpace(args[0])
			ctx := cmd.Context()
			t, err := opts.resolve(ctx, loader)
			if err != nil {
				return err
			}

			cmdline := commandLine(append([]string{"gcx frontend errors get", hash, "--app", t.appID()}, opts.sinceArg()...)...)
			stmts, err := buildErrorsGetStatements(t.app.ID, hash, t.start, t.end, opts.Instances, cmdline)
			if err != nil {
				return err
			}

			d, err := fetchErrorDetail(ctx, t.pinot, t.faro, errorsGetInput{
				app: t.app, dsUID: t.dsUID, start: t.start, end: t.end, hash: hash, stmts: stmts,
			})
			if err != nil {
				return err
			}
			d.Next = sessionNextSteps(d.Instances, t, opts.sinceArg())
			if opts.ShowSQL {
				d.SQL = sqlEntries(stmts.all()...)
			}

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), d)
			}, opts.Share, opts.exploreLink(t, stmts.stats, "errors get"))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// sessionNextSteps suggests sessions get for up to two distinct sessions.
func sessionNextSteps(instances []errorInstance, t *frontendTarget, since []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, in := range instances {
		if in.SessionID == "" || seen[in.SessionID] || !sessionIDRE.MatchString(in.SessionID) {
			continue
		}
		seen[in.SessionID] = true
		parts := append([]string{"gcx frontend sessions get", in.SessionID, "--app", t.appID(), "-d", t.dsUID}, since...)
		parts = append(parts, "--save", "/tmp/session-"+in.SessionID+".txt")
		out = append(out, commandLine(parts...))
		if len(out) == maxNextSessions {
			break
		}
	}
	return out
}

// sessionIDRE guards next-step commands against session IDs that would need
// shell quoting.
var sessionIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// errorDetailTextCodec is the human default for errors get.
type errorDetailTextCodec struct{}

func (errorDetailTextCodec) Format() format.Format { return format.Format(cmdio.FormatText) }

func (errorDetailTextCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

func (errorDetailTextCodec) Encode(w io.Writer, v any) error {
	d, ok := v.(*errorDetail)
	if !ok {
		return fmt.Errorf("invalid data type for text codec: expected *errorDetail, got %T", v)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", cmdio.OrDash(d.Type), cmdio.OrDash(oneLine(d.Template)))
	fmt.Fprintf(&b, "hash:        %s\n", d.Hash)
	fmt.Fprintf(&b, "app:         %s (%d)\n", cmdio.OrDash(d.App.Name), d.App.ID)
	fmt.Fprintf(&b, "counts:      %d events, %d sessions, %d users, %d pages\n", d.Counts.Events, d.Counts.Sessions, d.Counts.Users, d.Counts.Pages)
	if d.FirstSeen != nil {
		fmt.Fprintf(&b, "first seen:  %s", d.FirstSeen.At.Format(time.RFC3339))
		if d.FirstSeen.GitHash != nil {
			fmt.Fprintf(&b, " git=%s", *d.FirstSeen.GitHash)
		}
		if d.FirstSeen.BundleID != nil {
			fmt.Fprintf(&b, " bundle=%s", *d.FirstSeen.BundleID)
		}
		b.WriteByte('\n')
	} else {
		b.WriteString("first seen:  - (no record)\n")
	}
	fmt.Fprintf(&b, "in range:    %s .. %s\n", formatTimePtr(d.FirstSeenInRange), formatTimePtr(d.LastSeen))
	for _, v := range d.ExampleValues {
		fmt.Fprintf(&b, "example:     %s\n", oneLine(v))
	}

	b.WriteString("\nstacktrace")
	if !d.Stacktrace.Symbolicated {
		b.WriteString(" (not symbolicated)")
	}
	b.WriteString(":\n")
	for _, f := range d.Stacktrace.Frames {
		marker := " "
		if f.InApp {
			marker = "*"
		}
		file := f.File
		if f.Path != "" {
			file = f.Path
		}
		fmt.Fprintf(&b, "  %s %s (%s:%d:%d)\n", marker, cmdio.OrDash(f.Function), file, f.Line, f.Col)
	}
	if d.Stacktrace.Hint != "" {
		fmt.Fprintf(&b, "  hint: %s\n", d.Stacktrace.Hint)
	}

	b.WriteString("\nbreakdown:\n")
	for _, dim := range []struct {
		name string
		vcs  []valueCount
	}{{"app_version", d.Breakdown.AppVersion}, {"browser", d.Breakdown.Browser}, {"page", d.Breakdown.Page}, {"os", d.Breakdown.OS}} {
		parts := make([]string, len(dim.vcs))
		for i, vc := range dim.vcs {
			parts[i] = fmt.Sprintf("%s=%d", vc.Value, vc.Count)
		}
		fmt.Fprintf(&b, "  %-12s %s\n", dim.name, cmdio.OrDash(strings.Join(parts, " ")))
	}

	b.WriteString("\ninstances:\n")
	for _, in := range d.Instances {
		fmt.Fprintf(&b, "  %s session=%s user=%s page=%s version=%s browser=%s\n",
			formatTimePtr(in.At), cmdio.OrDash(in.SessionID), cmdio.OrDash(in.UserID), cmdio.OrDash(in.Page), cmdio.OrDash(in.AppVersion), cmdio.OrDash(in.Browser))
	}
	for _, n := range d.Next {
		fmt.Fprintf(&b, "\nnext: %s", n)
	}
	if len(d.Next) > 0 {
		b.WriteByte('\n')
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return writeSQLEntries(w, d.SQL)
}

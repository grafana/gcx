package dbo11y

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// defaultQueryWindow is the range requested for the top-queries view when
// --since isn't set.
const defaultQueryWindow = "5m"

// defaultTopQueriesLimit caps the number of top-queries rows shown by
// default; --top 0 requests the API's own maximum (maxTopQueries).
const defaultTopQueriesLimit = 10

// errInstanceNotFound signals that neither a server_id nor an instance name
// matched, so the caller emits its result document and exits non-zero.
var errInstanceNotFound = errors.New("instance not found")

type getOpts struct {
	IO         cmdio.Options
	Datasource string
	Since      string
	Top        int
	Filters    []string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &instanceDetailCodec{})
	o.IO.RegisterCustomCodec("wide", &instanceDetailCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVar(&o.Since, "since", defaultQueryWindow, "Window the top-queries and wait-event figures cover (e.g. 1m, 5m, 1h). Does not apply to instance.metrics, which the API reports over a fixed five minutes")
	flags.IntVar(&o.Top, "top", defaultTopQueriesLimit, fmt.Sprintf("Limit the number of top queries returned (0 = the API maximum; values above %d are clamped to it)", maxTopQueries))
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict the top-queries view with label=value, label!=value, label=~regex or label!~regex, e.g. --filter datname=~payments.*")
}

func (o *getOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Since) == "" {
		return gcxerrors.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	dur, err := model.ParseDuration(o.Since)
	if err != nil {
		return gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid duration", o.Since), err)
	}
	if dur <= 0 {
		return gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf("--since %q must be positive", o.Since), nil)
	}
	if o.Top < 0 {
		return gcxerrors.NewCommandUsageError(cmd, "--top must be zero or positive", nil)
	}
	return nil
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <instance>",
		Short: "Inspect a single Database Observability instance: metadata and top queries.",
		Long: `Show a query-performance snapshot for one database instance.

The argument is the instance name "gcx dbo11y instances list" reports as
NAME, or the server_id it reports alongside it — either resolves to the same
instance.

Query performance comes from the Database Observability API,
which resolves it from the stack's telemetry on your behalf, so this
reports what the product itself reports for every engine it supports:
Postgres, MySQL and SQL Server. Top queries are ranked by the share of
database time each statement accounts for, and carry the statement's own
normalized SQL.

"health" reports whether the instance is reporting to Database Observability,
which is its presence in the inventory rather than a separate check: the
collector stops publishing an instance it cannot reach after about three
minutes, so an instance that answers at all is one being collected from.

"sessions" reports the connection count against the engine's own limit, the
breakdown by session state, and the age of the oldest open transaction.
Coverage differs by engine: Postgres reports all of it, MySQL a count and a
limit only, SQL Server none.

"wait_events" reports where the instance spent time waiting, across every
query, in the collector's own categories rather than raw engine event names.

Both come from routes a stack's plugin may predate. Where that is so they are
omitted and the rest of the result still stands, rather than failing the
command.

--filter only scopes the top-queries view; the instance metadata is
unaffected. It takes label=value, label!=value, label=~regex and
label!~regex, with regexes anchored.`,
		Example: `
  # Query performance for one instance, by name
  gcx dbo11y instances get quickpizza-db

  # Or by the server_id from "instances list"
  gcx dbo11y instances get a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90

  # Show more top queries
  gcx dbo11y instances get quickpizza-db --top 20

  # Scope the top-queries view to one database on a multi-database instance
  gcx dbo11y instances get quickpizza-db --filter datname=payments

  # JSON for scripting
  gcx dbo11y instances get quickpizza-db -o json`,
		Args: cobra.ExactArgs(1),
		RunE: runGet(loader, opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Per-instance Database Observability snapshot, read from the Database Observability API. Argument is the instance name from 'gcx dbo11y instances list' or its server_id; both resolve. Reports instance metadata, a RED rollup under instance.metrics (qps, avg_latency_ms, error_rate, AWS-only cpu_percent) over a fixed five-minute window, and top_queries ranked by time_per_second (share of database time) with query_text, rows_examined and per-query error_rate. Engine-agnostic across Postgres/MySQL/SQL Server, except that per-query error_rate is absent for postgres and rows_examined is absent for sql_server. health.up reports whether the instance is reporting to Database Observability, which is its presence in the inventory rather than a separate check — the collector unregisters an instance it cannot reach after ~3 minutes of failed pings, so an instance that answers at all is being collected from, and a missing one reports health.up false alongside the not-found hint. sessions reports total and max_connections (both engines that report sessions), by_state and longest_transaction_seconds (postgres only), and is absent for sql_server. wait_events is the instance-wide breakdown in the collector's categories (IO Wait, Lock Wait, Network Wait, Engine Wait, Replication Wait, Other Wait), ordered with the dominant cause first. instance.labels carries the collector's labels keyed by raw label name, which is what --filter matches on. sessions and wait_events come from routes an older plugin may not serve; they are omitted in that case rather than failing the command. --top (default 10) caps the queries list, clamped to the API maximum of 100; each row carries its SQL text, so a high --top costs several times the default output. --since (default 5m) sizes the window top_queries and wait_events cover; the top-queries figures are floored at a minute. It does not apply to instance.metrics, which the API reports over a fixed five minutes. --filter accepts label=value, label!=value, label=~regex and label!~regex (repeatable, ANDed) and scopes top queries via datname, schema, database, query_id or digest; regexes are anchored, and any other field is a usage error rather than an empty result. The queries route ignores filters, so gcx applies them after the API has ranked the statements. On a name or server_id that matches nothing this command emits its result document, checks the stack's Database Observability activation status, and exits 1. Examples: gcx dbo11y instances get <instance> -o json; gcx dbo11y instances get <instance> --top 20 -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runGet(loader *providers.ConfigLoader, opts *getOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		name := strings.TrimSpace(args[0])
		if name == "" {
			return gcxerrors.NewCommandUsageError(cmd, "instance name is required", nil)
		}
		filters, err := parseFilters(opts.Filters)
		if err != nil {
			return gcxerrors.NewCommandUsageError(cmd, "", err)
		}
		if err := validateTopQueryFilters(filters); err != nil {
			return gcxerrors.NewCommandUsageError(cmd, "", err)
		}

		ctx := cmd.Context()

		cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
		if err != nil {
			return err
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}
		lokiDatasourceUID := ""
		if resolved, lokiErr := dsquery.ResolveDatasource(ctx, "", cfgCtx, cfg, "loki"); lokiErr != nil {
			cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("query text may be unavailable: could not resolve a Loki datasource: %v", lokiErr))
		} else {
			lokiDatasourceUID = resolved.UID
		}

		client, err := newAPIClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create Database Observability client: %w", err)
		}

		inst, err := resolveInstance(ctx, client, datasourceUID, name)
		if err != nil {
			if !isInstanceMiss(err) {
				return err
			}
			return reportInstanceMiss(ctx, cmd, &opts.IO, cfg, name, opts.Since)
		}

		// Three independent reads: none needs anything from the others beyond
		// the server_id already in hand, so they go out together rather than
		// making the snapshot cost three round trips in series.
		//
		// Session state and the wait-event breakdown are each optional: a
		// stack whose plugin predates the route reports nothing rather than
		// failing the command, since stacks upgrade on their own schedule. A
		// real failure still surfaces.
		var (
			sessions     Sessions
			haveSessions bool
			waitEvents   []WaitEvent
			topQueries   []TopQuery
			truncated    bool
			queriesErr   error
		)
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			var err error
			sessions, haveSessions, err = fetchSessions(gctx, client, datasourceUID, inst.ServerID)
			return err
		})
		g.Go(func() error {
			var err error
			waitEvents, err = fetchWaitEvents(gctx, client, datasourceUID, lokiDatasourceUID, inst.ServerID, opts.Since)
			return err
		})
		g.Go(func() error {
			// Captured rather than returned: a top-queries failure still
			// leaves a result document worth emitting, and returning it here
			// would cancel the two siblings that document reports.
			topQueries, truncated, queriesErr = fetchAndFilterTopQueries(gctx, client, datasourceUID, lokiDatasourceUID, inst.ServerID, opts.Since, opts.Top, filters)
			return nil
		})
		if err := g.Wait(); err != nil {
			return err
		}
		if truncated {
			cmdio.EmitHint(cmd.ErrOrStderr(),
				fmt.Sprintf("showing top %d queries by time share", len(topQueries)),
				fmt.Sprintf("gcx dbo11y instances get %s --top %d", name, nextTopSuggestion(opts.Top)))
		}

		detail := &InstanceDetail{
			Instance: inst,
			// The window the figures were requested over, which is what
			// --since asks for. instance.metrics is the exception and covers
			// the API's own fixed five minutes whatever this says.
			Window: opts.Since,
			// The API returned this instance, which is what reporting means.
			Health:     Health{Up: true},
			WaitEvents: waitEvents,
			TopQueries: topQueries,
		}
		if haveSessions {
			detail.Sessions = &sessions
		}
		if encErr := opts.IO.Encode(cmd.OutOrStdout(), detail); encErr != nil {
			return encErr
		}
		if queriesErr != nil {
			// Identity and the RED rollup are already in hand, so the result
			// document stands — but it is incomplete, and exit 4 says so
			// rather than letting a script read a partial answer as whole.
			cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("top queries unavailable: %v", queriesErr))
			return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, queriesErr)
		}
		return nil
	}
}

// resolveInstance fetches one instance by the command's argument, accepting
// either the server_id or the instance name. The server_id is tried first, since
// that is a single request; a 404 falls back to a name lookup against the
// inventory. Returns errInstanceNotFound when neither matches.
func resolveInstance(ctx context.Context, client *apiClient, datasourceUID, name string) (Instance, error) {
	inst, err := fetchInstanceDetail(ctx, client, datasourceUID, name)
	if err == nil {
		return inst, nil
	}
	var apiErr *apiError
	if !errors.As(err, &apiErr) || !isNotAServerID(apiErr) {
		return Instance{}, err
	}

	items, listErr := fetchInstances(ctx, client, datasourceUID, nil)
	if listErr != nil {
		return Instance{}, listErr
	}

	var matched []Instance
	for _, candidate := range items {
		if candidate.Name == name || candidate.Host == name {
			matched = append(matched, candidate)
		}
	}
	switch len(matched) {
	case 0:
		return Instance{}, errInstanceNotFound
	case 1:
		// Re-fetch by server_id: the detail route is what populates metrics,
		// which the inventory route never returns.
		return fetchInstanceDetail(ctx, client, datasourceUID, matched[0].ServerID)
	default:
		ids := make([]string, 0, len(matched))
		for _, m := range matched {
			ids = append(ids, m.ServerID)
		}
		sort.Strings(ids)
		return Instance{}, fmt.Errorf("%q matches %d instances; pass one of these server_ids instead: %s",
			name, len(matched), strings.Join(ids, ", "))
	}
}

// isInstanceMiss reports whether the argument matched no instance, as
// opposed to the lookup itself failing.
func isInstanceMiss(err error) bool {
	if errors.Is(err, errInstanceNotFound) {
		return true
	}
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// reportInstanceMiss writes the result document for an argument that matched
// nothing and returns the error to exit on. The document comes first on every
// path: stdout carries exactly one JSON value even here.
//
// A stack with Database Observability switched off looks the same from here
// as a mistyped name, so the activation state decides which of the two the
// message describes.
func reportInstanceMiss(ctx context.Context, cmd *cobra.Command, io *cmdio.Options, cfg internalconfig.NamespacedRESTConfig, name, window string) error {
	// Health.Up is left false: the API does not know this instance, which is the
	// only sense in which this command reports an instance as down.
	detail := &InstanceDetail{Instance: Instance{Name: name}, Window: window}

	if activated, actErr := checkActivation(ctx, cfg); actErr == nil && !activated {
		cmdio.EmitWarn(cmd.ErrOrStderr(), notActivatedError().Error())
		if encErr := io.Encode(cmd.OutOrStdout(), detail); encErr != nil {
			return encErr
		}
		return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, notActivatedError())
	}

	cmdio.EmitHint(cmd.ErrOrStderr(),
		fmt.Sprintf("no instance %q found, by name or server_id", name),
		"gcx dbo11y instances list")
	if encErr := io.Encode(cmd.OutOrStdout(), detail); encErr != nil {
		return encErr
	}
	return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, errInstanceNotFound)
}

// isNotAServerID reports whether a failed detail lookup means "this argument
// is not a server_id" rather than "the request went wrong", which is the
// difference between falling back to a name lookup and reporting the error.
func isNotAServerID(e *apiError) bool {
	return e.StatusCode == http.StatusNotFound || e.Code == codeInvalidServerID
}

// nextTopSuggestion returns the --top value to suggest after a truncated
// result, clamped so the suggestion is never a value the API rejects.
func nextTopSuggestion(top int) int {
	if top <= 0 || top*2 > maxTopQueries {
		return maxTopQueries
	}
	return top * 2
}

// fetchAndFilterTopQueries fetches top queries for serverID, applies the
// filters, and truncates to limit.
//
// The queries route ignores filters, so they are applied after the fetch.
// That means a filtered fetch asks for the API's maximum rather than the specified limit:
// asking for limit and then discarding the rows belonging to other
// databases would return fewer rows than were asked for. It is still not
// exhaustive — a statement ranked below the instance's busiest
// maxTopQueries by database load is never fetched, so a quiet database can
// come back empty.
//
// A fetch failure is returned as-is, with no rows. The caller decides what
// to do with it, and has the instance's identity and metrics already.
func fetchAndFilterTopQueries(ctx context.Context, client *apiClient, datasourceUID, lokiDatasourceUID, serverID, since string, limit int, filters []filter) ([]TopQuery, bool, error) {
	effectiveLimit := limit
	if effectiveLimit <= 0 || effectiveLimit > maxTopQueries {
		effectiveLimit = maxTopQueries
	}
	fetchLimit := effectiveLimit
	if len(filters) > 0 {
		fetchLimit = maxTopQueries
	}

	queries, err := fetchTopQueries(ctx, client, datasourceUID, lokiDatasourceUID, serverID, since, fetchLimit)
	if err != nil {
		return nil, false, err
	}
	queries = filterTopQueries(queries, filters)

	// Whether more rows exist is only unknowable when the server did the
	// clipping below its own cap: there, len == limit cannot be told apart
	// from a server-side cut. When the fetch asked for the maximum — because
	// matchers forced it, or because limit already was the maximum — the set
	// in hand is complete, and only a strict overflow means truncation.
	serverLimited := fetchLimit < maxTopQueries
	truncated := len(queries) > effectiveLimit || (serverLimited && len(queries) >= effectiveLimit)
	if len(queries) > effectiveLimit {
		queries = queries[:effectiveLimit]
	}
	return queries, truncated, nil
}

// instanceDetailCodec renders an InstanceDetail as a kubectl-describe-style
// key:value block followed by the top-queries table.
type instanceDetailCodec struct {
	Wide bool
}

func (c *instanceDetailCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *instanceDetailCodec) Decode(io.Reader, any) error {
	return errors.New("instances get table codec does not support decoding")
}

func (c *instanceDetailCodec) Encode(w io.Writer, v any) error {
	detail, ok := v.(*InstanceDetail)
	if !ok {
		return fmt.Errorf("invalid data type for instances get table codec: %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeRow := func(label, value string) {
		fmt.Fprintf(tw, "%s:\t%s\n", label, value)
	}

	inst := &detail.Instance
	writeRow("Name", orDash(inst.Name))
	writeRow("Reporting", boolYesNo(detail.Health.Up))
	writeRow("Engine", orDash(inst.Engine))
	writeRow("Version", orDash(inst.EngineVersion))
	writeRow("Provider", orDash(inst.ProviderName))
	if c.Wide {
		writeRow("Host", orDash(inst.Host))
		writeRow("Identifier", orDash(inst.InstanceIdentifier))
		writeRow("Region", orDash(inst.ProviderRegion))
		writeRow("Server ID", orDash(inst.ServerID))
	}
	fmt.Fprintln(tw)

	if sess := detail.Sessions; sess != nil {
		writeRow("Connections", fmtConnections(sess))
		if sess.LongestTransactionSeconds != nil {
			writeRow("Longest txn", formatSeconds(*sess.LongestTransactionSeconds, true))
		}
		fmt.Fprintln(tw)
	}

	if m := inst.Metrics; m != nil {
		// Fields are individually omitted by the API (not zeroed) when their
		// engine isn't modelled (QPS/AvgLatencyMs/ErrorRate: mysql and
		// postgres only) or the instance has no AWS db_instance_id to key CPU
		// on — a nil pointer means "not reported", rendered "-", never 0.
		writeRow("QPS", fmtOptFloat(m.QPS, 3))
		writeRow("Avg latency", fmtOptMillis(m.AvgLatencyMs))
		writeRow("Error rate", fmtOptPercent(m.ErrorRate))
		writeRow("CPU", fmtOptPercent1(m.CPUPercent))
		fmt.Fprintln(tw)
	}

	if err := tw.Flush(); err != nil {
		return err
	}

	if len(detail.WaitEvents) > 0 {
		fmt.Fprintln(w, "Waiting on:")
		wtw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, e := range detail.WaitEvents {
			fmt.Fprintf(wtw, "  %s:\t%s\n", e.Type, formatSeconds(e.WaitSeconds, true))
		}
		if err := wtw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Top queries by time share (window %s):\n", detail.Window)
	return topQueriesTable().Codec(string(c.Format())).Encode(w, detail.TopQueries)
}

// topQueriesTable declares the top-queries portion of `instances get`
// (ADR-024). The narrow view carries the ranking and the headline figures;
// QUERY TEXT, ROWS EXAMINED, ERROR RATE and ROWS/CALL sit in the wide view.
func topQueriesTable() cmdio.Table[TopQuery] {
	return cmdio.Table[TopQuery]{
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No query activity found in the requested window.")
			return err
		},
		Columns: []cmdio.Column[TopQuery]{
			{Header: "QUERYID", Content: func(q TopQuery) string { return orDash(q.QueryID) }},
			{Header: "DATNAME", Content: func(q TopQuery) string { return orDash(q.Datname) }},
			{Header: "CALLS/S", Content: func(q TopQuery) string { return fmt.Sprintf("%.3f", q.CallsPerSecond) }},
			{Header: "TIME/S", Content: func(q TopQuery) string { return fmt.Sprintf("%.6f", q.TimePerSecond) }},
			{Header: "MEAN LATENCY", Content: func(q TopQuery) string { return formatSeconds(q.MeanLatencySeconds, q.HasMeanLatency) }},
			{Header: "ROWS/CALL", Visible: cmdio.WideOnly, Content: func(q TopQuery) string { return formatRowsPerCall(q.RowsPerCall, q.HasRowsPerCall) }},
			// Each is absent for exactly one engine, and renders "-" rather
			// than 0 there: RowsExamined for sql_server, ErrorRate for
			// postgres, which has no per-statement errors counter on this
			// path.
			{Header: "ROWS EXAMINED", Visible: cmdio.WideOnly, Content: func(q TopQuery) string { return fmtOptFloat(q.RowsExamined, 0) }},
			{Header: "ERROR RATE", Visible: cmdio.WideOnly, Content: func(q TopQuery) string { return fmtOptPercent(q.ErrorRate) }},
			{Header: "QUERY TEXT", Visible: cmdio.WideOnly, Content: func(q TopQuery) string { return singleLineTruncate(q.QueryText, 150) }},
		},
	}
}

// formatSeconds prints a duration with units that scale to the magnitude —
// sub-millisecond stays in µs, sub-second stays in ms, anything larger is
// shown in seconds — or "-" when the value was not reported.
func formatSeconds(seconds float64, has bool) string {
	if !has {
		return "-"
	}
	switch {
	case seconds < 0.001:
		return fmt.Sprintf("%.0fµs", seconds*1_000_000)
	case seconds < 1:
		return fmt.Sprintf("%.2fms", seconds*1000)
	default:
		return fmt.Sprintf("%.3fs", seconds)
	}
}

func formatRowsPerCall(v float64, has bool) string {
	if !has {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

// fmtOptFloat formats an optional metric value, or "-" when the API omitted
// it (not reported, distinct from a genuine 0).
func fmtOptFloat(v *float64, decimals int) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.*f", decimals, *v)
}

// fmtOptMillis is fmtOptFloat's counterpart for a millisecond duration,
// scaled through the same units as formatSeconds.
func fmtOptMillis(v *float64) string {
	if v == nil {
		return "-"
	}
	return formatSeconds(*v/1000, true)
}

// fmtOptPercent renders an optional 0-1 ratio as a percentage with 3 decimals.
func fmtOptPercent(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.3f%%", *v*100)
}

// fmtOptPercent1 renders an optional 0-100 value (cpu_percent is already a
// percentage, not a ratio) with 1 decimal.
func fmtOptPercent1(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *v)
}

// singleLineTruncate collapses whitespace (query_text is often multi-line
// SQL) and truncates to maxLen runes for table display, appending "..." when
// truncated. Full text remains available via -o json/-o yaml.
func singleLineTruncate(s string, maxLen int) string {
	if s == "" {
		return "-"
	}
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen]) + "..."
	}
	return s
}

// fmtConnections renders the session count against the engine's own limit, which
// is the pair that means something — a count alone says nothing about headroom.
// Either half can be missing: SQL Server reports neither, and an engine without
// a state breakdown still reports both of these.
func fmtConnections(s *Sessions) string {
	switch {
	case s.Total == nil:
		return "-"
	case s.MaxConnections == nil || *s.MaxConnections == 0:
		return strconv.FormatFloat(*s.Total, 'f', -1, 64)
	default:
		return fmt.Sprintf("%s / %s (%.0f%%)",
			strconv.FormatFloat(*s.Total, 'f', -1, 64),
			strconv.FormatFloat(*s.MaxConnections, 'f', -1, 64),
			*s.Total / *s.MaxConnections * 100)
	}
}

package instances

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// defaultQueryWindow is the rate window for pg_stat_statements when --since
// isn't set. 5m mirrors appo11y's defaultRedWindow.
const defaultQueryWindow = "5m"

// defaultTopQueriesLimit caps the number of pg_stat_statements rows shown by
// default; --top 0 removes the cap.
const defaultTopQueriesLimit = 10

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
	flags.StringVar(&o.Since, "since", defaultQueryWindow, "Rate window applied to pg_stat_statements (e.g. 1m, 5m, 1h) — PromQL duration syntax")
	flags.IntVar(&o.Top, "top", defaultTopQueriesLimit, "Limit the number of top queries returned, ranked by time share (0 = unlimited)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Scope the connections/query-performance queries to series matching a label matcher, e.g. --filter datname=payments for Postgres or --filter schema=payments for MySQL (repeatable). Does not affect health/inventory metrics, which don't carry that label")
}

func (o *getOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Since) == "" {
		return fail.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	if _, err := model.ParseDuration(o.Since); err != nil {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid PromQL duration", o.Since), err)
	}
	if o.Top < 0 {
		return fail.NewCommandUsageError(cmd, "--top must be zero or positive", nil)
	}
	return nil
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Inspect a single Database Observability instance: health, connections, wait events, and top queries.",
		Long: `Show exporter health and a query-performance snapshot for one database instance.

The argument is the instance's service_name (the identifier "gcx dbo11y
instances list" reports as NAME). What's available depends on the instance's
engine (from "gcx dbo11y instances list"):

  - Health (up/down) is engine-agnostic, from the standard Prometheus scrape
    target gauge.
  - Postgres additionally reports exporter self-scrape health, per-state
    connection counts, active wait events, and the longest running
    transaction, all from postgres_exporter's pg_stat_activity collector.
  - MySQL reports a single current-connections count
    (mysql_global_status_threads_connected); wait events and exporter
    self-scrape health have no confirmed live metric for MySQL and are
    omitted rather than guessed.
  - Top queries (both engines) are ranked by time share (seconds of database
    time spent per second) over --since (default 5m): pg_stat_statements for
    Postgres, mysqld_exporter's Performance Schema eventsstatements collector
    for MySQL.

--filter only scopes the connections/query-performance queries — health and
inventory metadata don't carry a datname/schema label and are unaffected.

When no telemetry is found for the instance, this command checks whether
Database Observability has been activated for the stack and, if not, exits
non-zero with a hint to activate it in Grafana Cloud instead of a generic
"no telemetry" message.`,
		Example: `
  # Health, connections, and top queries for one instance (Postgres or MySQL)
  gcx dbo11y instances get quickpizza-db

  # Widen the query window and show more top queries
  gcx dbo11y instances get quickpizza-db --since 1h --top 20

  # Scope connections/queries to a single database on a multi-database instance
  gcx dbo11y instances get quickpizza-db --filter datname=payments

  # JSON for scripting
  gcx dbo11y instances get quickpizza-db -o json`,
		Args: cobra.ExactArgs(1),
		RunE: runGet(loader, opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Per-instance Database Observability snapshot. Health (up/down) is engine-agnostic. Postgres also reports connection counts by state, active wait events (wait_event_type/wait_event), and longest running transaction, from pg_stat_activity. MySQL reports a single current-connections count only (mysql_global_status_threads_connected); wait events aren't available as a live metric for MySQL. Top queries (both engines) are ranked by time share from pg_stat_statements (Postgres) or mysql_perf_schema_events_statements_* (MySQL) over --since (default 5m). Pairs with 'gcx dbo11y instances list' to find instance names and engines. Use --filter <label><op><value> (repeatable) to scope the connections/query-performance queries to one database on a multi-database instance, e.g. --filter datname=payments (Postgres) or --filter schema=payments (MySQL) — health/inventory metrics don't carry that label. On no telemetry this command also checks the stack's Database Observability activation status and, if not activated, exits 1 with a specific "not activated" hint instead of the generic no-telemetry message. Examples: gcx dbo11y instances get <name> -o json; gcx dbo11y instances get <name> --since 1h --top 20 -o json`,
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
			return fail.NewCommandUsageError(cmd, "instance name is required", nil)
		}
		matchers, err := parseFilters(opts.Filters)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
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

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		detail, truncated, err := fetchInstanceDetail(ctx, client, datasourceUID, name, opts.Since, opts.Top, matchers)
		if err != nil {
			return err
		}

		notFound := !detail.Health.HasUp && len(detail.Connections) == 0 && len(detail.TopQueries) == 0
		// checkActivation is a best-effort diagnostic enrichment: it can only
		// make the not-found case more specific, never less — an inconclusive
		// check (activated=false, err!=nil) falls through to the generic
		// no-telemetry message below rather than blocking.
		var notActivated bool
		if notFound {
			if activated, actErr := checkActivation(ctx, cfg); actErr == nil && !activated {
				notActivated = true
			}
		}
		switch {
		case notActivated:
			cmdio.EmitWarn(cmd.ErrOrStderr(), notActivatedError().Error())
		case notFound:
			cmdio.EmitHint(cmd.ErrOrStderr(),
				fmt.Sprintf("no telemetry found for %q in the requested window", name),
				"gcx dbo11y instances list")
		}
		if truncated {
			cmdio.EmitHint(cmd.ErrOrStderr(),
				fmt.Sprintf("showing top %d queries by time share", opts.Top),
				fmt.Sprintf("gcx dbo11y instances get %s --top %d", name, opts.Top*2))
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), detail); err != nil {
			return err
		}
		if notActivated {
			return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, notActivatedError())
		}
		if notFound {
			cmdio.EmitWarn(cmd.ErrOrStderr(), fmt.Sprintf("instance %q has no telemetry in the requested window", name))
			return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, errors.New("instance not found"))
		}
		return nil
	}
}

// fetchInstanceDetail runs the metadata lookup first (to learn the engine),
// then the health/connections/wait-events/longest-tx/top-queries queries in
// parallel, scoped to whichever metric family that engine actually has —
// see metricsForEngine. matchers only scope the per-engine activity/
// statements queries (connections, wait events, longest tx, top queries) —
// metadata and health metrics don't carry labels like datname/schema, so
// applying arbitrary matchers there would silently zero them out.
func fetchInstanceDetail(ctx context.Context, client *prometheus.Client, datasourceUID, name, window string, top int, matchers []Matcher) (*InstanceDetail, bool, error) {
	metadataExpr, err := buildConnectionInfoQuery([]Matcher{{Label: serviceNameLabel, Op: "=", Value: name}})
	if err != nil {
		return nil, false, fmt.Errorf("failed to build metadata query: %w", err)
	}
	metadataResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{Query: metadataExpr})
	if err != nil {
		return nil, false, fmt.Errorf("metadata query failed: %w", err)
	}
	metadata, err := parseInstancesResponse(metadataResp)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse instance metadata: %w", err)
	}
	inst := Instance{Name: name}
	if len(metadata) > 0 {
		inst = metadata[0]
	}
	metrics := metricsForEngine(inst.Engine)

	var (
		upResp, scrapeErrResp, scrapeDurResp *prometheus.QueryResponse
		connectionsResp, connectedResp       *prometheus.QueryResponse
		waitEventsResp, longestTxResp        *prometheus.QueryResponse
		callsResp, secondsResp, rowsResp     *prometheus.QueryResponse
	)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
		return buildScrapeUpQuery(name)
	}, &upResp))
	if metrics.scrapeErrorMetric != "" {
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildUpQuery(metrics.scrapeErrorMetric, name, nil)
		}, &scrapeErrResp))
	}
	if metrics.scrapeDurationMetric != "" {
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildUpQuery(metrics.scrapeDurationMetric, name, nil)
		}, &scrapeDurResp))
	}
	if metrics.activityMetric != "" {
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildConnectionsByStateQuery(name, matchers)
		}, &connectionsResp))
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildWaitEventsQuery(name, matchers)
		}, &waitEventsResp))
	}
	if metrics.connectedMetric != "" {
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildUpQuery(metrics.connectedMetric, name, matchers)
		}, &connectedResp))
	}
	if metrics.maxTxMetric != "" {
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildLongestTxQuery(name, matchers)
		}, &longestTxResp))
	}
	eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
		return buildTopQueriesRateQuery(metrics.statementsCalls, name, window, matchers, metrics.queryIDLabel, metrics.datnameLabel)
	}, &callsResp))
	eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
		return buildTopQueriesRateQuery(metrics.statementsSeconds, name, window, matchers, metrics.queryIDLabel, metrics.datnameLabel)
	}, &secondsResp))
	if metrics.statementsRows != "" {
		eg.Go(queryInto(egCtx, client, datasourceUID, func() (string, error) {
			return buildTopQueriesRateQuery(metrics.statementsRows, name, window, matchers, metrics.queryIDLabel, metrics.datnameLabel)
		}, &rowsResp))
	}
	if err := eg.Wait(); err != nil {
		return nil, false, err
	}

	up, hasUp := instantScalar(upResp)
	scrapeErr, hasScrapeErr := instantScalar(scrapeErrResp)
	scrapeDur, hasScrapeDur := instantScalar(scrapeDurResp)
	longestTx, hasLongestTx := instantScalar(longestTxResp)

	var connections []ConnectionState
	switch {
	case metrics.activityMetric != "":
		connections = parseConnectionsByState(connectionsResp)
	case metrics.connectedMetric != "":
		if v, ok := instantScalar(connectedResp); ok {
			connections = []ConnectionState{{State: "connected", Count: v}}
		}
	}

	calls := bucketByQueryKey(callsResp, metrics.queryIDLabel, metrics.datnameLabel)
	seconds := bucketByQueryKey(secondsResp, metrics.queryIDLabel, metrics.datnameLabel)
	rows := bucketByQueryKey(rowsResp, metrics.queryIDLabel, metrics.datnameLabel)
	topQueries, truncated := mergeTopQueries(calls, seconds, rows, top)

	return &InstanceDetail{
		Instance: inst,
		Window:   window,
		Health: InstanceHealth{
			Up:                    up > 0,
			HasUp:                 hasUp,
			ScrapeError:           scrapeErr > 0,
			HasScrapeError:        hasScrapeErr,
			ScrapeDurationSeconds: scrapeDur,
			HasScrapeDuration:     hasScrapeDur,
		},
		LongestTxSeconds: longestTx,
		HasLongestTx:     hasLongestTx,
		Connections:      connections,
		WaitEvents:       parseWaitEvents(waitEventsResp),
		TopQueries:       topQueries,
	}, truncated, nil
}

// queryInto returns an errgroup task that builds a PromQL expression via
// build, executes it, and stores the response in sink.
func queryInto(ctx context.Context, client *prometheus.Client, datasourceUID string, build func() (string, error), sink **prometheus.QueryResponse) func() error {
	return func() error {
		expr, err := build()
		if err != nil {
			return fmt.Errorf("failed to build query: %w", err)
		}
		resp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		*sink = resp
		return nil
	}
}

// instanceDetailCodec renders an InstanceDetail as a kubectl-describe-style
// key:value block followed by connections/wait-events/top-queries tables.
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
	writeRow("Engine", orDash(inst.Engine))
	writeRow("Version", orDash(inst.EngineVersion))
	writeRow("Environment", orDash(inst.Environment))
	writeRow("Provider", orDash(inst.ProviderName))
	if c.Wide {
		writeRow("Namespace", orDash(inst.Namespace))
		writeRow("Host", orDash(inst.Host))
		writeRow("Identifier", orDash(inst.InstanceIdentifier))
		writeRow("Region", orDash(inst.ProviderRegion))
	}
	fmt.Fprintln(tw)

	h := &detail.Health
	writeRow("Up", formatBoolMaybe(h.Up, h.HasUp))
	writeRow("Scrape error", formatBoolMaybe(h.ScrapeError, h.HasScrapeError))
	writeRow("Scrape duration", formatSeconds(h.ScrapeDurationSeconds, h.HasScrapeDuration))
	writeRow("Longest transaction", formatSeconds(detail.LongestTxSeconds, detail.HasLongestTx))
	fmt.Fprintln(tw)
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(detail.Connections) > 0 {
		fmt.Fprintln(w, "Connections by state:")
		t := style.NewTable("STATE", "COUNT")
		for _, c := range detail.Connections {
			t.Row(c.State, strconv.FormatFloat(c.Count, 'f', 0, 64))
		}
		if err := t.Render(w); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	if len(detail.WaitEvents) > 0 {
		fmt.Fprintln(w, "Active wait events:")
		t := style.NewTable("TYPE", "EVENT", "COUNT")
		for _, we := range detail.WaitEvents {
			t.Row(orDash(we.Type), orDash(we.Event), strconv.FormatFloat(we.Count, 'f', 0, 64))
		}
		if err := t.Render(w); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Top queries by time share (window %s):\n", detail.Window)
	if len(detail.TopQueries) == 0 {
		_, err := fmt.Fprintln(w, "No pg_stat_statements activity in the requested window.")
		return err
	}
	headers := []string{"QUERYID", "DATNAME", "CALLS/S", "TIME/S", "MEAN LATENCY"}
	if c.Wide {
		headers = append(headers, "ROWS/CALL")
	}
	t := style.NewTable(headers...)
	for _, q := range detail.TopQueries {
		row := []string{
			q.QueryID,
			orDash(q.Datname),
			fmt.Sprintf("%.3f", q.CallsPerSecond),
			fmt.Sprintf("%.6f", q.TimePerSecond),
			formatSeconds(q.MeanLatencySeconds, q.HasMeanLatency),
		}
		if c.Wide {
			row = append(row, formatRowsPerCall(q.RowsPerCall, q.HasRowsPerCall))
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func formatBoolMaybe(v, has bool) string {
	if !has {
		return "-"
	}
	if v {
		return "true"
	}
	return "false"
}

// formatSeconds prints a duration with units that scale to the magnitude —
// sub-millisecond stays in µs, sub-second stays in ms, anything larger is
// shown in seconds. Mirrors appo11y/services.formatDuration.
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

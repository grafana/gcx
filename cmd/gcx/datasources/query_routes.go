package datasources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/postgres"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
)

// Routing policy for the auto-detecting `gcx datasources query`.
//
// A normalized datasource kind is one of exactly three things, and the table
// below is where that is decided:
//
//   - expression-dispatchable — the generic `<uid> <expr>` form carries the
//     query honestly, so the kind has a handler in dispatch;
//   - redirect-only — the query is structured and no single expression can
//     represent it, so the kind has a message in redirects naming the typed
//     command to use instead;
//   - unrouted — neither, and the command reports the kind as unsupported.
//
// Adding a datasource kind is one entry here plus one small handler. It must
// never become another branch in genericQueryOpts.run: that function's control
// flow is fixed, which is what keeps QueryCmd's complexity independent of how
// many kinds gcx supports (#1137).

// genericQueryRequest is everything a dispatch handler needs. It is built once
// by the command, after validation and time parsing, so handlers do no flag
// or argument work of their own.
type genericQueryRequest struct {
	cfg   config.NamespacedRESTConfig
	uid   string
	expr  string
	start time.Time
	end   time.Time
	step  time.Duration

	// Kind-specific inputs the generic command binds as flags. A handler reads
	// only the ones its kind uses.
	profileType string
	maxNodes    int64
	limit       int

	// warn is the command's stderr. dispatchPostgres is its reader: it caps an
	// oversized LIMIT and must say so without polluting the stdout document.
	warn io.Writer
}

// queryDispatch runs the generic form for one kind and returns the value the
// command will encode. Handlers must not encode or print: the command owns
// output so that every kind emits exactly one JSON value on stdout.
type queryDispatch func(ctx context.Context, req genericQueryRequest) (any, error)

// queryRoutes holds the two disjoint routing tables. Disjointness and key
// canonicality are asserted in query_routes_internal_test.go rather than encoded in a
// type, so that "both" and "neither" cannot be constructed by accident.
type queryRoutes struct {
	dispatch  map[string]queryDispatch
	redirects map[string]string
}

// newQueryRoutes builds the tables once, when the command is constructed.
// Keeping it out of QueryCmd is deliberate: a map literal's size is attributed
// to the function that contains it, and QueryCmd is the function whose
// complexity budget #1137 is about.
func newQueryRoutes() queryRoutes {
	return queryRoutes{
		dispatch: map[string]queryDispatch{
			"clickhouse": dispatchClickHouse,
			"influxdb":   dispatchInfluxDB,
			"loki":       dispatchLoki,
			"postgres":   dispatchPostgres,
			"prometheus": dispatchPrometheus,
			"pyroscope":  dispatchPyroscope,
		},
		redirects: map[string]string{
			"cloudwatch": structuredQueryRedirect(
				"CloudWatch",
				"namespace, metric, dimensions, region, statistic, period",
				"gcx datasources cloudwatch query --namespace ... --metric ... --region ...",
			),
		},
	}
}

// supportedKinds returns every kind this command routes — expression-dispatchable
// and redirect-only alike — sorted, for the unsupported-type message. Deriving
// it is the point of #1137: the hand-maintained list had already drifted.
//
// Scope, precisely: these are the kinds `gcx datasources query` handles, not
// every kind gcx can query. Kinds with a typed `gcx datasources <kind> query`
// but no entry here (tempo, athena, infinity) are absent by construction.
// Redirect-only kinds are present because this command does handle them — by
// naming the typed command — even though it never runs their query.
func (r queryRoutes) supportedKinds() []string {
	kinds := make([]string, 0, len(r.dispatch)+len(r.redirects))
	for kind := range r.dispatch {
		kinds = append(kinds, kind)
	}
	for kind := range r.redirects {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)

	return kinds
}

// structuredQueryRedirect builds the message for a kind whose query takes
// structured parameters that no single expression can carry. An honest
// redirect to the typed command beats both a lossy generic path and the bare
// "not supported" default.
func structuredQueryRedirect(product, params, useCmd string) string {
	return fmt.Sprintf(
		"%s queries are structured (%s); "+
			"the generic `gcx datasources query <uid> <expr>` form can't carry them — "+
			"use `%s` instead",
		product, params, useCmd)
}

func dispatchPrometheus(ctx context.Context, req genericQueryRequest) (any, error) {
	client, err := prometheus.NewClient(req.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	resp, err := client.Query(ctx, req.uid, prometheus.QueryRequest{
		Query: req.expr,
		Start: req.start,
		End:   req.end,
		Step:  req.step,
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return resp, nil
}

func dispatchLoki(ctx context.Context, req genericQueryRequest) (any, error) {
	client, err := loki.NewClient(req.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	resp, err := client.Query(ctx, req.uid, loki.QueryRequest{
		Query: req.expr,
		Start: req.start,
		End:   req.end,
		Step:  req.step,
		Limit: req.limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return resp, nil
}

func dispatchPyroscope(ctx context.Context, req genericQueryRequest) (any, error) {
	if req.profileType == "" {
		return nil, errors.New("--profile-type is required for pyroscope queries")
	}

	client, err := pyroscope.NewClient(req.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	resp, err := client.Query(ctx, req.uid, pyroscope.QueryRequest{
		LabelSelector: req.expr,
		ProfileTypeID: req.profileType,
		Start:         req.start,
		End:           req.end,
		MaxNodes:      req.maxNodes,
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return resp, nil
}

func dispatchInfluxDB(ctx context.Context, req genericQueryRequest) (any, error) {
	client, err := influxdb.NewClient(req.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// InfluxDB is the one kind whose query language is a property of the
	// datasource rather than of the expression, so it costs a second lookup.
	mode, err := dsquery.GetInfluxDBMode(ctx, req.cfg, req.uid)
	if err != nil {
		return nil, fmt.Errorf("failed to detect influxdb mode: %w", err)
	}

	resp, err := client.Query(ctx, req.uid, influxdb.QueryRequest{
		Query: req.expr,
		Start: req.start,
		End:   req.end,
		Step:  req.step,
		Mode:  influxdb.Mode(mode),
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return resp, nil
}

func dispatchClickHouse(ctx context.Context, req genericQueryRequest) (any, error) {
	client, err := clickhouse.NewClient(req.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	clickhouseReq := clickhouse.QueryRequest{
		RawSQL: clickhouse.EnforceLimit(req.expr, 100, 1000),
		Start:  req.start,
		End:    req.end,
	}
	if req.step > 0 {
		clickhouseReq.IntervalMs = req.step.Milliseconds()
	}

	resp, err := client.Query(ctx, req.uid, clickhouseReq)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return resp, nil
}

func dispatchPostgres(ctx context.Context, req genericQueryRequest) (any, error) {
	client, err := postgres.NewClient(req.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	sql, capped := postgres.EnforceLimit(req.expr, 100, 1000)
	if capped {
		cmdio.Warning(req.warn, "LIMIT in query exceeds the maximum of 1000 and was capped")
	}

	postgresReq := postgres.QueryRequest{
		RawSQL: sql,
		Start:  req.start,
		End:    req.end,
	}
	if req.step > 0 {
		postgresReq.IntervalMs = req.step.Milliseconds()
	}

	resp, err := client.Query(ctx, req.uid, postgresReq)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return resp, nil
}

// Package instances implements the `gcx dbo11y instances` command group.
//
// Discovery and health data come from the postgres_exporter + pg_stat_statements
// metrics emitted by the `database_observability.postgres` Alloy component
// (job="integrations/db-o11y" by convention). `database_observability_connection_info`
// is the inventory metric — one series per database instance, carrying engine
// and cloud-provider metadata — mirroring how appo11y services list treats
// `target_info` as the service inventory.
package instances

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/promql-builder/go/promql"
)

// connectionInfoMetric is the Database Observability inventory metric: one
// gauge series per monitored database instance, carrying engine/provider
// metadata as labels.
const connectionInfoMetric = "database_observability_connection_info"

// dbo11yJobValue is the fixed job label the database_observability.postgres
// and database_observability.mysql Alloy components scrape under, regardless
// of engine. It disambiguates the universal `up` health signal from any other
// scrape target that happens to share the same service_name (an application
// pod's own `up` series, for example) — see buildScrapeUpQuery.
const dbo11yJobValue = "integrations/db-o11y"

// Postgres-specific metrics, scoped by the shared service_name label. Sourced
// from postgres_exporter (pg_stat_activity, pg_stat_statements collectors).
const (
	pgScrapeErrorMetric     = "pg_exporter_last_scrape_error"
	pgScrapeDurationMetric  = "pg_exporter_last_scrape_duration_seconds"
	pgActivityCountMetric   = "pg_stat_activity_count"
	pgActivityMaxTxMetric   = "pg_stat_activity_max_tx_duration"
	pgStatStatementsCalls   = "pg_stat_statements_calls_total"
	pgStatStatementsSeconds = "pg_stat_statements_seconds_total"
	pgStatStatementsRows    = "pg_stat_statements_rows_total"
)

// MySQL-specific metrics, scoped by the shared service_name label. Sourced
// from mysqld_exporter's Performance Schema eventsstatements collector.
// Confirmed against the grafana-dbo11y-app v2.30.0 production bundle
// (module.js chunks 530/917) — there is no live MySQL dbo11y telemetry on any
// gcx-registered stack to verify end-to-end, so these names are pinned from
// the shipping plugin's own PromQL literals rather than a query response.
const (
	mysqlStatementsCalls   = "mysql_perf_schema_events_statements_total"
	mysqlStatementsSeconds = "mysql_perf_schema_events_statements_seconds_total"
	mysqlStatementsRows    = "mysql_perf_schema_events_statements_rows_examined_total"
	mysqlConnectedMetric   = "mysql_global_status_threads_connected"
)

// serviceNameLabel is the label every Database Observability metric family
// shares (inventory, exporter health, and per-engine query-stats metrics
// alike), so it's the identifier `instances get <name>` scopes queries by.
const serviceNameLabel = "service_name"

// engineMySQL and engineDefault are the "engine" label values
// database_observability_connection_info emits. Any value other than
// "mysql" (including empty/unknown, when metadata lookup finds nothing)
// falls back to the Postgres metric family, matching this provider's
// original and most-verified behavior.
const engineMySQL = "mysql"

// engineQueryMetrics is the per-engine metric/label vocabulary for the
// pg_stat_statements-equivalent "top queries" view, plus which of the
// connections/wait-events/scrape-health signals that engine actually has a
// metric for. Empty string fields mean "no metric confirmed for this engine" —
// callers skip that query rather than guess a name and silently return
// nothing.
//
// MySQL has no confirmed per-state connection breakdown (only the single
// mysql_global_status_threads_connected gauge), no confirmed live wait-event
// metric (the product's own wait-event analysis is Loki-log-based for both
// engines — see database_observability_wait_event_seconds_total usage in the
// dbo11y-app bundle, which is a LogQL `unwrap` field, not a Prometheus
// metric), and no confirmed exporter self-scrape-health metric analogous to
// pg_exporter_last_scrape_error/duration_seconds.
type engineQueryMetrics struct {
	statementsCalls      string
	statementsSeconds    string
	statementsRows       string
	queryIDLabel         string
	datnameLabel         string
	connectedMetric      string // single current-connections gauge (MySQL); empty when a per-state breakdown exists instead
	activityMetric       string // per-state connections + wait-event breakdown (Postgres only)
	maxTxMetric          string // longest running transaction (Postgres only)
	scrapeErrorMetric    string // exporter self-reported scrape health (Postgres only)
	scrapeDurationMetric string
}

// metricsForEngine resolves the "engine" label from database_observability_connection_info
// to its query-metric vocabulary. Falls back to Postgres for any value other
// than "mysql", including empty (metadata lookup found nothing).
func metricsForEngine(engine string) engineQueryMetrics {
	if strings.EqualFold(strings.TrimSpace(engine), engineMySQL) {
		return engineQueryMetrics{
			statementsCalls:   mysqlStatementsCalls,
			statementsSeconds: mysqlStatementsSeconds,
			statementsRows:    mysqlStatementsRows,
			queryIDLabel:      "digest",
			datnameLabel:      "schema",
			connectedMetric:   mysqlConnectedMetric,
		}
	}
	return engineQueryMetrics{
		statementsCalls:      pgStatStatementsCalls,
		statementsSeconds:    pgStatStatementsSeconds,
		statementsRows:       pgStatStatementsRows,
		queryIDLabel:         "queryid",
		datnameLabel:         "datname",
		activityMetric:       pgActivityCountMetric,
		maxTxMetric:          pgActivityMaxTxMetric,
		scrapeErrorMetric:    pgScrapeErrorMetric,
		scrapeDurationMetric: pgScrapeDurationMetric,
	}
}

// escapePromqlValue escapes a raw user-supplied value for safe embedding as
// the value side of a PromQL label matcher. Mirrors appo11y/services'
// escapePromqlValue: backslashes doubled before quotes are escaped.
func escapePromqlValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}

// Instance is a single row in the database inventory, sourced from
// database_observability_connection_info.
type Instance struct {
	Name               string            `json:"name" yaml:"name"`
	Namespace          string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Engine             string            `json:"engine,omitempty" yaml:"engine,omitempty"`
	EngineVersion      string            `json:"engine_version,omitempty" yaml:"engine_version,omitempty"`
	Environment        string            `json:"environment,omitempty" yaml:"environment,omitempty"`
	Host               string            `json:"host,omitempty" yaml:"host,omitempty"`
	InstanceIdentifier string            `json:"instance_identifier,omitempty" yaml:"instance_identifier,omitempty"`
	ProviderName       string            `json:"provider_name,omitempty" yaml:"provider_name,omitempty"`
	ProviderAccount    string            `json:"provider_account,omitempty" yaml:"provider_account,omitempty"`
	ProviderRegion     string            `json:"provider_region,omitempty" yaml:"provider_region,omitempty"`
	Labels             map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// InstancesResponse is the top-level shape returned by `instances list`.
type InstancesResponse struct {
	Items []Instance `json:"items" yaml:"items"`
}

// unknownProviderValue is what the Alloy component emits for provider_* labels
// when a database isn't linked to a recognized cloud provider (self-hosted).
const unknownProviderValue = "unknown"

// orEmpty maps the exporter's "unknown" sentinel to an empty string so table
// rendering can fall back to "-" uniformly instead of printing "unknown".
func orEmpty(v string) string {
	if v == unknownProviderValue {
		return ""
	}
	return v
}

// buildConnectionInfoQuery returns the PromQL expression for the instance
// inventory. No aggregation is needed — the metric already carries one series
// per instance with every label the inventory view needs.
func buildConnectionInfoQuery(matchers []Matcher) (string, error) {
	v := promql.Vector(connectionInfoMetric)
	for _, m := range matchers {
		v = m.apply(v)
	}
	expr, err := v.Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// Matcher is a parsed `--filter` triple, identical in shape to appo11y/services.Matcher.
type Matcher struct {
	Label string
	Op    string // "=", "!=", "=~", "!~"
	Value string
}

func (m Matcher) apply(v *promql.VectorExprBuilder) *promql.VectorExprBuilder {
	val := escapePromqlValue(m.Value)
	switch m.Op {
	case "!=":
		return v.LabelNeq(m.Label, val)
	case "=~":
		return v.LabelMatchRegexp(m.Label, val)
	case "!~":
		return v.LabelNotMatchRegexp(m.Label, val)
	default: // "="
		return v.Label(m.Label, val)
	}
}

// matcherPattern accepts <label><op><value> where op is one of = != =~ !~.
var matcherPattern = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)(=~|!~|!=|=)(.*)$`)

// parseFilter validates a single `label<op>value` filter into a Matcher.
func parseFilter(raw string) (Matcher, error) {
	m := matcherPattern.FindStringSubmatch(raw)
	if m == nil {
		return Matcher{}, fmt.Errorf("invalid --filter %q: expected <label><op><value> where op is = != =~ !~", raw)
	}
	label, op, val := m[1], m[2], m[3]
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	return Matcher{Label: label, Op: op, Value: val}, nil
}

// parseFilters validates a slice of raw `--filter` strings into Matchers.
func parseFilters(raw []string) ([]Matcher, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]Matcher, 0, len(raw))
	for _, f := range raw {
		parsed, err := parseFilter(f)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

// connectionInfoPromotedLabels are the database_observability_connection_info
// labels already surfaced as typed Instance fields. Excluded from the
// catch-all Labels map so a row doesn't carry each value twice — once typed,
// once raw — and so the "unknown" provider sentinel (stripped from the typed
// fields by orEmpty) doesn't reappear verbatim in Labels.
func connectionInfoPromotedLabels() map[string]struct{} {
	return map[string]struct{}{
		"__name__":               {},
		serviceNameLabel:         {},
		"service_namespace":      {},
		"engine":                 {},
		"engine_version":         {},
		"deployment_environment": {},
		"instance":               {},
		"db_instance_identifier": {},
		"provider_name":          {},
		"provider_account":       {},
		"provider_region":        {},
	}
}

// parseInstancesResponse converts a database_observability_connection_info
// instant-query result into a sorted slice of Instance.
func parseInstancesResponse(resp *prometheus.QueryResponse) ([]Instance, error) {
	if resp == nil {
		return nil, errors.New("nil query response")
	}
	promoted := connectionInfoPromotedLabels()
	out := make([]Instance, 0, len(resp.Data.Result))
	for _, sample := range resp.Data.Result {
		name := sample.Metric[serviceNameLabel]
		if name == "" {
			continue
		}
		labels := map[string]string{}
		for k, v := range sample.Metric {
			if _, skip := promoted[k]; skip || v == "" {
				continue
			}
			labels[k] = v
		}
		out = append(out, Instance{
			Name:               name,
			Namespace:          sample.Metric["service_namespace"],
			Engine:             sample.Metric["engine"],
			EngineVersion:      sample.Metric["engine_version"],
			Environment:        sample.Metric["deployment_environment"],
			Host:               sample.Metric["instance"],
			InstanceIdentifier: orEmpty(sample.Metric["db_instance_identifier"]),
			ProviderName:       orEmpty(sample.Metric["provider_name"]),
			ProviderAccount:    orEmpty(sample.Metric["provider_account"]),
			ProviderRegion:     orEmpty(sample.Metric["provider_region"]),
			Labels:             labels,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// scopedByServiceName returns a vector selector for `metric` filtered to a
// single instance via the shared service_name label, plus any caller-supplied
// matchers.
func scopedByServiceName(metric, name string, matchers []Matcher) *promql.VectorExprBuilder {
	v := promql.Vector(metric).Label(serviceNameLabel, escapePromqlValue(name))
	for _, m := range matchers {
		v = m.apply(v)
	}
	return v
}

// buildScrapeUpQuery returns the PromQL for the universal Prometheus scrape-health
// gauge (`up`), scoped to one instance's dbo11y scrape target specifically —
// engine-agnostic, unlike pg_up/mysql_up. The job matcher is required: `up`
// is emitted by every scrape target, so an unscoped service_name match can
// collide with an unrelated target that happens to share the name (e.g. the
// database's own application pod).
func buildScrapeUpQuery(name string) (string, error) {
	if name == "" {
		return "", errors.New("instance name is required")
	}
	v := promql.Vector("up").
		Label(serviceNameLabel, escapePromqlValue(name)).
		Label("job", dbo11yJobValue)
	expr, err := v.Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildUpQuery returns the PromQL for an instant single-value metric
// (exporter scrape health, MySQL's current-connections gauge) scoped to one
// instance.
func buildUpQuery(metric, name string, matchers []Matcher) (string, error) {
	if name == "" {
		return "", errors.New("instance name is required")
	}
	if metric == "" {
		return "", errors.New("metric name is required")
	}
	expr, err := scopedByServiceName(metric, name, matchers).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildConnectionsByStateQuery returns `sum by (state) (pg_stat_activity_count{...})`
// for one instance.
func buildConnectionsByStateQuery(name string, matchers []Matcher) (string, error) {
	if name == "" {
		return "", errors.New("instance name is required")
	}
	v := scopedByServiceName(pgActivityCountMetric, name, matchers)
	expr, err := promql.Sum(v).By([]string{"state"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildWaitEventsQuery returns `sum by (wait_event_type, wait_event)
// (pg_stat_activity_count{..., wait_event!=""})` for one instance — the
// sessions currently blocked on something, broken out by what they're
// waiting on.
func buildWaitEventsQuery(name string, matchers []Matcher) (string, error) {
	if name == "" {
		return "", errors.New("instance name is required")
	}
	v := scopedByServiceName(pgActivityCountMetric, name, matchers).LabelNeq("wait_event", "")
	expr, err := promql.Sum(v).By([]string{"wait_event_type", "wait_event"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildLongestTxQuery returns `max(pg_stat_activity_max_tx_duration{...})`
// for one instance.
func buildLongestTxQuery(name string, matchers []Matcher) (string, error) {
	if name == "" {
		return "", errors.New("instance name is required")
	}
	v := scopedByServiceName(pgActivityMaxTxMetric, name, matchers)
	expr, err := promql.Max(v).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildTopQueriesRateQuery returns `sum by (<queryIDLabel>, <datnameLabel>)
// (rate(<metric>{...}[<window>]))`, used for the calls/seconds/rows rates
// that make up the top-queries view. Label names are engine-specific:
// queryid/datname for Postgres, digest/schema for MySQL.
func buildTopQueriesRateQuery(metric, name, window string, matchers []Matcher, queryIDLabel, datnameLabel string) (string, error) {
	if name == "" {
		return "", errors.New("instance name is required")
	}
	if metric == "" {
		return "", errors.New("metric name is required")
	}
	v := scopedByServiceName(metric, name, matchers).Range(window)
	expr, err := promql.Sum(promql.Rate(v)).By([]string{queryIDLabel, datnameLabel}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// InstanceHealth is the exporter-reported liveness of one database instance.
type InstanceHealth struct {
	Up                    bool    `json:"up" yaml:"up"`
	HasUp                 bool    `json:"has_up" yaml:"has_up"`
	ScrapeError           bool    `json:"scrape_error" yaml:"scrape_error"`
	HasScrapeError        bool    `json:"has_scrape_error" yaml:"has_scrape_error"`
	ScrapeDurationSeconds float64 `json:"scrape_duration_seconds" yaml:"scrape_duration_seconds"`
	HasScrapeDuration     bool    `json:"has_scrape_duration" yaml:"has_scrape_duration"`
}

// ConnectionState is one row of the connections-by-state breakdown.
type ConnectionState struct {
	State string  `json:"state" yaml:"state"`
	Count float64 `json:"count" yaml:"count"`
}

// WaitEvent is one row of the active wait-event breakdown.
type WaitEvent struct {
	Type  string  `json:"type" yaml:"type"`
	Event string  `json:"event" yaml:"event"`
	Count float64 `json:"count" yaml:"count"`
}

// TopQuery is one row of the top-queries-by-time-share view, computed from
// pg_stat_statements over the requested window.
type TopQuery struct {
	QueryID            string  `json:"query_id" yaml:"query_id"`
	Datname            string  `json:"datname" yaml:"datname"`
	CallsPerSecond     float64 `json:"calls_per_second" yaml:"calls_per_second"`
	TimePerSecond      float64 `json:"time_per_second" yaml:"time_per_second"`
	MeanLatencySeconds float64 `json:"mean_latency_seconds" yaml:"mean_latency_seconds"`
	HasMeanLatency     bool    `json:"has_mean_latency" yaml:"has_mean_latency"`
	RowsPerCall        float64 `json:"rows_per_call" yaml:"rows_per_call"`
	HasRowsPerCall     bool    `json:"has_rows_per_call" yaml:"has_rows_per_call"`
}

// InstanceDetail is the `instances get` response: inventory metadata, exporter
// health, connection/wait-event breakdowns, and the top queries by time share.
type InstanceDetail struct {
	Instance         Instance          `json:"instance" yaml:"instance"`
	Window           string            `json:"window" yaml:"window"`
	Health           InstanceHealth    `json:"health" yaml:"health"`
	LongestTxSeconds float64           `json:"longest_tx_seconds" yaml:"longest_tx_seconds"`
	HasLongestTx     bool              `json:"has_longest_tx" yaml:"has_longest_tx"`
	Connections      []ConnectionState `json:"connections,omitempty" yaml:"connections,omitempty"`
	WaitEvents       []WaitEvent       `json:"wait_events,omitempty" yaml:"wait_events,omitempty"`
	TopQueries       []TopQuery        `json:"top_queries,omitempty" yaml:"top_queries,omitempty"`
}

// instantScalar pulls the first sample's value out of a Prometheus instant
// response and parses it as float64. false means "no data" (no series,
// unparseable, or NaN/Inf) rather than a misleading zero.
func instantScalar(resp *prometheus.QueryResponse) (float64, bool) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return 0, false
	}
	return sampleScalar(resp.Data.Result[0])
}

func sampleScalar(sample prometheus.Sample) (float64, bool) {
	if len(sample.Value) < 2 {
		return 0, false
	}
	str, ok := sample.Value[1].(string)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// parseConnectionsByState converts a `sum by (state)` response into sorted
// ConnectionState rows (count desc, then state asc).
func parseConnectionsByState(resp *prometheus.QueryResponse) []ConnectionState {
	if resp == nil {
		return nil
	}
	out := make([]ConnectionState, 0, len(resp.Data.Result))
	for _, sample := range resp.Data.Result {
		v, ok := sampleScalar(sample)
		if !ok {
			continue
		}
		out = append(out, ConnectionState{State: sample.Metric["state"], Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].State < out[j].State
	})
	return out
}

// parseWaitEvents converts a `sum by (wait_event_type, wait_event)` response
// into sorted WaitEvent rows (count desc).
func parseWaitEvents(resp *prometheus.QueryResponse) []WaitEvent {
	if resp == nil {
		return nil
	}
	out := make([]WaitEvent, 0, len(resp.Data.Result))
	for _, sample := range resp.Data.Result {
		v, ok := sampleScalar(sample)
		if !ok {
			continue
		}
		out = append(out, WaitEvent{
			Type:  sample.Metric["wait_event_type"],
			Event: sample.Metric["wait_event"],
			Count: v,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Event < out[j].Event
	})
	return out
}

// queryKey identifies one pg_stat_statements row for joining the calls/time/rows
// rates together before ranking.
type queryKey struct {
	queryID string
	datname string
}

// bucketByQueryKey folds a rate-query response into a map keyed by
// (queryIDLabel, datnameLabel) — the engine-specific query-identity labels
// (queryid/datname for Postgres, digest/schema for MySQL).
func bucketByQueryKey(resp *prometheus.QueryResponse, queryIDLabel, datnameLabel string) map[queryKey]float64 {
	out := make(map[queryKey]float64)
	if resp == nil {
		return out
	}
	for _, sample := range resp.Data.Result {
		v, ok := sampleScalar(sample)
		if !ok {
			continue
		}
		k := queryKey{queryID: sample.Metric[queryIDLabel], datname: sample.Metric[datnameLabel]}
		out[k] = v
	}
	return out
}

// mergeTopQueries joins the calls/time/rows rate buckets into TopQuery rows
// and sorts by time share (seconds of DB time spent per second) descending —
// the same "where is the time going" ranking appo11y's operations view uses,
// applied to SQL statements instead of RPC operations. limit caps the
// returned rows to the busiest N; 0 means unlimited. The second return value
// reports whether rows were dropped by the cap, so callers can hint at it
// (mirroring instances list's --limit truncation hint).
func mergeTopQueries(calls, seconds, rows map[queryKey]float64, limit int) ([]TopQuery, bool) {
	keys := make(map[queryKey]struct{}, len(calls)+len(seconds)+len(rows))
	for k := range calls {
		keys[k] = struct{}{}
	}
	for k := range seconds {
		keys[k] = struct{}{}
	}
	for k := range rows {
		keys[k] = struct{}{}
	}

	out := make([]TopQuery, 0, len(keys))
	for k := range keys {
		callRate, hasCalls := calls[k]
		secRate, hasSeconds := seconds[k]
		rowRate, hasRows := rows[k]

		tq := TopQuery{
			QueryID:        k.queryID,
			Datname:        k.datname,
			CallsPerSecond: callRate,
			TimePerSecond:  secRate,
		}
		if hasCalls && callRate > 0 && hasSeconds {
			tq.MeanLatencySeconds = secRate / callRate
			tq.HasMeanLatency = true
		}
		if hasCalls && callRate > 0 && hasRows {
			tq.RowsPerCall = rowRate / callRate
			tq.HasRowsPerCall = true
		}
		out = append(out, tq)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].TimePerSecond != out[j].TimePerSecond {
			return out[i].TimePerSecond > out[j].TimePerSecond
		}
		if out[i].QueryID != out[j].QueryID {
			return out[i].QueryID < out[j].QueryID
		}
		return out[i].Datname < out[j].Datname
	})
	truncated := false
	if limit > 0 && len(out) > limit {
		out = out[:limit]
		truncated = true
	}
	return out, truncated
}

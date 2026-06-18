// Package services implements the `gcx appo11y services` command group.
//
// Service discovery mirrors the grafana/app-observability-app plugin: the
// `target_info` metric (OTel resource attributes) is treated as the inventory
// of services for a stack, and `job` is the service identifier.
package services

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

// targetInfoMetrics are the two inventory sources we always union over:
// `target_info` is what OTel SDKs emit alongside other metrics; `traces_target_info`
// is what Tempo derives from trace exports. Stacks vary in which they carry,
// and some services appear in only one — so the discovery view queries both.
func targetInfoMetrics() []string {
	return []string{"target_info", "traces_target_info"}
}

// defaultServiceGraphMetric is the Tempo-emitted service-graph total. Services
// that appear as `server` here but never in target_info are "uninstrumented" —
// other services trace calls to them, but they don't emit OTel telemetry of
// their own.
const defaultServiceGraphMetric = "traces_service_graph_request_total"

// matcherPattern accepts <label><op><value> where op is one of = != =~ !~.
// Value may be quoted or bare (bare means we'll quote it).
var matcherPattern = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)(=~|!~|!=|=)(.*)$`)

// groupByLabels is the projection every services discovery query uses.
// `job` and `telemetry_sdk_language` mirror the plugin discovery query; the
// remaining labels are surfaced by the table codec (default and wide tiers).
// Including them in the group-by keeps discovery to a single round-trip —
// labels missing on a given series simply render as empty strings.
//
// extra is appended (deduplicated) so `--columns` can pull in additional
// target_info labels without a second query.
func groupByLabels(extra []string) []string {
	base := append([]string{"telemetry_sdk_language", "job"}, allTargetInfoLabels()...)
	seen := make(map[string]struct{}, len(base)+len(extra))
	for _, l := range base {
		seen[l] = struct{}{}
	}
	for _, l := range extra {
		if l == "" {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		base = append(base, l)
	}
	return base
}

// Matcher is a parsed `--filter` triple. Quoting and escaping happen in the
// promql-builder, so Value is held as a raw unquoted string.
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

// escapePromqlValue escapes a raw user-supplied value so that it can safely be
// embedded as the value side of a PromQL label matcher. The builder wraps
// values in double quotes but does not itself escape interior backslashes or
// quotes — without this step, a value like `bar"; foo` would close the
// matcher string early and allow injection of additional PromQL.
//
// Order matters: backslashes must be doubled before quotes are escaped, or
// the inserted `\"` would then have its leading `\` doubled again.
func escapePromqlValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}

// buildServicesQuery returns a PromQL expression that groups the named
// target-info-shaped metric by the discovery key (telemetry_sdk_language, job)
// and projects the metadata labels the table view needs, so a single
// round-trip fills both default and wide columns.
//
// matchers are already-validated label filters; extraLabels are appended to
// the group-by projection for `--columns`.
func buildServicesQuery(metric string, matchers []Matcher, extraLabels []string) (string, error) {
	v := promql.Vector(metric)
	for _, m := range matchers {
		v = m.apply(v)
	}
	expr, err := promql.Group(v).By(groupByLabels(extraLabels)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// parseFilter validates a single `label<op>value` filter and returns it as a
// Matcher. Values may be wrapped in double quotes (e.g. `service_namespace="payments"`);
// quotes are stripped so the builder can re-escape consistently.
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

// Service is a single row in the services inventory.
//
// Name is the bare service name (no namespace prefix). Namespace is parsed
// from the `job` label using the `<namespace>/<service>` convention — see
// parseJob.
type Service struct {
	Name         string            `json:"name" yaml:"name"`
	Namespace    string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Language     string            `json:"language,omitempty" yaml:"language,omitempty"`
	Instrumented bool              `json:"instrumented" yaml:"instrumented"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// parseJob splits a target_info `job` label on the first slash, treating
// `<namespace>/<service>` as the canonical encoding. Jobs without a slash
// become (empty, job); anything after the first slash is preserved in the
// service name.
func parseJob(job string) (string, string) {
	ns, name, found := strings.Cut(job, "/")
	if !found {
		return "", job
	}
	return ns, name
}

// ServicesResponse is the top-level shape returned by the list command. Wrapping
// the slice in a struct keeps room for future metadata (next-page token, totals,
// truncation flags) without changing the JSON contract.
type ServicesResponse struct {
	Items []Service `json:"items" yaml:"items"`
}

// LanguageCount is one row of a per-language summary.
type LanguageCount struct {
	Language string `json:"language" yaml:"language"`
	Count    int    `json:"count" yaml:"count"`
}

// CountSummary is the alternate response shape emitted in `--count` mode.
type CountSummary struct {
	Total      int             `json:"total" yaml:"total"`
	ByLanguage []LanguageCount `json:"by_language" yaml:"by_language"`
}

// summarizeByLanguage rolls services into a CountSummary, sorted by count desc
// then language asc. An empty language becomes "(unknown)" so the row never
// disappears in the table view.
func summarizeByLanguage(items []Service) *CountSummary {
	counts := map[string]int{}
	for _, s := range items {
		lang := s.Language
		if lang == "" {
			lang = "(unknown)"
		}
		counts[lang]++
	}
	rows := make([]LanguageCount, 0, len(counts))
	for lang, n := range counts {
		rows = append(rows, LanguageCount{Language: lang, Count: n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Language < rows[j].Language
	})
	return &CountSummary{Total: len(items), ByLanguage: rows}
}

// parseServicesResponses unions multiple target-info-style query responses
// into a single, deduplicated Service slice. Each response is appended into a
// combined sample set, then handed to parseServicesResponse which already
// merges by (namespace, name, language).
func parseServicesResponses(responses []*prometheus.QueryResponse) ([]Service, error) {
	combined := &prometheus.QueryResponse{}
	for _, r := range responses {
		if r == nil {
			continue
		}
		combined.Data.Result = append(combined.Data.Result, r.Data.Result...)
	}
	return parseServicesResponse(combined)
}

// parseServicesResponse converts a Prometheus instant-query result into a
// deduplicated, sorted slice of Services. Each sample's `job` is split via
// parseJob into (namespace, name); samples sharing (namespace, name, language)
// are merged, keeping the first non-empty value seen for each metadata label.
func parseServicesResponse(resp *prometheus.QueryResponse) ([]Service, error) {
	if resp == nil {
		return nil, errors.New("nil query response")
	}
	type key struct{ namespace, name, language string }
	byKey := make(map[key]*Service)
	for _, sample := range resp.Data.Result {
		job := sample.Metric["job"]
		if job == "" {
			continue
		}
		ns, svcName := parseJob(job)
		k := key{namespace: ns, name: svcName, language: sample.Metric["telemetry_sdk_language"]}
		svc, ok := byKey[k]
		if !ok {
			svc = &Service{Name: svcName, Namespace: ns, Language: k.language, Instrumented: true}
			byKey[k] = svc
		}
		for lk, lv := range sample.Metric {
			if lk == "job" || lk == "telemetry_sdk_language" || lk == "__name__" || lv == "" {
				continue
			}
			if svc.Labels == nil {
				svc.Labels = map[string]string{}
			}
			if _, has := svc.Labels[lk]; !has {
				svc.Labels[lk] = lv
			}
		}
	}
	out := make([]Service, 0, len(byKey))
	for _, svc := range byKey {
		out = append(out, *svc)
	}
	sortServices(out)
	return out, nil
}

// sortServices orders by (namespace, name, language) so the table groups
// services under their namespace.
func sortServices(s []Service) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Namespace != s[j].Namespace {
			return s[i].Namespace < s[j].Namespace
		}
		if s[i].Name != s[j].Name {
			return s[i].Name < s[j].Name
		}
		return s[i].Language < s[j].Language
	})
}

// buildServiceGraphQuery returns a PromQL expression that lists every service
// observed as a `server` in the Tempo service-graph metric, projecting
// server_service_namespace alongside so uninstrumented services with that
// label show up under the right namespace. `connection_type!=""` keeps only
// edges where Tempo actually classified the call (database, messaging, etc.) —
// without it, partial series with empty edge metadata leak in and inflate
// the uninstrumented set. metric defaults to "traces_service_graph_request_total".
func buildServiceGraphQuery(metric string) (string, error) {
	v := promql.Vector(metric).LabelNeq("connection_type", "")
	expr, err := promql.Group(v).By([]string{"server", "server_service_namespace"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// parseServiceGraphResponse returns one Service per distinct (server,
// server_service_namespace). Results are marked `Instrumented: false`; the
// caller is expected to keep that flag when merging.
func parseServiceGraphResponse(resp *prometheus.QueryResponse) ([]Service, error) {
	if resp == nil {
		return nil, errors.New("nil query response")
	}
	type key struct{ namespace, name string }
	seen := make(map[key]struct{})
	out := make([]Service, 0, len(resp.Data.Result))
	for _, sample := range resp.Data.Result {
		name := sample.Metric["server"]
		if name == "" {
			continue
		}
		ns := sample.Metric["server_service_namespace"]
		k := key{namespace: ns, name: name}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, Service{Name: name, Namespace: ns, Instrumented: false})
	}
	sortServices(out)
	return out, nil
}

// instrumentedKey identifies a service by (namespace, name) so the merge can
// tell a target_info-known service from a service-graph entry with the same
// bare name in a different namespace. Names without a namespace also get a
// bare-name entry, so a service-graph "checkout" with no namespace still
// matches a target_info "oteldemo01/checkout" when the stack hasn't set
// server_service_namespace.
type instrumentedKey struct {
	namespace, name string
}

func instrumentedIndex(instrumented []Service) map[instrumentedKey]struct{} {
	idx := make(map[instrumentedKey]struct{}, len(instrumented)*2)
	for _, s := range instrumented {
		idx[instrumentedKey{namespace: s.Namespace, name: s.Name}] = struct{}{}
		// Service-graph entries often lack server_service_namespace; recognise
		// them by bare name too.
		idx[instrumentedKey{name: s.Name}] = struct{}{}
	}
	return idx
}

// uninstrumentedFromGraph returns the service-graph entries that are not already
// known to the baseline (target_info) set, matching on (namespace, name) and on
// bare name. Input order is preserved.
func uninstrumentedFromGraph(baseline, graph []Service) []Service {
	idx := instrumentedIndex(baseline)
	out := make([]Service, 0, len(graph))
	for _, s := range graph {
		if _, has := idx[instrumentedKey{namespace: s.Namespace, name: s.Name}]; has {
			continue
		}
		if _, has := idx[instrumentedKey{name: s.Name}]; has {
			continue
		}
		out = append(out, s)
	}
	return out
}

// mergeServiceSets joins target_info-derived services with service-graph
// servers. The display set is emitted verbatim; service-graph entries are
// only appended when they're not already known to the baseline. Baseline and
// display may be the same slice when no user filters are in play.
func mergeServiceSets(display, baseline, graph []Service) []Service {
	out := make([]Service, 0, len(display)+len(graph))
	out = append(out, display...)
	out = append(out, uninstrumentedFromGraph(baseline, graph)...)
	sortServices(out)
	return out
}

// OTel proto-style label values emitted by all metrics-generator variants;
// the metric names themselves differ between modes (see MetricsMode below).
const (
	statusCodeError  = "STATUS_CODE_ERROR"
	spanKindServer   = "SPAN_KIND_SERVER"
	spanKindConsumer = "SPAN_KIND_CONSUMER"
)

// MetricsMode identifies which family of Tempo/OTel span-metric names a
// stack emits. Three distinct name sets cover the modes a Grafana Cloud
// stack typically configures (legacy Tempo metrics-generator / Beyla
// share names with each other; modern OTel Collector emits a v3 family;
// the bare OTel Collector connector emits without the `traces_` prefix).
// The active mode is normally a stack-level setting; the CLI exposes it
// as `--metrics-mode` so a user can override the auto-probe.
type MetricsMode string

const (
	// MetricsModeV3 is the modern Tempo/OTel-Collector-≥1.0.9 family.
	// Default — matches what new Grafana Cloud stacks emit.
	MetricsModeV3 MetricsMode = "v3"
	// MetricsModeTempo is the legacy Tempo metrics-generator family
	// (also used by Beyla; sometimes labelled "legacy").
	MetricsModeTempo MetricsMode = "tempo"
	// MetricsModeOTel is the bare OTel Collector spanmetrics connector
	// family (no `traces_` prefix).
	MetricsModeOTel MetricsMode = "otel"
)

// metricNames is the (calls, latencyBucket) pair selected by MetricsMode.
type metricNames struct {
	calls         string
	latencyBucket string
}

// metricNamesByMode returns the (calls, latency-bucket) pair for the
// requested MetricsMode:
//
//	v3    → traces_span_metrics_* (modern OTel Collector / Tempo)
//	tempo → traces_spanmetrics_*  (legacy Tempo metrics-generator, Beyla)
//	otel  → bare calls_total / duration_seconds_bucket (OTel Collector
//	        spanmetrics connector, no `traces_` prefix)
//
// Constructed on demand rather than as a package global to keep the table
// inside the type's behaviour and satisfy gochecknoglobals.
func metricNamesByMode(mode MetricsMode) (metricNames, bool) {
	switch mode {
	case MetricsModeV3:
		return metricNames{
			calls:         "traces_span_metrics_calls_total",
			latencyBucket: "traces_span_metrics_duration_seconds_bucket",
		}, true
	case MetricsModeTempo:
		return metricNames{
			calls:         "traces_spanmetrics_calls_total",
			latencyBucket: "traces_spanmetrics_latency_bucket",
		}, true
	case MetricsModeOTel:
		return metricNames{
			calls:         "calls_total",
			latencyBucket: "duration_seconds_bucket",
		}, true
	}
	return metricNames{}, false
}

// metricsModeAuto is the flag value that triggers automatic detection.
// It is NOT a MetricsMode value — it resolves to one at runtime by
// probing the stack.
const metricsModeAuto = "auto"

// metricsModePreference orders the modes for auto-detection: when multiple
// families return data (common during a stack's v2→v3 migration), prefer
// the modern names so the snapshot reflects the current canonical family.
func metricsModePreference() []MetricsMode {
	return []MetricsMode{MetricsModeV3, MetricsModeTempo, MetricsModeOTel}
}

// resolveMetricsMode maps the --metrics-mode flag value onto a canonical
// MetricsMode or returns ("", true) when the user wants auto-detection.
// A few alternative names are accepted (legacy, beyla, otel-109) so users
// don't have to remember which short label maps to which family. Empty
// input defaults to auto so the common case "just works" without a flag.
func resolveMetricsMode(raw string) (MetricsMode, bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return "", true, nil
	case "v3", "otel-109", "otel109", "otelcollector109":
		return MetricsModeV3, false, nil
	case "tempo", "tempo-metrics-gen", "tempometricsgen", "beyla", "beyla-metrics-gen", "legacy":
		return MetricsModeTempo, false, nil
	case "otel", "otel-collector", "otelcollector":
		return MetricsModeOTel, false, nil
	}
	return "", false, fmt.Errorf("--metrics-mode %q is not one of: auto, v3, tempo, otel", raw)
}

// buildModeProbeQuery returns a cheap PromQL expression that yields a
// single scalar when the named calls metric has any series for the
// requested job, and empty otherwise. Used by auto-detection to pick a
// MetricsMode without running the full RED query against every family.
func buildModeProbeQuery(metric, job string) (string, error) {
	if metric == "" || job == "" {
		return "", errors.New("metric and job are required")
	}
	v := promql.Vector(metric).Label("job", escapePromqlValue(job))
	expr, err := promql.Count(v).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// jobLabel returns the PromQL `job` label value for a (namespace, name)
// pair, matching the `<namespace>/<service>` encoding target_info uses
// throughout this package.
func jobLabel(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// defaultInboundSpanKinds captures the two span kinds that represent
// incoming traffic for RED purposes: SERVER (HTTP/gRPC handlers) and
// CONSUMER (message-queue consumers). CLIENT/PRODUCER are outbound and
// would double-count if mixed in.
func defaultInboundSpanKinds() []string {
	return []string{spanKindServer, spanKindConsumer}
}

// spanKindRegex turns a kind list into a PromQL regex value. The result is
// always anchored to the literal kinds — no user input flows in.
func spanKindRegex(kinds []string) string {
	if len(kinds) == 0 {
		return spanKindServer
	}
	return strings.Join(kinds, "|")
}

// buildServiceMetadataQuery filters the target_info union by a single
// (namespace, name). When namespace is empty the matcher uses the bare name
// to catch the `job="auth"` shape; otherwise it matches the canonical
// `<namespace>/<name>` encoding. metric must be one of `target_info` or
// `traces_target_info`.
func buildServiceMetadataQuery(metric, namespace, name string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	job := name
	if namespace != "" {
		job = namespace + "/" + name
	}
	v := promql.Vector(metric).Label("job", escapePromqlValue(job))
	expr, err := promql.Group(v).By(groupByLabels(nil)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildBareNameLookupQuery searches the target_info union for any series
// whose `job` is either the bare `<name>` or some `<namespace>/<name>`.
// Used to auto-resolve the namespace when the user passes only a bare
// service name (the alternative is silent no-data for namespaced services).
// metric must be one of `target_info` or `traces_target_info`.
func buildBareNameLookupQuery(metric, name string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	// `(.+/)?<escaped name>` matches both bare `<name>` and any
	// `<ns>/<name>` shape. PromQL regexes are RE2 — anchoring is implicit
	// for full-match, so we don't need ^/$ markers.
	pattern := "(.+/)?" + regexp.QuoteMeta(name)
	v := promql.Vector(metric).LabelMatchRegexp("job", escapePromqlValue(pattern))
	expr, err := promql.Group(v).By([]string{"job"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// extractJobsFromResponses returns the deduplicated set of `job` label
// values present in the provided Prometheus responses. Empty jobs are
// dropped.
func extractJobsFromResponses(responses []*prometheus.QueryResponse) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(responses))
	for _, resp := range responses {
		if resp == nil {
			continue
		}
		for _, sample := range resp.Data.Result {
			job := sample.Metric["job"]
			if job == "" {
				continue
			}
			if _, dup := seen[job]; dup {
				continue
			}
			seen[job] = struct{}{}
			out = append(out, job)
		}
	}
	sort.Strings(out)
	return out
}

// namespacesForName parses a slice of job labels (as returned by
// buildBareNameLookupQuery) and returns the distinct namespaces that the
// requested service appears under. A job equal to the bare name is treated
// as the empty-namespace case; jobs of the shape `<ns>/<name>` contribute
// `<ns>`; jobs that end with `/<name>` but with extra slashes (rare) are
// preserved as the full prefix so the caller can still target them via
// --namespace.
func namespacesForName(jobs []string, name string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job == name {
			if _, dup := seen[""]; dup {
				continue
			}
			seen[""] = struct{}{}
			out = append(out, "")
			continue
		}
		if !strings.HasSuffix(job, "/"+name) {
			// Regex matched but the suffix doesn't actually end with /<name>
			// (defensive: shouldn't happen with our pattern).
			continue
		}
		ns := strings.TrimSuffix(job, "/"+name)
		if _, dup := seen[ns]; dup {
			continue
		}
		seen[ns] = struct{}{}
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

// buildRateQuery returns the PromQL for the headline request rate (per
// second) over the given window, scoped to the service and span kinds.
func buildRateQuery(names metricNames, namespace, name, window string, kinds []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	v := scopedSpanMetric(names.calls, namespace, name, kinds, window)
	expr, err := promql.Sum(promql.Rate(v)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildErrorRateQuery returns the PromQL for the error rate (per second)
// over the given window, scoped to status_code=STATUS_CODE_ERROR.
func buildErrorRateQuery(names metricNames, namespace, name, window string, kinds []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	v := scopedSpanMetric(names.calls, namespace, name, kinds, window).
		Label("status_code", statusCodeError)
	expr, err := promql.Sum(promql.Rate(v)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildLatencyQuantileQuery returns the PromQL for `histogram_quantile(phi,
// sum by (le) (rate(... [window]))) `. phi must be in [0, 1].
func buildLatencyQuantileQuery(names metricNames, namespace, name, window string, kinds []string, phi float64) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	if phi < 0 || phi > 1 {
		return "", fmt.Errorf("phi must be in [0,1], got %v", phi)
	}
	v := scopedSpanMetric(names.latencyBucket, namespace, name, kinds, window)
	sumByLe := promql.Sum(promql.Rate(v)).By([]string{"le"})
	expr, err := promql.HistogramQuantile(phi, sumByLe).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// scopedSpanMetric returns a vector selector for `metric` filtered by a
// single `job="<ns>/<name>"` (or bare `job="<name>"`) label plus a
// span_kind regex. Range is applied so the caller can wrap in `rate()`.
//
// We keep `service` + `service_namespace` out of the selector on purpose:
// not every metric family emits them. Newer stacks emit both, but the
// legacy Tempo `traces_spanmetrics_*` family and the OTel Collector
// variant only emit `job`. Filtering on `job` alone keeps the query
// portable across every metrics-mode this command supports.
func scopedSpanMetric(metric, namespace, name string, kinds []string, window string) *promql.VectorExprBuilder {
	return promql.Vector(metric).
		Label("job", escapePromqlValue(jobLabel(namespace, name))).
		LabelMatchRegexp("span_kind", spanKindRegex(kinds)).
		Range(window)
}

// instantScalar pulls the first sample's value out of a Prometheus instant
// response and parses it as float64. The second return is false when there
// is no series (empty result), when the value is NaN/Inf, or when it can't
// be parsed — in all three cases the caller should treat the metric as
// "no data" rather than zero so the table can render `-` instead of `0.00`.
func instantScalar(resp *prometheus.QueryResponse) (float64, bool) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return 0, false
	}
	sample := resp.Data.Result[0]
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

// REDStats holds the rate / errors / duration snapshot for one service over
// a time window. Latency fields are seconds; *HasData* flags distinguish
// "0.0 measured" from "no series in window". MetricsMode records which
// span-metric family fed the numbers, so a no-data result can be diagnosed
// (wrong mode? service really has no traffic?).
type REDStats struct {
	Window          string      `json:"window" yaml:"window"`
	MetricsMode     MetricsMode `json:"metrics_mode" yaml:"metrics_mode"`
	SpanKinds       string      `json:"span_kinds" yaml:"span_kinds"`
	RatePerSecond   float64     `json:"rate_per_second" yaml:"rate_per_second"`
	ErrorRatePerSec float64     `json:"error_rate_per_second" yaml:"error_rate_per_second"`
	ErrorPercent    float64     `json:"error_percent" yaml:"error_percent"`
	P50Seconds      float64     `json:"p50_seconds" yaml:"p50_seconds"`
	P95Seconds      float64     `json:"p95_seconds" yaml:"p95_seconds"`
	P99Seconds      float64     `json:"p99_seconds" yaml:"p99_seconds"`
	HasTraffic      bool        `json:"has_traffic" yaml:"has_traffic"`
	HasErrors       bool        `json:"has_errors" yaml:"has_errors"`
	HasLatencyP50   bool        `json:"has_latency_p50" yaml:"has_latency_p50"`
	HasLatencyP95   bool        `json:"has_latency_p95" yaml:"has_latency_p95"`
	HasLatencyP99   bool        `json:"has_latency_p99" yaml:"has_latency_p99"`
}

// ServiceDetail is the get-command response: inventory metadata plus a RED
// snapshot. Service.Instrumented=false plus !RED.HasTraffic means we found
// the service only via the service graph and it has no Tempo spanmetrics
// emitting on its behalf.
type ServiceDetail struct {
	Service Service  `json:"service" yaml:"service"`
	RED     REDStats `json:"red" yaml:"red"`
}

// Service-graph metric names. These are emitted by Tempo's
// metrics-generator and are consistent across all metrics-modes —
// unlike the spanmetrics family, only the histogram bucket may vary
// (we use the seconds-prefixed variant, which is the modern emission).
const (
	serviceGraphRequestTotalMetric        = "traces_service_graph_request_total"
	serviceGraphRequestFailedTotalMetric  = "traces_service_graph_request_failed_total"
	serviceGraphRequestServerBucketMetric = "traces_service_graph_request_server_seconds_bucket"
	serviceGraphRequestClientBucketMetric = "traces_service_graph_request_client_seconds_bucket"
)

// mapDirection picks which side of the edge the queried service sits on.
// callersDirection: X is the `server`, peers are `client`/`client_service_namespace`.
// calleesDirection: X is the `client`, peers are `server`/`server_service_namespace`.
// The direction also picks which latency histogram is meaningful —
// callers see X's response time (server_seconds), callees see how long
// X waited on the peer (client_seconds).
type mapDirection int

const (
	callersDirection mapDirection = iota
	calleesDirection
)

// peerLabels returns the (name, namespace) label pair to group by for
// this direction. Always returned in (nameLabel, namespaceLabel) order.
func (d mapDirection) peerLabels() (string, string) {
	if d == callersDirection {
		return "client", "client_service_namespace"
	}
	return "server", "server_service_namespace"
}

// selfLabels returns the (name, namespace) labels that filter the
// queried service down. These are always the *opposite* of peerLabels:
// for callers we filter by server="X" (we want edges *into* X), for
// callees by client="X" (edges *out of* X).
func (d mapDirection) selfLabels() (string, string) {
	if d == callersDirection {
		return "server", "server_service_namespace"
	}
	return "client", "client_service_namespace"
}

// latencyBucketMetric picks the histogram that matches the latency
// the queried service is responsible for or waiting on. For inbound
// callers we want server-side (how long X took to respond); for
// outbound callees we want client-side (how long X waited).
func (d mapDirection) latencyBucketMetric() string {
	if d == callersDirection {
		return serviceGraphRequestServerBucketMetric
	}
	return serviceGraphRequestClientBucketMetric
}

// buildServiceMapEdgeQuery returns a `sum by (peer_name, peer_namespace,
// connection_type) (rate(metric{self_labels}[window]))` expression that
// aggregates a service-graph counter to one row per peer per
// connection-type. metric is one of the service-graph counter metrics
// (request_total, request_failed_total).
func buildServiceMapEdgeQuery(metric string, dir mapDirection, namespace, name, window string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	if metric == "" {
		return "", errors.New("metric name is required")
	}
	selfName, selfNs := dir.selfLabels()
	peerName, peerNs := dir.peerLabels()

	v := promql.Vector(metric).Label(selfName, escapePromqlValue(name))
	if namespace != "" {
		v = v.Label(selfNs, escapePromqlValue(namespace))
	}
	v = v.Range(window)

	expr, err := promql.Sum(promql.Rate(v)).
		By([]string{peerName, peerNs, "connection_type"}).
		Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildServiceMapLatencyQuery returns
//
//	histogram_quantile(phi, sum by (le, peer_name, peer_namespace, connection_type) (rate(<bucket>[window])))
//
// with the bucket metric picked by `dir.latencyBucketMetric()`.
func buildServiceMapLatencyQuery(dir mapDirection, namespace, name, window string, phi float64) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	if phi < 0 || phi > 1 {
		return "", fmt.Errorf("phi must be in [0,1], got %v", phi)
	}
	selfName, selfNs := dir.selfLabels()
	peerName, peerNs := dir.peerLabels()

	v := promql.Vector(dir.latencyBucketMetric()).Label(selfName, escapePromqlValue(name))
	if namespace != "" {
		v = v.Label(selfNs, escapePromqlValue(namespace))
	}
	v = v.Range(window)

	sumByLe := promql.Sum(promql.Rate(v)).By([]string{"le", peerName, peerNs, "connection_type"})
	expr, err := promql.HistogramQuantile(phi, sumByLe).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// Edge is one row in the callers or callees list — a peer of the
// queried service plus the rate/error/latency observed for that edge.
// Connection type is empty for HTTP/gRPC peers; `database` / `messaging`
// / `virtual_node` for typed edges.
type Edge struct {
	Peer            Service `json:"peer" yaml:"peer"`
	ConnectionType  string  `json:"connection_type,omitempty" yaml:"connection_type,omitempty"`
	RatePerSecond   float64 `json:"rate_per_second" yaml:"rate_per_second"`
	ErrorRatePerSec float64 `json:"error_rate_per_second" yaml:"error_rate_per_second"`
	ErrorPercent    float64 `json:"error_percent" yaml:"error_percent"`
	P95Seconds      float64 `json:"p95_seconds" yaml:"p95_seconds"`
	HasErrors       bool    `json:"has_errors" yaml:"has_errors"`
	HasLatency      bool    `json:"has_latency" yaml:"has_latency"`
}

// ServiceMap is the response shape for `services map`: the queried
// service plus its inbound (callers) and outbound (callees) edges from
// the Tempo service-graph metric. Each direction is independently
// sorted by rate desc.
type ServiceMap struct {
	Service Service `json:"service" yaml:"service"`
	Window  string  `json:"window" yaml:"window"`
	Callers []Edge  `json:"callers" yaml:"callers"`
	Callees []Edge  `json:"callees" yaml:"callees"`
}

// edgeKey identifies an edge for the purpose of joining the rate /
// error / latency responses. The same peer reached over two different
// connection types is two distinct edges in the service graph.
type edgeKey struct {
	name      string
	namespace string
	connType  string
}

// extractEdges flattens a Prometheus instant response into a map keyed
// by (peer name, peer namespace, connection_type). Samples with empty
// peer name or unparseable values are dropped so callers can treat
// absence as "no data". peerName / peerNs name which labels to read.
func extractEdges(resp *prometheus.QueryResponse, peerName, peerNs string) map[edgeKey]float64 {
	out := make(map[edgeKey]float64)
	if resp == nil {
		return out
	}
	for _, sample := range resp.Data.Result {
		n := sample.Metric[peerName]
		if n == "" {
			continue
		}
		if len(sample.Value) < 2 {
			continue
		}
		str, ok := sample.Value[1].(string)
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(str, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		key := edgeKey{
			name:      n,
			namespace: sample.Metric[peerNs],
			connType:  sample.Metric["connection_type"],
		}
		out[key] = f
	}
	return out
}

// mergeEdges joins per-quantity maps (rate, errors, p95) into one row
// per (peer, connection_type), then sorts by rate desc with a stable
// (name, namespace) tiebreak. HasErrors is inferred when rate>0 — a
// healthy edge with no STATUS_CODE_ERROR series prints 0% rather than
// "no data". Matches the convention from REDStats / Operation.
func mergeEdges(rates, errors, p95s map[edgeKey]float64) []Edge {
	keys := make(map[edgeKey]struct{})
	for k := range rates {
		keys[k] = struct{}{}
	}
	for k := range errors {
		keys[k] = struct{}{}
	}
	for k := range p95s {
		keys[k] = struct{}{}
	}
	out := make([]Edge, 0, len(keys))
	for k := range keys {
		rate, hasRate := rates[k]
		errRate, hasErr := errors[k]
		p95, hasP95 := p95s[k]
		hasTraffic := hasRate && rate > 0
		out = append(out, Edge{
			Peer:            Service{Name: k.name, Namespace: k.namespace},
			ConnectionType:  k.connType,
			RatePerSecond:   rate,
			ErrorRatePerSec: errRate,
			ErrorPercent:    computeErrorPercent(errRate, rate),
			P95Seconds:      p95,
			HasErrors:       hasErr || hasTraffic,
			HasLatency:      hasP95,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RatePerSecond != out[j].RatePerSecond {
			return out[i].RatePerSecond > out[j].RatePerSecond
		}
		if out[i].Peer.Namespace != out[j].Peer.Namespace {
			return out[i].Peer.Namespace < out[j].Peer.Namespace
		}
		if out[i].Peer.Name != out[j].Peer.Name {
			return out[i].Peer.Name < out[j].Peer.Name
		}
		return out[i].ConnectionType < out[j].ConnectionType
	})
	return out
}

// computeErrorPercent reports errors/total as a percentage (0..100), or 0
// when there's no traffic. A non-zero error rate with zero total rate
// shouldn't happen in practice but we collapse it to 0 rather than +Inf so
// the table never prints `+Inf%`.
func computeErrorPercent(errors, total float64) float64 {
	if total <= 0 {
		return 0
	}
	pct := (errors / total) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

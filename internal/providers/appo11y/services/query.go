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

// labelNamePattern is the PromQL label-name grammar; `--group-by` values
// must match it so they can be dropped into a `by (...)` clause verbatim.
var labelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// parseGroupBy splits and validates the `--group-by` values (comma-separated
// or repeated) into an ordered, de-duplicated slice of label names. Grouping
// pivots a command's aggregation from a single collapsed number into one row
// per distinct value of the label(s) — the outlier-finding counterpart to
// --filter. Returns nil when nothing was requested.
func parseGroupBy(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		for label := range strings.SplitSeq(entry, ",") {
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			if !labelNamePattern.MatchString(label) {
				return nil, fmt.Errorf("invalid --group-by %q: expected a PromQL label name (letters, digits, underscore; not starting with a digit)", label)
			}
			if _, dup := seen[label]; dup {
				continue
			}
			seen[label] = struct{}{}
			out = append(out, label)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// groupKeyDelim separates label values in a canonical group key. NUL never
// appears in a Prometheus label value, so it can't collide two distinct
// combinations into one key.
const groupKeyDelim = "\x00"

// groupKeyFor builds a canonical key for a sample's values of groupLabels
// (in order) and returns the label→value map for display. An absent label
// contributes an empty string, so series missing a group label collapse
// into a single "(none)"-style bucket rather than vanishing.
func groupKeyFor(metric map[string]string, groupLabels []string) (string, map[string]string) {
	labels := make(map[string]string, len(groupLabels))
	parts := make([]string, len(groupLabels))
	for i, l := range groupLabels {
		v := metric[l]
		labels[l] = v
		parts[i] = v
	}
	return strings.Join(parts, groupKeyDelim), labels
}

// sumBy returns the group-by label set for a plain (non-quantile)
// aggregation: exactly the requested group labels, or nil to signal the
// caller should emit `sum(...)` with no `by` clause at all.
func sumBy(groupBy []string) []string {
	if len(groupBy) == 0 {
		return nil
	}
	return groupBy
}

// parseFilters validates a slice of raw `--filter` strings into Matchers.
// Shared by the per-service commands (get, map, list-operations) so a
// single service can be scoped to a dimension the underlying metric
// carries — most usefully a cluster/region label. The list command has
// its own buildFilters wrapper because it layers --language/--env on top.
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

// metricNames is the span-metric family selected by MetricsMode.
// `latencyCount` and `latencySum` enable the average-latency query that
// the operations command needs to compute time-share — they're emitted
// alongside `latencyBucket` by every span-metrics generator.
type metricNames struct {
	calls         string
	latencyBucket string
	latencyCount  string
	latencySum    string
}

// metricNamesByMode returns the span-metric family for the requested
// MetricsMode:
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
			latencyCount:  "traces_span_metrics_duration_seconds_count",
			latencySum:    "traces_span_metrics_duration_seconds_sum",
		}, true
	case MetricsModeTempo:
		return metricNames{
			calls:         "traces_spanmetrics_calls_total",
			latencyBucket: "traces_spanmetrics_latency_bucket",
			latencyCount:  "traces_spanmetrics_latency_count",
			latencySum:    "traces_spanmetrics_latency_sum",
		}, true
	case MetricsModeOTel:
		return metricNames{
			calls:         "calls_total",
			latencyBucket: "duration_seconds_bucket",
			latencyCount:  "duration_seconds_count",
			latencySum:    "duration_seconds_sum",
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
func buildModeProbeQuery(metric, job string, matchers []Matcher) (string, error) {
	if metric == "" || job == "" {
		return "", errors.New("metric and job are required")
	}
	v := promql.Vector(metric).Label("job", escapePromqlValue(job))
	for _, m := range matchers {
		v = m.apply(v)
	}
	expr, err := promql.Count(v).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildSeriesSelector renders a Prometheus `/series` match selector for a
// metric scoped to one service (job) plus any --filter matchers, e.g.
// `traces_span_metrics_calls_total{job="billing/checkout",k8s_cluster_name="prod"}`.
// Used by `services list-labels` to enumerate the label keys/values that
// --filter and --group-by can operate on. Values are escaped the same way
// as the query builders (no injection).
func buildSeriesSelector(metric, job string, matchers []Matcher) string {
	var b strings.Builder
	b.WriteString(metric)
	b.WriteString(`{job="`)
	b.WriteString(escapePromqlValue(job))
	b.WriteString(`"`)
	for _, m := range matchers {
		b.WriteString(",")
		b.WriteString(m.Label)
		b.WriteString(m.Op)
		b.WriteString(`"`)
		b.WriteString(escapePromqlValue(m.Value))
		b.WriteString(`"`)
	}
	b.WriteString("}")
	return b.String()
}

// LabelSummary is one row of `services list-labels`: a label present on the
// service's series, its distinct-value count, and either a sample of
// values (default view) or the full set (when a single label is
// requested).
type LabelSummary struct {
	Name        string   `json:"name" yaml:"name"`
	Cardinality int      `json:"cardinality" yaml:"cardinality"`
	Values      []string `json:"values,omitempty" yaml:"values,omitempty"`
}

// ServiceLabelsResponse is the `services list-labels` response: the queried
// service, the metric whose series were inspected, and one entry per
// discovered label (sorted by name).
type ServiceLabelsResponse struct {
	Service Service        `json:"service" yaml:"service"`
	Metric  string         `json:"metric" yaml:"metric"`
	Window  string         `json:"window" yaml:"window"`
	Items   []LabelSummary `json:"items" yaml:"items"`
}

// collectSeriesLabels folds a /series response (one map per series) into a
// label→distinct-values index, dropping the synthetic __name__. Callers
// turn this into LabelSummary rows.
func collectSeriesLabels(series []map[string]string) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, s := range series {
		for k, v := range s {
			if k == "__name__" {
				continue
			}
			if out[k] == nil {
				out[k] = make(map[string]struct{})
			}
			out[k][v] = struct{}{}
		}
	}
	return out
}

// summarizeLabels turns the collectSeriesLabels index into sorted
// LabelSummary rows. sampleN caps how many values each row carries (0 =
// all); the full count is always reported via Cardinality so the caller
// can show "N more".
func summarizeLabels(index map[string]map[string]struct{}, sampleN int) []LabelSummary {
	out := make([]LabelSummary, 0, len(index))
	for name, vals := range index {
		values := make([]string, 0, len(vals))
		for v := range vals {
			values = append(values, v)
		}
		sort.Strings(values)
		if sampleN > 0 && len(values) > sampleN {
			values = values[:sampleN]
		}
		out = append(out, LabelSummary{Name: name, Cardinality: len(vals), Values: values})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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
func buildServiceMetadataQuery(metric, namespace, name string, matchers []Matcher) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	job := name
	if namespace != "" {
		job = namespace + "/" + name
	}
	v := promql.Vector(metric).Label("job", escapePromqlValue(job))
	for _, m := range matchers {
		v = m.apply(v)
	}
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
func buildBareNameLookupQuery(metric, name string, matchers []Matcher) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	// `(.+/)?<escaped name>` matches both bare `<name>` and any
	// `<ns>/<name>` shape. PromQL regexes are RE2 — anchoring is implicit
	// for full-match, so we don't need ^/$ markers.
	pattern := "(.+/)?" + regexp.QuoteMeta(name)
	v := promql.Vector(metric).LabelMatchRegexp("job", escapePromqlValue(pattern))
	for _, m := range matchers {
		v = m.apply(v)
	}
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
// second) over the given window, scoped to the service, span kinds, and
// any caller-supplied `--filter` matchers. When groupBy is non-empty the
// aggregation pivots to `sum by (<groupBy>)` so each label combination
// yields its own row.
func buildRateQuery(names metricNames, namespace, name, window string, kinds []string, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	v := scopedSpanMetric(names.calls, namespace, name, kinds, window, matchers)
	agg := promql.Sum(promql.Rate(v))
	if by := sumBy(groupBy); by != nil {
		agg = agg.By(by)
	}
	expr, err := agg.Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildErrorRateQuery returns the PromQL for the error rate (per second)
// over the given window, scoped to status_code=STATUS_CODE_ERROR.
func buildErrorRateQuery(names metricNames, namespace, name, window string, kinds []string, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	v := scopedSpanMetric(names.calls, namespace, name, kinds, window, matchers).
		Label("status_code", statusCodeError)
	agg := promql.Sum(promql.Rate(v))
	if by := sumBy(groupBy); by != nil {
		agg = agg.By(by)
	}
	expr, err := agg.Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildLatencyQuantileQuery returns the PromQL for `histogram_quantile(phi,
// sum by (le[, groupBy]) (rate(... [window]))) `. phi must be in [0, 1].
// The group-by labels are preserved through the quantile so each
// combination keeps its own latency.
func buildLatencyQuantileQuery(names metricNames, namespace, name, window string, kinds []string, phi float64, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	if phi < 0 || phi > 1 {
		return "", fmt.Errorf("phi must be in [0,1], got %v", phi)
	}
	v := scopedSpanMetric(names.latencyBucket, namespace, name, kinds, window, matchers)
	sumByLe := promql.Sum(promql.Rate(v)).By(append([]string{"le"}, groupBy...))
	expr, err := promql.HistogramQuantile(phi, sumByLe).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildOperationsRateQuery returns the per-operation request rate over
// the given window: `sum by (span_name) (rate(<calls>[<window>]))`.
// Shape parallels buildRateQuery but the aggregation is grouped by
// span_name so the response can be pivoted into one row per operation.
func buildOperationsRateQuery(names metricNames, namespace, name, window string, kinds []string, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	v := scopedSpanMetric(names.calls, namespace, name, kinds, window, matchers)
	expr, err := promql.Sum(promql.Rate(v)).By(append([]string{"span_name"}, groupBy...)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildOperationsErrorRateQuery returns the per-operation error rate,
// filtered to status_code=STATUS_CODE_ERROR. An operation with no errors
// in the window produces no series — the caller treats that as 0.
func buildOperationsErrorRateQuery(names metricNames, namespace, name, window string, kinds []string, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	v := scopedSpanMetric(names.calls, namespace, name, kinds, window, matchers).
		Label("status_code", statusCodeError)
	expr, err := promql.Sum(promql.Rate(v)).By(append([]string{"span_name"}, groupBy...)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildOperationsLatencyQuantileQuery returns
// `histogram_quantile(phi, sum by (le, span_name) (rate(<bucket>[<window>])))`.
// span_name is preserved through the histogram_quantile call so quantiles
// stay per-operation.
func buildOperationsLatencyQuantileQuery(names metricNames, namespace, name, window string, kinds []string, phi float64, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	if phi < 0 || phi > 1 {
		return "", fmt.Errorf("phi must be in [0,1], got %v", phi)
	}
	v := scopedSpanMetric(names.latencyBucket, namespace, name, kinds, window, matchers)
	sumByLeAndOp := promql.Sum(promql.Rate(v)).By(append([]string{"le", "span_name"}, groupBy...))
	expr, err := promql.HistogramQuantile(phi, sumByLeAndOp).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildOperationsAvgLatencyQuery returns the per-operation average
// latency in seconds:
//
//	sum by (span_name) (rate(<sum>[<window>])) / sum by (span_name) (rate(<count>[<window>]))
//
// The average × rate gives wall-clock time spent per second, which is
// what we sort the table by (time share). Native quantiles aren't a
// substitute — the time-share signal needs the first moment.
func buildOperationsAvgLatencyQuery(names metricNames, namespace, name, window string, kinds []string, matchers []Matcher, groupBy []string) (string, error) {
	if name == "" {
		return "", errors.New("service name is required")
	}
	sumV := scopedSpanMetric(names.latencySum, namespace, name, kinds, window, matchers)
	countV := scopedSpanMetric(names.latencyCount, namespace, name, kinds, window, matchers)
	byOp := append([]string{"span_name"}, groupBy...)
	num := promql.Sum(promql.Rate(sumV)).By(byOp)
	den := promql.Sum(promql.Rate(countV)).By(byOp)
	expr, err := promql.Div(num, den).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// scopedSpanMetric returns a vector selector for `metric` filtered by a
// single `job="<ns>/<name>"` (or bare `job="<name>"`) label plus a
// span_kind regex, then any caller-supplied matchers. Range is applied so
// the caller can wrap in `rate()`.
//
// We keep `service` + `service_namespace` out of the selector on purpose:
// not every metric family emits them. Newer stacks emit both, but the
// legacy Tempo `traces_spanmetrics_*` family and the OTel Collector
// variant only emit `job`. Filtering on `job` alone keeps the query
// portable across every metrics-mode this command supports.
//
// matchers are the user's `--filter` label selectors; they scope the
// numbers to a dimension the span metric happens to carry — most
// commonly a cluster label (k8s_cluster_name / cluster) so a service
// deployed across regions can be broken out one region at a time.
func scopedSpanMetric(metric, namespace, name string, kinds []string, window string, matchers []Matcher) *promql.VectorExprBuilder {
	v := promql.Vector(metric).
		Label("job", escapePromqlValue(jobLabel(namespace, name))).
		LabelMatchRegexp("span_kind", spanKindRegex(kinds))
	for _, m := range matchers {
		v = m.apply(v)
	}
	return v.Range(window)
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
	return sampleScalar(resp.Data.Result[0])
}

// sampleScalar parses one Prometheus sample's instant value as float64.
// Returns false for a missing value, NaN/Inf, or an unparseable string so
// callers can render "no data" rather than a misleading zero.
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

// groupBucket carries a value plus the label→value map that identifies its
// group, so a grouped response can be pivoted into per-combination rows.
type groupBucket struct {
	value  float64
	labels map[string]string
}

// extractGrouped pivots a grouped instant response into one bucket per
// distinct combination of groupLabels. The canonical key (groupKeyFor)
// dedupes; the last sample wins for a given key (a well-formed grouped
// query emits one series per combination, so collisions don't occur in
// practice). Samples with unparseable values are dropped.
func extractGrouped(resp *prometheus.QueryResponse, groupLabels []string) map[string]groupBucket {
	out := make(map[string]groupBucket)
	if resp == nil {
		return out
	}
	for _, sample := range resp.Data.Result {
		f, ok := sampleScalar(sample)
		if !ok {
			continue
		}
		key, labels := groupKeyFor(sample.Metric, groupLabels)
		out[key] = groupBucket{value: f, labels: labels}
	}
	return out
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

// GroupedRED is one row of a `services get --group-by` result: the group's
// label values plus that group's RED snapshot. Labels is keyed by the
// requested group label name(s); an empty value means the series didn't
// carry that label.
type GroupedRED struct {
	Labels map[string]string `json:"labels" yaml:"labels"`
	RED    REDStats          `json:"red" yaml:"red"`
}

// GroupedServiceDetail is the get-command response when --group-by is set:
// the service identity plus one RED row per distinct group-label
// combination, sorted by request rate desc so the busiest/outlier groups
// surface first.
type GroupedServiceDetail struct {
	Service Service      `json:"service" yaml:"service"`
	GroupBy []string     `json:"group_by" yaml:"group_by"`
	Window  string       `json:"window" yaml:"window"`
	Items   []GroupedRED `json:"items" yaml:"items"`
}

// mergeGroupedRED joins the per-group rate/error/latency buckets into one
// GroupedRED per distinct group key, mirroring the single-service field
// semantics in fetchServiceDetail (HasErrors is inferred once there's
// traffic; latency flags track "measured" vs "no series"). Rows are sorted
// by rate desc with a stable group-label tiebreak so outliers surface and
// output is reproducible.
func mergeGroupedRED(window string, mode MetricsMode, kinds []string, groupBy []string, rates, errs, p50s, p95s, p99s map[string]groupBucket) []GroupedRED {
	keys := make(map[string]struct{})
	for _, m := range []map[string]groupBucket{rates, errs, p50s, p95s, p99s} {
		for k := range m {
			keys[k] = struct{}{}
		}
	}

	// labelsFor prefers whichever bucket carried the group labels for a key;
	// rate is the most likely to be present, but any of them will do.
	labelsFor := func(key string) map[string]string {
		for _, m := range []map[string]groupBucket{rates, errs, p50s, p95s, p99s} {
			if b, ok := m[key]; ok {
				return b.labels
			}
		}
		return map[string]string{}
	}

	out := make([]GroupedRED, 0, len(keys))
	for key := range keys {
		rate, hasRate := rates[key]
		errRate, hasErr := errs[key]
		p50, hasP50 := p50s[key]
		p95, hasP95 := p95s[key]
		p99, hasP99 := p99s[key]
		hasTraffic := hasRate && rate.value > 0
		out = append(out, GroupedRED{
			Labels: labelsFor(key),
			RED: REDStats{
				Window:          window,
				MetricsMode:     mode,
				SpanKinds:       spanKindRegex(kinds),
				RatePerSecond:   rate.value,
				ErrorRatePerSec: errRate.value,
				ErrorPercent:    computeErrorPercent(errRate.value, rate.value),
				P50Seconds:      p50.value,
				P95Seconds:      p95.value,
				P99Seconds:      p99.value,
				HasTraffic:      hasTraffic,
				HasErrors:       hasErr || hasTraffic,
				HasLatencyP50:   hasP50,
				HasLatencyP95:   hasP95,
				HasLatencyP99:   hasP99,
			},
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].RED.RatePerSecond != out[j].RED.RatePerSecond {
			return out[i].RED.RatePerSecond > out[j].RED.RatePerSecond
		}
		return groupLabelString(out[i].Labels, groupBy) < groupLabelString(out[j].Labels, groupBy)
	})
	return out
}

// groupLabelString renders a group's label values in groupBy order for a
// stable sort tiebreak and for table display (e.g. "prod-us" or
// "prod-us / go" for multi-label grouping). Empty values render as "(none)".
func groupLabelString(labels map[string]string, groupBy []string) string {
	parts := make([]string, len(groupBy))
	for i, l := range groupBy {
		v := labels[l]
		if v == "" {
			v = "(none)"
		}
		parts[i] = v
	}
	return strings.Join(parts, " / ")
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
func buildServiceMapEdgeQuery(metric string, dir mapDirection, namespace, name, window string, matchers []Matcher, groupBy []string) (string, error) {
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
	for _, m := range matchers {
		v = m.apply(v)
	}
	v = v.Range(window)

	expr, err := promql.Sum(promql.Rate(v)).
		By(append([]string{peerName, peerNs, "connection_type"}, groupBy...)).
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
func buildServiceMapLatencyQuery(dir mapDirection, namespace, name, window string, phi float64, matchers []Matcher, groupBy []string) (string, error) {
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
	for _, m := range matchers {
		v = m.apply(v)
	}
	v = v.Range(window)

	sumByLe := promql.Sum(promql.Rate(v)).By(append([]string{"le", peerName, peerNs, "connection_type"}, groupBy...))
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
	Peer            Service           `json:"peer" yaml:"peer"`
	ConnectionType  string            `json:"connection_type,omitempty" yaml:"connection_type,omitempty"`
	Labels          map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	RatePerSecond   float64           `json:"rate_per_second" yaml:"rate_per_second"`
	ErrorRatePerSec float64           `json:"error_rate_per_second" yaml:"error_rate_per_second"`
	ErrorPercent    float64           `json:"error_percent" yaml:"error_percent"`
	P95Seconds      float64           `json:"p95_seconds" yaml:"p95_seconds"`
	HasErrors       bool              `json:"has_errors" yaml:"has_errors"`
	HasLatency      bool              `json:"has_latency" yaml:"has_latency"`
}

// ServiceMap is the response shape for `services map`: the queried
// service plus its inbound (callers) and outbound (callees) edges from
// the Tempo service-graph metric. Each direction is independently
// sorted by rate desc.
type ServiceMap struct {
	Service Service  `json:"service" yaml:"service"`
	Window  string   `json:"window" yaml:"window"`
	GroupBy []string `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	Callers []Edge   `json:"callers" yaml:"callers"`
	Callees []Edge   `json:"callees" yaml:"callees"`
}

// edgeKey identifies an edge for the purpose of joining the rate /
// error / latency responses. The same peer reached over two different
// connection types is two distinct edges in the service graph; groupKey
// splits an edge further per --group-by combination.
type edgeKey struct {
	name      string
	namespace string
	connType  string
	groupKey  string
}

// extractEdges flattens a Prometheus instant response into a map keyed
// by (peer name, peer namespace, connection_type, group). Samples with
// empty peer name or unparseable values are dropped so callers can treat
// absence as "no data". peerName / peerNs name which labels to read;
// groupLabels are the --group-by dimensions carried in the bucket.
func extractEdges(resp *prometheus.QueryResponse, peerName, peerNs string, groupLabels []string) map[edgeKey]groupBucket {
	out := make(map[edgeKey]groupBucket)
	if resp == nil {
		return out
	}
	for _, sample := range resp.Data.Result {
		n := sample.Metric[peerName]
		if n == "" {
			continue
		}
		f, ok := sampleScalar(sample)
		if !ok {
			continue
		}
		gkey, glabels := groupKeyFor(sample.Metric, groupLabels)
		key := edgeKey{
			name:      n,
			namespace: sample.Metric[peerNs],
			connType:  sample.Metric["connection_type"],
			groupKey:  gkey,
		}
		out[key] = groupBucket{value: f, labels: glabels}
	}
	return out
}

// Operation is one row in the operations table — a single span_name
// inside a service, with its RED snapshot. AvgSeconds is the arithmetic
// mean latency (sum / count), distinct from the quantile fields, and is
// used to compute time-share. *Has* flags distinguish "0 measured" from
// "no series in window" the same way REDStats does.
type Operation struct {
	Name             string            `json:"operation" yaml:"operation"`
	Labels           map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	RatePerSecond    float64           `json:"rate_per_second" yaml:"rate_per_second"`
	ErrorRatePerSec  float64           `json:"error_rate_per_second" yaml:"error_rate_per_second"`
	ErrorPercent     float64           `json:"error_percent" yaml:"error_percent"`
	AvgSeconds       float64           `json:"avg_seconds" yaml:"avg_seconds"`
	P50Seconds       float64           `json:"p50_seconds" yaml:"p50_seconds"`
	P95Seconds       float64           `json:"p95_seconds" yaml:"p95_seconds"`
	P99Seconds       float64           `json:"p99_seconds" yaml:"p99_seconds"`
	TimeSharePercent float64           `json:"time_share_percent" yaml:"time_share_percent"`
	HasTraffic       bool              `json:"has_traffic" yaml:"has_traffic"`
	HasErrors        bool              `json:"has_errors" yaml:"has_errors"`
	HasAvgLatency    bool              `json:"has_avg_latency" yaml:"has_avg_latency"`
	HasLatencyP50    bool              `json:"has_latency_p50" yaml:"has_latency_p50"`
	HasLatencyP95    bool              `json:"has_latency_p95" yaml:"has_latency_p95"`
	HasLatencyP99    bool              `json:"has_latency_p99" yaml:"has_latency_p99"`
}

// OperationsResponse is the top-level shape for the operations command:
// the service identity that was queried plus the per-operation rows.
// Wrapping the slice keeps room for future metadata (page tokens, totals,
// truncation flags) without changing the JSON contract.
type OperationsResponse struct {
	Service     Service     `json:"service" yaml:"service"`
	Window      string      `json:"window" yaml:"window"`
	MetricsMode MetricsMode `json:"metrics_mode" yaml:"metrics_mode"`
	SpanKinds   string      `json:"span_kinds" yaml:"span_kinds"`
	GroupBy     []string    `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	Items       []Operation `json:"items" yaml:"items"`
}

// opAggKey identifies an operations row: the span_name plus the
// canonical --group-by combination (empty when not grouping, so the map
// collapses to one row per span_name exactly as before).
type opAggKey struct {
	name     string
	groupKey string
}

// extractOperations collapses a Prometheus instant response into a map
// keyed by (span_name, group). Samples with empty span_name or
// unparseable values are dropped so callers can treat absence as
// "no data". groupLabels are the --group-by dimensions carried in the
// bucket.
func extractOperations(resp *prometheus.QueryResponse, groupLabels []string) map[opAggKey]groupBucket {
	out := make(map[opAggKey]groupBucket)
	if resp == nil {
		return out
	}
	for _, sample := range resp.Data.Result {
		op := sample.Metric["span_name"]
		if op == "" {
			continue
		}
		f, ok := sampleScalar(sample)
		if !ok {
			continue
		}
		gkey, glabels := groupKeyFor(sample.Metric, groupLabels)
		out[opAggKey{name: op, groupKey: gkey}] = groupBucket{value: f, labels: glabels}
	}
	return out
}

// bucketLabelsFor returns the group-label map for key, preferring
// whichever of the supplied maps carries it (rate is usually present).
func edgeLabelsFor(key edgeKey, maps ...map[edgeKey]groupBucket) map[string]string {
	for _, m := range maps {
		if b, ok := m[key]; ok {
			return b.labels
		}
	}
	return nil
}

func opLabelsFor(key opAggKey, maps ...map[opAggKey]groupBucket) map[string]string {
	for _, m := range maps {
		if b, ok := m[key]; ok {
			return b.labels
		}
	}
	return nil
}

// mergeEdges joins per-quantity maps (rate, errors, p95) into one row
// per (peer, connection_type, group), then sorts by rate desc with a
// stable (group, namespace, name, connType) tiebreak. HasErrors is
// inferred when rate>0 — a healthy edge with no STATUS_CODE_ERROR series
// prints 0% rather than "no data". Matches the convention from REDStats /
// Operation. groupBy orders the group tiebreak.
func mergeEdges(rates, errors, p95s map[edgeKey]groupBucket, groupBy []string) []Edge {
	keys := make(map[edgeKey]struct{})
	for _, m := range []map[edgeKey]groupBucket{rates, errors, p95s} {
		for k := range m {
			keys[k] = struct{}{}
		}
	}
	out := make([]Edge, 0, len(keys))
	for k := range keys {
		rate, hasRate := rates[k]
		errRate, hasErr := errors[k]
		p95, hasP95 := p95s[k]
		hasTraffic := hasRate && rate.value > 0
		out = append(out, Edge{
			Peer:            Service{Name: k.name, Namespace: k.namespace},
			ConnectionType:  k.connType,
			Labels:          edgeLabelsFor(k, rates, errors, p95s),
			RatePerSecond:   rate.value,
			ErrorRatePerSec: errRate.value,
			ErrorPercent:    computeErrorPercent(errRate.value, rate.value),
			P95Seconds:      p95.value,
			HasErrors:       hasErr || hasTraffic,
			HasLatency:      hasP95,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RatePerSecond != out[j].RatePerSecond {
			return out[i].RatePerSecond > out[j].RatePerSecond
		}
		if gi, gj := groupLabelString(out[i].Labels, groupBy), groupLabelString(out[j].Labels, groupBy); gi != gj {
			return gi < gj
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

// mergeOperations joins per-quantity maps (rate, errors, avg, p50, p95,
// p99) into one row per distinct (span_name, group), then computes
// time-share client-side. Time-share is normalized WITHIN each group —
// the denominator is the sum of (avg × rate) across the operations
// sharing that group key — so a per-cluster breakdown has each cluster's
// operations sum to ~100%, which is what makes cross-group outliers
// comparable. Operations missing an avg-latency signal contribute 0.
func mergeOperations(rates, errors, avgs, p50s, p95s, p99s map[opAggKey]groupBucket, groupBy []string) []Operation {
	keys := make(map[opAggKey]struct{})
	for _, m := range []map[opAggKey]groupBucket{rates, errors, avgs, p50s, p95s, p99s} {
		for k := range m {
			keys[k] = struct{}{}
		}
	}

	out := make([]Operation, 0, len(keys))
	rowGroupKeys := make([]string, 0, len(keys))
	totalWall := make(map[string]float64)
	for k := range keys {
		rate, hasRate := rates[k]
		errRate, hasErr := errors[k]
		avg, hasAvg := avgs[k]
		p50, hasP50 := p50s[k]
		p95, hasP95 := p95s[k]
		p99, hasP99 := p99s[k]

		hasTraffic := hasRate && rate.value > 0
		// Once we know there's traffic, missing error series ⇒ 0 errors,
		// not "unknown" — same logic as REDStats.HasErrors in services get.
		hasErrors := hasErr || hasTraffic
		if hasAvg && hasTraffic {
			totalWall[k.groupKey] += avg.value * rate.value
		}
		out = append(out, Operation{
			Name:            k.name,
			Labels:          opLabelsFor(k, rates, errors, avgs, p50s, p95s, p99s),
			RatePerSecond:   rate.value,
			ErrorRatePerSec: errRate.value,
			ErrorPercent:    computeErrorPercent(errRate.value, rate.value),
			AvgSeconds:      avg.value,
			P50Seconds:      p50.value,
			P95Seconds:      p95.value,
			P99Seconds:      p99.value,
			HasTraffic:      hasTraffic,
			HasErrors:       hasErrors,
			HasAvgLatency:   hasAvg,
			HasLatencyP50:   hasP50,
			HasLatencyP95:   hasP95,
			HasLatencyP99:   hasP99,
		})
		rowGroupKeys = append(rowGroupKeys, k.groupKey)
	}

	for i := range out {
		if out[i].HasAvgLatency && out[i].HasTraffic {
			if tw := totalWall[rowGroupKeys[i]]; tw > 0 {
				out[i].TimeSharePercent = (out[i].AvgSeconds * out[i].RatePerSecond / tw) * 100
			}
		}
	}

	// Default order: group asc (rows cluster by group), then time-share
	// desc, then name asc so equal-share rows are reproducible. When not
	// grouping, the group key is "" for every row and this collapses to
	// the original (time-share desc, name asc).
	sort.Slice(out, func(i, j int) bool {
		if gi, gj := groupLabelString(out[i].Labels, groupBy), groupLabelString(out[j].Labels, groupBy); gi != gj {
			return gi < gj
		}
		if out[i].TimeSharePercent != out[j].TimeSharePercent {
			return out[i].TimeSharePercent > out[j].TimeSharePercent
		}
		return out[i].Name < out[j].Name
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

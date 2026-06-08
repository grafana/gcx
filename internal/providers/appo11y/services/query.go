// Package services implements the `gcx appo11y services` command group.
//
// Service discovery mirrors the grafana/app-observability-app plugin: the
// `target_info` metric (OTel resource attributes) is treated as the inventory
// of services for a stack, and `job` is the service identifier.
package services

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
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

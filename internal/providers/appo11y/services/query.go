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

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/promql-builder/go/promql"
)

// defaultTargetInfoMetric is the canonical OpenTelemetry target_info metric.
// The app-observability-app plugin abstracts this behind `${metricName:targetInfo}`
// to support alternative metric modes; for now gcx hardcodes the default.
const defaultTargetInfoMetric = "target_info"

// matcherPattern accepts <label><op><value> where op is one of = != =~ !~.
// Value may be quoted or bare (bare means we'll quote it).
var matcherPattern = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)(=~|!~|!=|=)(.*)$`)

// groupByLabels is the projection every services discovery query uses.
// `job` and `telemetry_sdk_language` mirror the plugin discovery query; the
// remaining labels are surfaced in `--output wide`. Including them in the
// group-by keeps discovery to a single round-trip — labels missing on a given
// series simply render as empty strings, which the codec maps to "-".
//
// extra is appended (deduplicated) so `--columns` can pull in additional
// target_info labels without a second query.
func groupByLabels(extra []string) []string {
	base := append([]string{"telemetry_sdk_language", "job"}, wideLabels()...)
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
	switch m.Op {
	case "!=":
		return v.LabelNeq(m.Label, m.Value)
	case "=~":
		return v.LabelMatchRegexp(m.Label, m.Value)
	case "!~":
		return v.LabelNotMatchRegexp(m.Label, m.Value)
	default: // "="
		return v.Label(m.Label, m.Value)
	}
}

// buildServicesQuery returns a PromQL expression that groups the target_info
// inventory. It mirrors the plugin query at
// plugin/src/modules/services/utils/servicesQueryBuilder.ts, expanded to
// also project the metadata labels the plugin enriches in a second query.
//
// matchers are already-validated label filters; metric defaults to
// "target_info"; extraLabels are appended to the group-by projection for
// `--columns`.
func buildServicesQuery(metric string, matchers []Matcher, extraLabels []string) (string, error) {
	if metric == "" {
		metric = defaultTargetInfoMetric
	}
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
type Service struct {
	Name     string            `json:"name" yaml:"name"`
	Language string            `json:"language,omitempty" yaml:"language,omitempty"`
	Labels   map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
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

// parseServicesResponse converts a Prometheus instant-query result into a
// deduplicated, sorted slice of Services. Multiple samples can share the same
// (job, language) when a service has varying resource attributes (e.g. running
// in two Kubernetes namespaces); we merge them, keeping the first non-empty
// value seen for each metadata label.
func parseServicesResponse(resp *prometheus.QueryResponse) ([]Service, error) {
	if resp == nil {
		return nil, errors.New("nil query response")
	}
	type key struct{ name, language string }
	byKey := make(map[key]*Service)
	for _, sample := range resp.Data.Result {
		job := sample.Metric["job"]
		if job == "" {
			continue
		}
		k := key{name: job, language: sample.Metric["telemetry_sdk_language"]}
		svc, ok := byKey[k]
		if !ok {
			svc = &Service{Name: job, Language: k.language}
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Language < out[j].Language
	})
	return out, nil
}

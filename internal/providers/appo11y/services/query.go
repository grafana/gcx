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
)

// defaultTargetInfoMetric is the canonical OpenTelemetry target_info metric.
// The app-observability-app plugin abstracts this behind `${metricName:targetInfo}`
// to support alternative metric modes; for now gcx hardcodes the default.
const defaultTargetInfoMetric = "target_info"

// matcherPattern accepts <label><op><value> where op is one of = != =~ !~.
// Value may be quoted or bare (bare means we'll quote it).
var matcherPattern = regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_]*)(=~|!~|!=|=)(.*)$`)

// groupByLabels is the projection that every services discovery query uses.
// `job` and `telemetry_sdk_language` mirror the plugin discovery query; the
// remaining labels are surfaced in `--output wide`. Including them in the
// group-by keeps discovery to a single round-trip — labels missing on a given
// series simply render as empty strings, which the codec maps to "-".
func groupByLabels() []string {
	return append([]string{"telemetry_sdk_language", "job"}, wideLabels()...)
}

// buildServicesQuery returns a PromQL expression that groups the target_info
// inventory. It mirrors the plugin query at
// plugin/src/modules/services/utils/servicesQueryBuilder.ts, expanded to
// also project the metadata labels the plugin enriches in a second query.
//
// filters are PromQL matcher fragments already validated by parseFilter.
// metric defaults to "target_info".
func buildServicesQuery(metric string, filters []string) string {
	if metric == "" {
		metric = defaultTargetInfoMetric
	}
	selector := metric
	if len(filters) > 0 {
		selector = fmt.Sprintf("%s{%s}", metric, strings.Join(filters, ", "))
	}
	return fmt.Sprintf("group by (%s) (%s)", strings.Join(groupByLabels(), ", "), selector)
}

// parseFilter validates a single `label<op>value` filter and returns a PromQL
// matcher fragment ready to drop into a selector. Bare values are wrapped in
// double quotes.
func parseFilter(raw string) (string, error) {
	m := matcherPattern.FindStringSubmatch(raw)
	if m == nil {
		return "", fmt.Errorf("invalid --filter %q: expected <label><op><value> where op is = != =~ !~", raw)
	}
	label, op, val := m[1], m[2], m[3]
	val = strings.TrimSpace(val)
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		return label + op + val, nil
	}
	val = strings.ReplaceAll(val, `\`, `\\`)
	val = strings.ReplaceAll(val, `"`, `\"`)
	return fmt.Sprintf(`%s%s"%s"`, label, op, val), nil
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

package prometheus

import (
	"github.com/prometheus/common/model"
	promlabels "github.com/prometheus/prometheus/model/labels"
	promparser "github.com/prometheus/prometheus/promql/parser"
)

// PromQLFilter is a single label matcher extracted from a PromQL vector
// selector, e.g. the `job="grafana"` in `up{job="grafana"}`.
type PromQLFilter struct {
	Label    string
	Operator string // one of =, !=, =~, !~
	Value    string
}

// ParseSimpleVectorQuery extracts the metric name and label matchers from a
// safe subset of PromQL, for building a Metrics Drilldown deep link: a bare
// vector selector (metric{...}), or a linear chain of range-vector/function/
// aggregation wrappers around exactly one underlying selector
// (rate(metric{...}[5m]), sum(rate(metric{...}[5m]))). Anything involving
// more than one distinct metric reference (e.g. a binary expression like
// sum(rate(a[5m]) + rate(b[5m]))) or unrecognized structure (subqueries,
// multi-arg calls, ...) returns ok=false — deliberately stricter than
// Metrics Drilldown's own permissive AST walk, which can union matchers
// across unrelated metrics into one misleading link.
func ParseSimpleVectorQuery(expr string) (string, []PromQLFilter, bool) {
	parsed, err := promparser.NewParser(promparser.Options{}).ParseExpr(expr)
	if err != nil {
		return "", nil, false
	}

	vs, ok := unwrapToSingleSelector(parsed)
	if !ok {
		return "", nil, false
	}

	var filters []PromQLFilter
	for _, m := range vs.LabelMatchers {
		if m.Name == model.MetricNameLabel {
			continue
		}
		op, ok := matchTypeOperator(m.Type)
		if !ok {
			return "", nil, false
		}
		filters = append(filters, PromQLFilter{Label: m.Name, Operator: op, Value: m.Value})
	}

	return vs.Name, filters, true
}

// unwrapToSingleSelector walks a linear chain of matrix selectors, single-arg
// function calls, aggregations, and parens down to exactly one underlying
// vector selector. Any binary expression, subquery, or multi-arg call
// encountered along the way means the query doesn't reduce to a single
// metric reference, so it returns ok=false.
func unwrapToSingleSelector(expr promparser.Expr) (*promparser.VectorSelector, bool) {
	switch e := expr.(type) {
	case *promparser.VectorSelector:
		return e, true
	case *promparser.MatrixSelector:
		return unwrapToSingleSelector(e.VectorSelector)
	case *promparser.ParenExpr:
		return unwrapToSingleSelector(e.Expr)
	case *promparser.Call:
		if len(e.Args) != 1 {
			return nil, false
		}
		return unwrapToSingleSelector(e.Args[0])
	case *promparser.AggregateExpr:
		return unwrapToSingleSelector(e.Expr)
	default:
		return nil, false
	}
}

func matchTypeOperator(t promlabels.MatchType) (string, bool) {
	switch t {
	case promlabels.MatchEqual:
		return "=", true
	case promlabels.MatchNotEqual:
		return "!=", true
	case promlabels.MatchRegexp:
		return "=~", true
	case promlabels.MatchNotRegexp:
		return "!~", true
	default:
		return "", false
	}
}

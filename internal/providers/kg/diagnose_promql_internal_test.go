package kg

import (
	"testing"

	promparser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostile contains both a quote and a backslash, which break a matcher
// built with string formatting.
const hostile = `svc"} or vector(1) #\`

// assertMatcherValues parses query as PromQL and checks that every expected
// label matcher round-trips with its original, unescaped value.
func assertMatcherValues(t *testing.T, query string, want map[string]string) {
	t.Helper()

	expr, err := promparser.NewParser(promparser.Options{}).ParseExpr(query)
	require.NoError(t, err, "query must parse: %s", query)

	got := map[string]string{}
	promparser.Inspect(expr, func(node promparser.Node, _ []promparser.Node) error {
		if vs, ok := node.(*promparser.VectorSelector); ok {
			for _, m := range vs.LabelMatchers {
				if m.Name != "__name__" {
					got[m.Name] = m.Value
				}
			}
		}
		return nil
	})
	for name, value := range want {
		assert.Equal(t, value, got[name], "matcher %s in %s", name, query)
	}
}

func TestMetricChecks_EscapesEnvAndNamespace(t *testing.T) {
	for _, def := range metricChecks(hostile, hostile) {
		t.Run(def.Name, func(t *testing.T) {
			expr, err := promparser.NewParser(promparser.Options{}).ParseExpr(def.Query)
			require.NoError(t, err, "query must parse: %s", def.Query)
			_, isAgg := expr.(*promparser.AggregateExpr)
			assert.True(t, isAgg, "query must stay a single count(): %s", def.Query)
		})
	}

	defs := metricChecks(hostile, hostile)
	assertMatcherValues(t, defs[0].Query, map[string]string{"deployment_environment": hostile})
	assertMatcherValues(t, defs[1].Query, map[string]string{
		"client_deployment_environment": hostile,
		"client_service_namespace":      hostile,
	})
	assertMatcherValues(t, defs[2].Query, map[string]string{"asserts_env": hostile, "namespace": hostile})
}

func TestServiceQueries_EscapeServiceName(t *testing.T) {
	matchers := []labelMatcher{eqMatcher("service", hostile), eqMatcher("asserts_env", hostile)}
	query := countQuery("asserts:relation:calls", matchers...)
	assertMatcherValues(t, query, map[string]string{"service": hostile, "asserts_env": hostile})

	hint := metricsQueryHint(vectorSelector("traces_target_info", eqMatcher("service_name", hostile)))
	assert.Contains(t, hint, `service_name="svc\"} or vector(1) #\\"`)
}

func TestCountQuery_NegativeMatcher(t *testing.T) {
	query := countQuery("traces_service_graph_request_total",
		eqMatcher("server_deployment_environment", "prod"), neqMatcher("client", "user"))
	assert.Equal(t, `count(traces_service_graph_request_total{server_deployment_environment="prod",client!="user"})`, query)
}

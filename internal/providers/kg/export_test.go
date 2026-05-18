package kg

import (
	"context"

	"github.com/grafana/gcx/internal/query/prometheus"
)

// ScopeFlags is an exported alias for scopeFlags, used only in tests.
type ScopeFlags = scopeFlags

// NewTestScopeFlags constructs a ScopeFlags for use in tests.
func NewTestScopeFlags(env, site, namespace string) ScopeFlags {
	return ScopeFlags{env: env, site: site, namespace: namespace}
}

// ValidateScopes wraps the unexported validateScopes method for testing.
func (f ScopeFlags) ValidateScopes(ctx context.Context, c *Client) error {
	return f.validateScopes(ctx, c)
}

// InsightMatcher is an exported alias for the unexported insightMatcher type,
// used by tests.
type InsightMatcher = insightMatcher

// ParseInsightFlag wraps the unexported parseInsightFlag for testing.
func ParseInsightFlag(s string) (InsightMatcher, error) {
	return parseInsightFlag(s)
}

// FilterByInsightMatchers wraps the unexported filterByInsightMatchers for testing.
func FilterByInsightMatchers(results []SearchResult, matchers []InsightMatcher) []SearchResult {
	return filterByInsightMatchers(results, matchers)
}

// RunDiagnose wraps the unexported runDiagnose function for testing.
// Pass nil promClient and empty datasourceUID to skip metric checks.
func RunDiagnose(ctx context.Context, client *Client, scope *ScopeFlags, promClient *prometheus.Client, datasourceUID string) DiagnoseResult {
	return runDiagnose(ctx, client, scope, promClient, datasourceUID)
}

// RunServiceDiagnose wraps the unexported runServiceDiagnose function for testing.
func RunServiceDiagnose(ctx context.Context, client *Client, serviceName string, scope *ScopeFlags, promClient *prometheus.Client, datasourceUID string) ServiceDiagnoseResult {
	return runServiceDiagnose(ctx, client, serviceName, scope, promClient, datasourceUID)
}

// RunLabelsDiagnose wraps the unexported runLabelsDiagnose function for testing.
func RunLabelsDiagnose(ctx context.Context, client *Client, promClient *prometheus.Client, datasourceUID string) LabelsDiagnoseResult {
	return runLabelsDiagnose(ctx, client, promClient, datasourceUID)
}

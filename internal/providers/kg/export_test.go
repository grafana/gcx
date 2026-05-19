package kg

import (
	"context"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/tempo"
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
// Trace-side checks are skipped (nil tempo client); use RunDiagnoseWithTempo
// to exercise those.
func RunDiagnose(ctx context.Context, client *Client, scope *ScopeFlags, promClient *prometheus.Client, datasourceUID string) DiagnoseResult {
	return runDiagnose(ctx, client, scope, promClient, datasourceUID, nil, "")
}

// RunDiagnoseWithTempo wraps runDiagnose with full client wiring for tests
// that exercise both metric and trace-side checks.
func RunDiagnoseWithTempo(ctx context.Context, client *Client, scope *ScopeFlags, promClient *prometheus.Client, datasourceUID string, tempoClient *tempo.Client, tempoDatasourceUID string) DiagnoseResult {
	return runDiagnose(ctx, client, scope, promClient, datasourceUID, tempoClient, tempoDatasourceUID)
}

// RunServiceDiagnose wraps the unexported runServiceDiagnose function for testing.
func RunServiceDiagnose(ctx context.Context, client *Client, serviceName string, scope *ScopeFlags, promClient *prometheus.Client, datasourceUID string) ServiceDiagnoseResult {
	return runServiceDiagnose(ctx, client, serviceName, scope, promClient, datasourceUID)
}

// RunLabelsDiagnose wraps the unexported runLabelsDiagnose function for testing.
func RunLabelsDiagnose(ctx context.Context, client *Client, promClient *prometheus.Client, datasourceUID string) LabelsDiagnoseResult {
	return runLabelsDiagnose(ctx, client, promClient, datasourceUID)
}

// CheckContainerImageLabelDrift wraps the unexported check for testing.
func CheckContainerImageLabelDrift(ctx context.Context, client *prometheus.Client, datasourceUID, namespace string) *CheckResult {
	return checkContainerImageLabelDrift(ctx, client, datasourceUID, namespace)
}

// CheckResourceFamilyCoverage wraps the unexported check for testing.
func CheckResourceFamilyCoverage(ctx context.Context, client *prometheus.Client, datasourceUID, env, namespace string) *CheckResult {
	return checkResourceFamilyCoverage(ctx, client, datasourceUID, env, namespace)
}

// ExpectedResourceTypes exposes the canonical asserts_resource_type set
// for assertion in tests.
func ExpectedResourceTypes() []string {
	out := make([]string, len(expectedResourceTypes))
	copy(out, expectedResourceTypes)
	return out
}

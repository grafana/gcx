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

// InterpretServiceResults wraps the unexported interpretServiceResults function for testing.
func InterpretServiceResults(r *ServiceDiagnoseResult) ([]string, []string) {
	return interpretServiceResults(r)
}

// ComputeServiceSummary wraps ServiceDiagnoseResult.computeSummary for testing.
func ComputeServiceSummary(r *ServiceDiagnoseResult) {
	r.computeSummary()
}

// RunLabelsDiagnose wraps the unexported runLabelsDiagnose function for testing.
func RunLabelsDiagnose(ctx context.Context, client *Client, promClient *prometheus.Client, datasourceUID string) LabelsDiagnoseResult {
	return runLabelsDiagnose(ctx, client, promClient, datasourceUID)
}

// --- Orientation test entry points ---

// ComputeOrientation wraps the unexported computeOrientation function for testing.
func ComputeOrientation(in OrientationInput, scope *ScopeFlags) Orientation {
	return computeOrientation(in, scope)
}

// BuildEntityOverview wraps the unexported buildEntityOverview function for testing.
func BuildEntityOverview(in OrientationInput) EntityOverview {
	return buildEntityOverview(in)
}

// BuildScopeSummary wraps the unexported buildScopeSummary function for testing.
func BuildScopeSummary(in OrientationInput, scope *ScopeFlags) ScopeSummary {
	return buildScopeSummary(in, scope)
}

// DetectNoEntities wraps the unexported detectNoEntities function for testing.
func DetectNoEntities(in OrientationInput, overview EntityOverview) *MatchedScenario {
	return detectNoEntities(in, overview)
}

// DetectEntitiesNoEdges wraps the unexported detectEntitiesNoEdges function for testing.
func DetectEntitiesNoEdges(in OrientationInput, overview EntityOverview, scope *ScopeFlags) *MatchedScenario {
	return detectEntitiesNoEdges(in, overview, scope)
}

// DetectCantFilter wraps the unexported detectCantFilter function for testing.
func DetectCantFilter(in OrientationInput, scope *ScopeFlags) *MatchedScenario {
	return detectCantFilter(in, scope)
}

// DetectMissingEntities wraps the unexported detectMissingEntities function for testing.
func DetectMissingEntities(in OrientationInput, scope *ScopeFlags) *MatchedScenario {
	return detectMissingEntities(in, scope)
}

// BuildStartingPoints wraps the unexported buildStartingPoints function for testing.
func BuildStartingPoints(scope ScopeSummary) []StartingPoint {
	return buildStartingPoints(scope)
}

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

// FilterBySeverity wraps the unexported filterBySeverity for testing.
func FilterBySeverity(results []SearchResult, sev string) []SearchResult {
	return filterBySeverity(results, sev)
}

// BuildInsightSearchRequest exposes buildInsightSearchRequest for tests.
//
//nolint:gochecknoglobals // Test-only export alias.
var BuildInsightSearchRequest = buildInsightSearchRequest

// RunDiagnose wraps the unexported runDiagnose function for testing.
// Pass nil promClient and empty datasourceUID to skip metric checks.
func RunDiagnose(ctx context.Context, client *Client, scope *ScopeFlags, promClient *prometheus.Client, datasourceUID string) DiagnoseResult {
	return runDiagnose(ctx, client, scope, promClient, datasourceUID)
}

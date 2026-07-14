package kg

import (
	"context"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
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

// PipelineHealthFromSummary wraps the unexported pipelineHealthFromSummary
// function for testing.
func PipelineHealthFromSummary(s DiagnoseSummary) PipelineHealth {
	return pipelineHealthFromSummary(s)
}

// --- KG write-flag helper test entry points ---

func ParseEntityRefToken(token string) (EntityRef, error) { return parseEntityRefToken(token) }
func ParseTTL(s string) (int64, error)                    { return parseTTL(s) }
func ValidateWritableDomain(d string) error               { return validateWritableDomain(d) }
func ValidateDomain(d string) error                       { return validateDomain(d) }
func ValidateIdentifier(s, field string) error            { return validateIdentifier(s, field) }

// FakeWriteLoader is a RESTConfigLoader test double for write-command tests.
type FakeWriteLoader struct {
	Cfg    internalconfig.NamespacedRESTConfig
	CfgErr error
}

func (f *FakeWriteLoader) LoadGrafanaConfig(_ context.Context) (internalconfig.NamespacedRESTConfig, error) {
	return f.Cfg, f.CfgErr
}

// --- KG write-command test entry points ---

func NewEntitiesCreateCommand(loader RESTConfigLoader) *cobra.Command {
	return newEntitiesCreateCommand(loader)
}

func NewEntitiesDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	return newEntitiesDeleteCommand(loader)
}

func BuildEntityWriteRequest(domain, entityType, name string, scope, property map[string]string, ttl string) (EntityWriteRequest, error) {
	return buildEntityWriteRequest(domain, entityType, name, scope, property, ttl)
}

func NewRelationshipsCreateCommand(loader RESTConfigLoader) *cobra.Command {
	return newRelationshipsCreateCommand(loader)
}

// NewSuppressionsCommand exposes the suppressions command group for tests.
func NewSuppressionsCommand(loader RESTConfigLoader) *cobra.Command {
	return newSuppressionsCommand(loader)
}

func NewRelationshipsDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	return newRelationshipsDeleteCommand(loader)
}

func BuildRelationshipWriteRequest(domain, relType, from string, fromScope map[string]string, to string, toScope, property map[string]string, ttl string) (RelationshipWriteRequest, error) {
	return buildRelationshipWriteRequest(domain, relType, from, fromScope, to, toScope, property, ttl)
}

// DiscoverEntityScope wraps the unexported discoverEntityScope for testing.
// It builds a throwaway cobra command to satisfy the context/stderr deps.
func DiscoverEntityScope(client *Client, entityType, name, domain string, startMs, endMs int64) (map[string]string, error) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return discoverEntityScope(cmd, client, entityType, name, domain, startMs, endMs)
}

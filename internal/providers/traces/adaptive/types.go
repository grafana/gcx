package traces

import "github.com/grafana/gcx/internal/resources/adapter"

// PolicyTypedLabels holds structured labels attached to policies and recommendations.
type PolicyTypedLabels struct {
	Type           string            `json:"type,omitempty"`
	SourceForecast string            `json:"source_forecast,omitempty"`
	SourceLabels   map[string]string `json:"source_labels,omitempty"`
}

// Policy represents an adaptive traces sampling policy.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Policy struct {
	ID                      string             `json:"id,omitempty"`
	Type                    string             `json:"type"`
	Name                    string             `json:"name"`
	Body                    map[string]any     `json:"body,omitzero"`
	Labels                  *PolicyTypedLabels `json:"labels,omitempty"`
	ExpiresAt               string             `json:"expires_at,omitempty"`
	VersionRecommendationID string             `json:"version_recommendation_id,omitempty"`
	VersionCreatedAt        string             `json:"version_created_at,omitempty"`
	VersionCreatedBy        string             `json:"version_created_by,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer for TypedCRUD compatibility.
func (p Policy) GetResourceName() string { return p.ID }

// SetResourceName implements adapter.ResourceIdentity for TypedCRUD compatibility.
func (p *Policy) SetResourceName(name string) { p.ID = name }

// Compile-time assertion that *Policy satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &Policy{}

// TenantConfig represents the writable configuration for an adaptive traces tenant.
// The API does not support partial updates — all fields must be provided on
// every update. Omitted fields will be reset to their zero values.
type TenantConfig struct {
	DisableAnomalyPolicies          bool   `json:"disable_anomaly_policies" yaml:"disable_anomaly_policies"`
	SpanNameSemconvTransformEnabled bool   `json:"span_name_semconv_transform_enabled" yaml:"span_name_semconv_transform_enabled"`
	SpanNameSemconvVersion          string `json:"span_name_semconv_version" yaml:"span_name_semconv_version"`
}

// ReadonlyTenantConfig is the response model for GET /api/v1/config.
// It is a superset of TenantConfig, including computed fields like the
// anomaly rate limit that are set by the forecaster.
type ReadonlyTenantConfig struct {
	AnomalyRateLimitBytesPerSec     float64 `json:"anomaly_rate_limit_bytes_per_sec,omitempty" yaml:"anomaly_rate_limit_bytes_per_sec,omitempty"`
	DisableAnomalyPolicies          bool    `json:"disable_anomaly_policies" yaml:"disable_anomaly_policies"`
	SpanNameSemconvTransformEnabled bool    `json:"span_name_semconv_transform_enabled" yaml:"span_name_semconv_transform_enabled"`
	SpanNameSemconvVersion          string  `json:"span_name_semconv_version" yaml:"span_name_semconv_version"`
}

// PolicySeed represents the policy configuration embedded in a recommendation action.
type PolicySeed struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Name             string `json:"name"`
	Body             any    `json:"body"`
	ExpiresInSeconds *int64 `json:"expires_in_seconds,omitempty"`
}

// RecommendationAction describes a single action within a recommendation.
type RecommendationAction struct {
	Action               string      `json:"action"`
	PolicyID             string      `json:"policy_id,omitempty"`
	RecommendationSeedID string      `json:"recommendation_seed_id"`
	Seed                 *PolicySeed `json:"seed,omitempty"`
}

// Recommendation represents an adaptive traces sampling recommendation.
type Recommendation struct {
	ID          string                 `json:"id"`
	Message     string                 `json:"message"`
	Tags        []string               `json:"tags"`
	Labels      *PolicyTypedLabels     `json:"labels,omitempty"`
	CreatedAt   string                 `json:"created_at"`
	Applied     bool                   `json:"applied"`
	AppliedAt   string                 `json:"applied_at,omitempty"`
	Dismissed   bool                   `json:"dismissed"`
	DismissedAt string                 `json:"dismissed_at,omitempty"`
	Stale       bool                   `json:"stale"`
	Actions     []RecommendationAction `json:"actions"`
}

package metrics

import (
	"context"
	"encoding/json"
	"fmt"

	adaptiveauth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		exemptionDescriptorVar.GroupVersionKind(),
		adapter.SpecFieldKey("metric"),
	)
}

const (
	RuleAPIVersion = "adaptive-metrics.ext.grafana.app/v1alpha1"
	RuleKind       = "AggregationRule"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var ruleDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-metrics.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     RuleKind,
	Singular: "aggregationrule",
	Plural:   "aggregationrules",
}

// RuleDescriptor returns the resource descriptor for adaptive metrics aggregation rules.
func RuleDescriptor() resources.Descriptor { return ruleDescriptorVar }

// RuleSchema returns a JSON Schema for the AggregationRule resource type.
func RuleSchema() json.RawMessage {
	return adapter.SchemaFromType[MetricRule](RuleDescriptor())()
}

// RuleExample returns an example AggregationRule manifest as JSON.
func RuleExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": RuleAPIVersion,
		"kind":       RuleKind,
		"metadata":   map[string]any{"name": "my-metric"},
		"spec": map[string]any{
			"metric":       "my-metric",
			"match_type":   "exact",
			"drop_labels":  []string{"pod", "container"},
			"keep_labels":  []string{"job", "instance"},
			"aggregations": []string{"sum", "count"},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/metrics: failed to marshal rule example: %v", err))
	}
	return b
}

func newAdaptiveMetricsClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	signalAuth, err := adaptiveauth.ResolveSignalAuth(ctx, loader, "metrics")
	if err != nil {
		return nil, err
	}
	return NewClient(ctx, signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

// NewRuleTypedCRUD creates a TypedCRUD for adaptive metrics aggregation rules.
func NewRuleTypedCRUD(ctx context.Context, loader *providers.ConfigLoader, segment string) (*adapter.TypedCRUD[MetricRule], error) {
	backend, err := newRulesBackend(ctx, loader, segment)
	if err != nil {
		return nil, err
	}
	return newRuleTypedCRUD(backend), nil
}

func newRuleTypedCRUD(backend *rulesBackend) *adapter.TypedCRUD[MetricRule] {
	return &adapter.TypedCRUD[MetricRule]{
		ListFn:     backend.List,
		GetFn:      backend.Get,
		CreateFn:   backend.Create,
		UpdateFn:   backend.Update,
		DeleteFn:   func(ctx context.Context, name string) error { return backend.Delete(ctx, name) },
		Namespace:  "default",
		Descriptor: ruleDescriptorVar,
	}
}

// NewRuleAdapterFactory returns an adapter.Factory for adaptive metrics aggregation rules.
func NewRuleAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, err := NewRuleTypedCRUD(ctx, loader, "")
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// buildMetricsTypedCRUD builds a TypedCRUD for Adaptive Metrics resources.
func buildMetricsTypedCRUD[T adapter.ResourceNamer](
	desc resources.Descriptor,
	list func(context.Context) ([]T, error),
	get func(context.Context, string) (*T, error),
	create func(context.Context, *T) (*T, error),
	update func(context.Context, string, *T) (*T, error),
	del func(context.Context, string) error,
) *adapter.TypedCRUD[T] {
	return &adapter.TypedCRUD[T]{
		ListFn: func(ctx context.Context, limit int64) ([]T, error) {
			items, err := list(ctx)
			if err != nil {
				return nil, err
			}
			return adapter.TruncateSlice(items, limit), nil
		},
		GetFn:       get,
		CreateFn:    create,
		UpdateFn:    update,
		DeleteFn:    del,
		Namespace:   "default",
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
}

// ---------------------------------------------------------------------------
// Segment resource adapter
// ---------------------------------------------------------------------------

const (
	SegmentAPIVersion = "adaptive-metrics.ext.grafana.app/v1alpha1"
	SegmentKind       = "Segment"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var segmentDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-metrics.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     SegmentKind,
	Singular: "segment",
	Plural:   "segments",
}

// SegmentDescriptor returns the resource descriptor for Adaptive Metrics segments.
func SegmentDescriptor() resources.Descriptor { return segmentDescriptorVar }

// SegmentSchema returns a JSON Schema for the MetricSegment resource type.
func SegmentSchema() json.RawMessage {
	return adapter.SchemaFromType[MetricSegment](SegmentDescriptor())()
}

// SegmentExample returns an example MetricSegment manifest as JSON.
func SegmentExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": SegmentAPIVersion,
		"kind":       SegmentKind,
		"metadata":   map[string]any{"name": "my-segment"},
		"spec": map[string]any{
			"name":                "production",
			"selector":            `{env="production"}`,
			"fallback_to_default": false,
			"auto_apply":          map[string]any{"enabled": false},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/metrics: failed to marshal segment example: %v", err))
	}
	return b
}

// NewSegmentTypedCRUD creates a TypedCRUD for Adaptive Metrics segments.
func NewSegmentTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[MetricSegment], error) {
	client, err := newAdaptiveMetricsClient(ctx, loader)
	if err != nil {
		return nil, err
	}
	crud := buildMetricsTypedCRUD(segmentDescriptorVar,
		client.ListSegments,
		client.GetSegment,
		client.CreateSegment,
		func(ctx context.Context, id string, s *MetricSegment) (*MetricSegment, error) {
			return client.UpdateSegment(ctx, id, s)
		},
		client.DeleteSegment,
	)
	return crud, nil
}

// NewSegmentAdapterFactory returns an adapter.Factory for Adaptive Metrics segments.
func NewSegmentAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, err := NewSegmentTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// ---------------------------------------------------------------------------
// Exemption resource adapter
// ---------------------------------------------------------------------------

const (
	ExemptionAPIVersion = "adaptive-metrics.ext.grafana.app/v1alpha1"
	ExemptionKind       = "Exemption"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var exemptionDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-metrics.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     ExemptionKind,
	Singular: "exemption",
	Plural:   "exemptions",
}

// ExemptionDescriptor returns the resource descriptor for Adaptive Metrics exemptions.
func ExemptionDescriptor() resources.Descriptor { return exemptionDescriptorVar }

// ExemptionSchema returns a JSON Schema for the MetricExemption resource type.
func ExemptionSchema() json.RawMessage {
	return adapter.SchemaFromType[MetricExemption](ExemptionDescriptor())()
}

// ExemptionExample returns an example MetricExemption manifest as JSON.
func ExemptionExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": ExemptionAPIVersion,
		"kind":       ExemptionKind,
		"metadata":   map[string]any{"name": "my-exemption"},
		"spec": map[string]any{
			"metric":          "my_critical_metric",
			"match_type":      "exact",
			"reason":          "Critical metric — exempt from recommendations",
			"active_interval": "30d",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/metrics: failed to marshal exemption example: %v", err))
	}
	return b
}

// NewExemptionTypedCRUD creates a TypedCRUD for Adaptive Metrics exemptions (default segment).
func NewExemptionTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[MetricExemption], error) {
	client, err := newAdaptiveMetricsClient(ctx, loader)
	if err != nil {
		return nil, err
	}
	const defaultSegment = ""
	crud := buildMetricsTypedCRUD(exemptionDescriptorVar,
		func(ctx context.Context) ([]MetricExemption, error) {
			return client.ListExemptions(ctx, defaultSegment)
		},
		func(ctx context.Context, id string) (*MetricExemption, error) {
			return client.GetExemption(ctx, id, defaultSegment)
		},
		func(ctx context.Context, e *MetricExemption) (*MetricExemption, error) {
			return client.CreateExemption(ctx, e, defaultSegment)
		},
		func(ctx context.Context, id string, e *MetricExemption) (*MetricExemption, error) {
			return client.UpdateExemption(ctx, id, e, defaultSegment)
		},
		func(ctx context.Context, id string) error {
			return client.DeleteExemption(ctx, id, defaultSegment)
		},
	)
	return crud, nil
}

// NewExemptionAdapterFactory returns an adapter.Factory for Adaptive Metrics exemptions.
func NewExemptionAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, err := NewExemptionTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

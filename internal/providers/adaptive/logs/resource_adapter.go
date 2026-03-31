package logs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	adaptiveauth "github.com/grafana/gcx/internal/providers/adaptive/auth"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	ExemptionAPIVersion = "adaptive-logs.ext.grafana.app/v1alpha1"
	ExemptionKind       = "Exemption"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var exemptionDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-logs.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     ExemptionKind,
	Singular: "exemption",
	Plural:   "exemptions",
}

// ExemptionDescriptor returns the resource descriptor for adaptive log exemptions.
func ExemptionDescriptor() resources.Descriptor { return exemptionDescriptorVar }

// ExemptionSchema returns a JSON Schema for the Exemption resource type.
func ExemptionSchema() json.RawMessage {
	return adapter.SchemaFromType[Exemption](ExemptionDescriptor())
}

// ExemptionExample returns an example Exemption manifest as JSON.
func ExemptionExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": ExemptionAPIVersion,
		"kind":       ExemptionKind,
		"metadata":   map[string]any{"name": "my-exemption"},
		"spec": map[string]any{
			"stream_selector": `{app="critical-service"}`,
			"reason":          "Critical service — exempt from log dropping",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/logs: failed to marshal example: %v", err))
	}
	return b
}

func newAdaptiveLogsClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	signalAuth, err := adaptiveauth.ResolveSignalAuth(ctx, loader, "logs")
	if err != nil {
		return nil, err
	}
	return NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

func buildLogsTypedCRUD[T adapter.ResourceNamer](
	desc resources.Descriptor,
	list func(context.Context) ([]T, error),
	get func(context.Context, string) (*T, error),
	create func(context.Context, *T) (*T, error),
	update func(context.Context, string, *T) (*T, error),
	del func(context.Context, string) error,
) *adapter.TypedCRUD[T] {
	return &adapter.TypedCRUD[T]{
		ListFn:      list,
		GetFn:       get,
		CreateFn:    create,
		UpdateFn:    update,
		DeleteFn:    del,
		Namespace:   "default",
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
}

// NewExemptionTypedCRUD creates a TypedCRUD for adaptive log exemptions.
func NewExemptionTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[Exemption], string, error) {
	client, err := newAdaptiveLogsClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := buildLogsTypedCRUD(exemptionDescriptorVar,
		client.ListExemptions,
		client.GetExemption,
		client.CreateExemption,
		client.UpdateExemption,
		client.DeleteExemption,
	)
	return crud, "default", nil
}

// NewExemptionAdapterFactory returns an adapter.Factory for adaptive log exemptions.
func NewExemptionAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewExemptionTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// ---------------------------------------------------------------------------
// LogSegment resource adapter
// ---------------------------------------------------------------------------

const (
	SegmentAPIVersion = "adaptive-logs.ext.grafana.app/v1alpha1"
	SegmentKind       = "Segment"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var segmentDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-logs.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     SegmentKind,
	Singular: "segment",
	Plural:   "segments",
}

// SegmentDescriptor returns the resource descriptor for adaptive log segments.
func SegmentDescriptor() resources.Descriptor { return segmentDescriptorVar }

// SegmentSchema returns a JSON Schema for the LogSegment resource type.
func SegmentSchema() json.RawMessage {
	return adapter.SchemaFromType[LogSegment](SegmentDescriptor())
}

// SegmentExample returns an example LogSegment manifest as JSON.
func SegmentExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": SegmentAPIVersion,
		"kind":       SegmentKind,
		"metadata":   map[string]any{"name": "my-segment"},
		"spec": map[string]any{
			"name":                "production-logs",
			"selector":            `{env="production"}`,
			"fallback_to_default": false,
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/logs: failed to marshal segment example: %v", err))
	}
	return b
}

// NewSegmentTypedCRUD creates a TypedCRUD for adaptive log segments.
func NewSegmentTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[LogSegment], string, error) {
	client, err := newAdaptiveLogsClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := buildLogsTypedCRUD(segmentDescriptorVar,
		client.ListSegments,
		client.GetSegment,
		client.CreateSegment,
		client.UpdateSegment,
		client.DeleteSegment,
	)
	return crud, "default", nil
}

// NewSegmentAdapterFactory returns an adapter.Factory for adaptive log segments.
func NewSegmentAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewSegmentTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

package traces

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

const (
	PolicyAPIVersion = "adaptive-traces.ext.grafana.app/v1alpha1"
	PolicyKind       = "Policy"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var policyDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-traces.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     PolicyKind,
	Singular: "policy",
	Plural:   "policies",
}

// PolicyDescriptor returns the resource descriptor for adaptive traces policies.
func PolicyDescriptor() resources.Descriptor { return policyDescriptorVar }

// PolicySchema returns the JSON schema for the Policy resource type.
func PolicySchema() json.RawMessage {
	return adapter.SchemaFromType[Policy](PolicyDescriptor())
}

// PolicyExample returns an example Policy manifest as JSON.
func PolicyExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": PolicyAPIVersion,
		"kind":       PolicyKind,
		"metadata":   map[string]any{"name": "my-policy"},
		"spec": map[string]any{
			"type": "probabilistic",
			"name": "Sample 10% of traces",
			"body": map[string]any{
				"sampling_percentage": 10.0,
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/traces: failed to marshal example: %v", err))
	}
	return b
}

// NewPolicyTypedCRUD creates a TypedCRUD for adaptive traces policies.
func NewPolicyTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[Policy], string, error) {
	signalAuth, err := adaptiveauth.ResolveSignalAuth(ctx, loader, "traces")
	if err != nil {
		return nil, "", err
	}
	client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

	crud := &adapter.TypedCRUD[Policy]{
		ListFn: func(ctx context.Context) ([]Policy, error) {
			return client.ListPolicies(ctx)
		},
		GetFn: func(ctx context.Context, name string) (*Policy, error) {
			return client.GetPolicy(ctx, name)
		},
		CreateFn: func(ctx context.Context, p *Policy) (*Policy, error) {
			return client.CreatePolicy(ctx, p)
		},
		UpdateFn: func(ctx context.Context, name string, p *Policy) (*Policy, error) {
			return client.UpdatePolicy(ctx, name, p)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			return client.DeletePolicy(ctx, name)
		},
		Namespace:   "default",
		StripFields: []string{"id"},
		Descriptor:  policyDescriptorVar,
	}
	return crud, "default", nil
}

// NewPolicyAdapterFactory returns a lazy adapter.Factory for adaptive traces policies.
func NewPolicyAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewPolicyTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

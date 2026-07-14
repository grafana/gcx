package definitions

import (
	"context"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for SLO definitions. Its
// Group/Version/Kind mirror SloResource()'s declaration below — kept as a
// small standalone helper because NewTypedCRUD (used by the hand-written
// commands.go) needs a *adapter.TypedCRUD[Slo] rather than the
// ResourceAdapter that SloResource()'s registration path builds.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "slo.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "SLO",
		Singular: "slo",
		Plural:   "slos",
	}
}

// SloResource declares the SLO definitions resource type end-to-end for
// adapter.NewProvider: identity (GVK), registration metadata, and client
// constructor. Declaring NaturalKey folds in
// RegisterNaturalKey(gvk, SpecFieldKey("name")) and declaring URLTemplate
// folds in the deeplink registration — neither needs a separate init().
// Schema and Example are derived from Slo/the value below rather than
// hand-written. A function (not a package-level var) to avoid a mutable
// shared global.
func SloResource() adapter.Resource[Slo] {
	return adapter.Resource[Slo]{
		Group:   "slo.ext.grafana.app",
		Version: "v1alpha1",
		Kind:    "SLO",

		NaturalKey:  "name",
		URLTemplate: "/a/grafana-slo-app/slo/{name}",
		StripFields: []string{"uuid", "readOnly"},

		Example: Slo{
			UUID:        "my-slo",
			Name:        "HTTP Availability",
			Description: "Tracks HTTP request success rate",
			Query: Query{
				Type: "freeform",
				Freeform: &FreeformQuery{
					Query: `sum(rate(http_requests_total{status!~"5.."}[5m])) / sum(rate(http_requests_total[5m]))`,
				},
			},
			Objectives: []Objective{{Value: 0.995, Window: "28d"}},
			Labels:     []Label{{Key: "team", Value: "platform"}},
		},

		NewClient: newAdapterClient,
	}
}

// newAdapterClient is SloResource's NewClient implementation. It builds the
// SLO client directly from ClientDeps.HTTP, constructing no transport of its
// own. The returned *Client implements the five capability interfaces (see
// the compile-time guards in client.go), so the capability seam wires all
// five verbs.
func newAdapterClient(_ context.Context, deps adapter.ClientDeps) (any, error) {
	return newClientFromDeps(deps), nil
}

// newTypedCRUD builds the TypedCRUD used by NewTypedCRUD below. Client's
// Get/Create/Update/Delete methods are wired directly (they already match
// the TypedCRUD Fn signatures); List is adapted from ListOptions to the
// limit parameter TypedCRUD expects.
func newTypedCRUD(client *Client, cfg internalconfig.NamespacedRESTConfig) *adapter.TypedCRUD[Slo] {
	return &adapter.TypedCRUD[Slo]{
		ListFn: func(ctx context.Context, limit int64) ([]Slo, error) {
			return client.List(ctx, adapter.ListOptions{Limit: limit})
		},
		GetFn:       client.Get,
		CreateFn:    client.Create,
		UpdateFn:    client.Update,
		DeleteFn:    client.Delete,
		Namespace:   cfg.Namespace,
		StripFields: []string{"uuid", "readOnly"},
		Descriptor:  StaticDescriptor(),
	}
}

// NewTypedCRUD creates a TypedCRUD for SLO definitions using the provided
// loader. This is the hand-written commands.go's entry point for the
// `gcx slo definitions` command tree — a separate call site from the
// `gcx resources` pipeline built via SloResource()/adapter.NewProvider in
// provider.go, but both are backed by the same Client capability methods,
// so they return equivalent data.
// Returns both the CRUD instance and the config for additional operations
// like Prometheus queries.
func NewTypedCRUD(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[Slo], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for SLO: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create SLO definitions client: %w", err)
	}

	return newTypedCRUD(client, cfg), cfg, nil
}

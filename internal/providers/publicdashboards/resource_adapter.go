package publicdashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the API version for PublicDashboard resources.
	APIVersion = "publicdashboards.ext.grafana.app/v1alpha1"
	// Kind is the kind for PublicDashboard resources.
	Kind = "PublicDashboard"
)

// staticDescriptor is the resource descriptor for PublicDashboard resources.
//
//nolint:gochecknoglobals // Static descriptor used in self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "publicdashboards.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "PublicDashboard",
	Singular: "publicdashboard",
	Plural:   "publicdashboards",
}

// StaticDescriptor returns the resource descriptor for PublicDashboard.
func StaticDescriptor() resources.Descriptor { return staticDescriptor }

// PublicDashboardSchema returns a JSON Schema for the PublicDashboard resource type.
func PublicDashboardSchema() json.RawMessage {
	return adapter.SchemaFromType[PublicDashboard](StaticDescriptor())
}

// PublicDashboardExample returns an example PublicDashboard manifest as JSON.
func PublicDashboardExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "pd-abc123",
		},
		"spec": map[string]any{
			"uid":                  "pd-abc123",
			"dashboardUid":         "dash-xyz",
			"isEnabled":            true,
			"annotationsEnabled":   false,
			"timeSelectionEnabled": false,
			"share":                "public",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("publicdashboards: failed to marshal example: %v", err))
	}
	return b
}

// RESTConfigLoader can load a NamespacedRESTConfig from the active context.
type RESTConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for PublicDashboards.
// The factory captures the RESTConfigLoader and constructs the client on first invocation.
func NewAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewTypedCRUD creates a TypedCRUD[PublicDashboard] for use in commands and
// the adapter factory. It loads the REST config and constructs the client.
func NewTypedCRUD(ctx context.Context, loader RESTConfigLoader) (*adapter.TypedCRUD[PublicDashboard], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for publicdashboards: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create publicdashboards client: %w", err)
	}

	crud := &adapter.TypedCRUD[PublicDashboard]{
		ListFn: func(ctx context.Context, limit int64) ([]PublicDashboard, error) {
			items, err := client.List(ctx)
			if err != nil {
				return nil, err
			}
			return adapter.TruncateSlice(items, limit), nil
		},

		GetFn: func(ctx context.Context, name string) (*PublicDashboard, error) {
			items, err := client.List(ctx)
			if err != nil {
				return nil, err
			}
			for i := range items {
				if items[i].UID == name {
					pd := items[i]
					return &pd, nil
				}
			}
			return nil, fmt.Errorf("public dashboard %q: %w", name, adapter.ErrNotFound)
		},

		CreateFn: func(ctx context.Context, spec *PublicDashboard) (*PublicDashboard, error) {
			if spec.DashboardUID == "" {
				return nil, errors.New("spec.dashboardUid is required to create a public dashboard")
			}
			return client.Create(ctx, spec.DashboardUID, spec)
		},

		UpdateFn: func(ctx context.Context, name string, spec *PublicDashboard) (*PublicDashboard, error) {
			if spec.DashboardUID == "" {
				return nil, errors.New("spec.dashboardUid is required to update a public dashboard")
			}
			return client.Update(ctx, spec.DashboardUID, name, spec)
		},

		DeleteFn: func(ctx context.Context, name string) error {
			items, err := client.List(ctx)
			if err != nil {
				return err
			}
			for _, pd := range items {
				if pd.UID == name {
					return client.Delete(ctx, pd.DashboardUID, name)
				}
			}
			return fmt.Errorf("public dashboard %q: %w", name, adapter.ErrNotFound)
		},

		// AccessToken is server-generated; never round-trip it back to the server.
		StripFields: []string{"accessToken"},
		Namespace:   cfg.Namespace,
		Descriptor:  staticDescriptor,
	}

	return crud, cfg, nil
}

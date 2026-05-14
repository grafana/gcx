package collections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for AI Observability collections.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "sigil.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "Collection",
		Singular: "collection",
		Plural:   "collections",
	}
}

// CollectionSchema returns a JSON Schema for the Collection resource type.
func CollectionSchema() json.RawMessage {
	return adapter.SchemaFromType[Collection](StaticDescriptor())
}

// stripFields lists server-managed fields that must not appear in the YAML
// representation. These are restored on read by the GET endpoint.
func stripFields() []string {
	return []string{
		"collection_id",
		"tenant_id",
		"created_by",
		"updated_by",
		"created_at",
		"updated_at",
		"member_count",
	}
}

// NewTypedCRUD creates a TypedCRUD for AI Observability collections.
//
// The collections API is *not* an upsert (unlike evaluators), so CreateFn and
// UpdateFn dispatch to different HTTP endpoints (POST vs PATCH).
//
// UpdateFn only sends the description when it is non-empty so that an absent
// description in the pulled YAML — Collection.Description has `omitempty`, so
// it round-trips as zero — does not clear the server-side value. To explicitly
// clear a description, use `gcx aio11y collections update <id> --description ""`.
func NewTypedCRUD(ctx context.Context) (*adapter.TypedCRUD[Collection], string, error) {
	var loader providers.ConfigLoader
	loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load REST config for AI Observability collections: %w", err)
	}

	base, err := aio11yhttp.NewClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create AI Observability HTTP client: %w", err)
	}
	client := NewClient(base)

	crud := &adapter.TypedCRUD[Collection]{
		ListFn: func(ctx context.Context, limit int64) ([]Collection, error) {
			return client.List(ctx, int(limit))
		},
		GetFn: func(ctx context.Context, name string) (*Collection, error) {
			item, err := client.Get(ctx, name)
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("collection %s: %w", name, adapter.ErrNotFound)
			}
			return item, err
		},
		CreateFn: func(ctx context.Context, item *Collection) (*Collection, error) {
			return client.Create(ctx, item)
		},
		UpdateFn: func(ctx context.Context, name string, item *Collection) (*Collection, error) {
			update := &UpdateRequest{Name: &item.Name}
			if item.Description != "" {
				update.Description = &item.Description
			}
			return client.Update(ctx, name, update)
		},
		DeleteFn:    client.Delete,
		Namespace:   cfg.Namespace,
		StripFields: stripFields(),
		Descriptor:  StaticDescriptor(),
	}
	return crud, cfg.Namespace, nil
}

// NewLazyFactory returns an adapter.Factory for AI Observability collections.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewTypedCRUD(ctx)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

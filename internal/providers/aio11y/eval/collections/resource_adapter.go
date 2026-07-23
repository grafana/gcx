package collections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticDescriptor returns the resource descriptor for Agent Observability collections.
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

// NewTypedCRUD creates a TypedCRUD for Agent Observability collections.
//
// The collections API is *not* an upsert (unlike evaluators), so CreateFn and
// UpdateFn dispatch to different HTTP endpoints (POST vs PATCH).
//
// UpdateFn only sends the description when it is non-empty so that an absent
// description in the pulled YAML — Collection.Description has `omitempty`, so
// it round-trips as zero — does not clear the server-side value. To explicitly
// clear a description, use `gcx agento11y collections update <id> --description ""`.
// The loader carries the command's --config selection; adapter factories pass
// a zero-value loader and inherit the selection (config file and context name)
// threaded through ctx by the resources command.
func NewTypedCRUD(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[Collection], string, error) {
	crud, _, err := newCRUDAndClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	return crud, crud.Namespace, nil
}

// newCRUDAndClient builds the collections client and its envelope CRUD from a
// single config load, so callers that need both (the update command routes its
// PATCH through the client but renders via the CRUD) cannot end up with the
// two halves pointing at different stacks.
func newCRUDAndClient(ctx context.Context, loader *providers.ConfigLoader) (*adapter.TypedCRUD[Collection], *Client, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load REST config for Agent Observability collections: %w", err)
	}

	base, err := aio11yhttp.NewClient(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Agent Observability HTTP client: %w", err)
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
	return crud, client, nil
}

// NewLazyFactory returns an adapter.Factory for Agent Observability collections.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

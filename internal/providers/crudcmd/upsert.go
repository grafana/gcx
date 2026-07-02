package crudcmd

import (
	"context"
	"fmt"
)

// UpsertConfig configures the create-or-update-by-probing-404 flow shared by
// push commands that treat local manifests as authoritative: if the item has
// no ID it's always created; otherwise a Get probes existence, and a
// not-found error falls through to Create while any other outcome updates.
type UpsertConfig[T any] struct {
	// HasID reports whether item already carries a server-assigned ID.
	HasID func(item T) bool
	// ID extracts the server-assigned ID (only called when HasID is true).
	ID func(item T) string
	// Name extracts a human-readable name for success/error messages.
	Name func(item T) string

	// Get probes for existence by ID. It must return an error satisfying
	// IsNotFound when the item does not exist.
	Get func(ctx context.Context, id string) error
	// IsNotFound reports whether err from Get indicates a missing resource.
	IsNotFound func(err error) bool

	// Create creates a new item.
	Create func(ctx context.Context, item T) (T, error)
	// Update updates an existing item by ID.
	Update func(ctx context.Context, id string, item T) error

	// OnCreated and OnUpdated report the outcome (e.g. via cmdio.Success).
	OnCreated func(created T)
	OnUpdated func(item T)
}

// Upsert runs the create-or-update flow described by cfg for a single item.
func Upsert[T any](ctx context.Context, item T, cfg UpsertConfig[T]) error {
	if !cfg.HasID(item) {
		created, err := cfg.Create(ctx, item)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", cfg.Name(item), err)
		}
		cfg.OnCreated(created)
		return nil
	}

	id := cfg.ID(item)
	getErr := cfg.Get(ctx, id)
	switch {
	case getErr == nil:
		if err := cfg.Update(ctx, id, item); err != nil {
			return fmt.Errorf("failed to update %s: %w", id, err)
		}
		cfg.OnUpdated(item)
		return nil

	case cfg.IsNotFound(getErr):
		created, err := cfg.Create(ctx, item)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", cfg.Name(item), err)
		}
		cfg.OnCreated(created)
		return nil

	default:
		return fmt.Errorf("failed to check %s: %w", id, getErr)
	}
}

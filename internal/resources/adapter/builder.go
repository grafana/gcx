package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RegistrationMeta carries the descriptor, strip fields, example, and URL
// template for one BuildRegistration call. Schema and GVK are NOT carried
// here — BuildRegistration derives them from the type parameter and the
// Descriptor, so callers must not hand-thread them.
type RegistrationMeta struct {
	Descriptor  resources.Descriptor
	StripFields []string
	Example     json.RawMessage
	URLTemplate string
}

// NewRegistrationMeta builds a RegistrationMeta whose Descriptor is assembled
// from gv/kind/singular/plural. Providers with their own GroupVersion
// constants typically wrap this in a small package-local helper (see
// irm.oncallMeta for an example).
func NewRegistrationMeta(gv schema.GroupVersion, kind, singular, plural string) RegistrationMeta {
	return RegistrationMeta{
		Descriptor: resources.Descriptor{
			GroupVersion: gv,
			Kind:         kind,
			Singular:     singular,
			Plural:       plural,
		},
	}
}

// CRUDOption configures an optional CRUD operation on the TypedCRUD[T] built
// by BuildRegistration, given the resolved client of type C.
type CRUDOption[T ResourceNamer, C any] func(client C, crud *TypedCRUD[T])

// WithCreate configures BuildRegistration's TypedCRUD.CreateFn using fn,
// which is invoked with the resolved client.
func WithCreate[T ResourceNamer, C any](fn func(ctx context.Context, client C, item *T) (*T, error)) CRUDOption[T, C] {
	return func(client C, crud *TypedCRUD[T]) {
		crud.CreateFn = func(ctx context.Context, item *T) (*T, error) {
			return fn(ctx, client, item)
		}
	}
}

// WithUpdate configures BuildRegistration's TypedCRUD.UpdateFn using fn,
// which is invoked with the resolved client.
func WithUpdate[T ResourceNamer, C any](fn func(ctx context.Context, client C, name string, item *T) (*T, error)) CRUDOption[T, C] {
	return func(client C, crud *TypedCRUD[T]) {
		crud.UpdateFn = func(ctx context.Context, name string, item *T) (*T, error) {
			return fn(ctx, client, name, item)
		}
	}
}

// WithDelete configures BuildRegistration's TypedCRUD.DeleteFn using fn,
// which is invoked with the resolved client.
func WithDelete[T ResourceNamer, C any](fn func(ctx context.Context, client C, name string) error) CRUDOption[T, C] {
	return func(client C, crud *TypedCRUD[T]) {
		crud.DeleteFn = func(ctx context.Context, name string) error {
			return fn(ctx, client, name)
		}
	}
}

// BuildRegistration assembles a Registration from a client loader, resource
// metadata, and List/Get functions, wiring optional Create/Update/Delete via
// CRUDOption. Schema and GVK are auto-derived from T and the Descriptor —
// callers MUST NOT hand-thread them.
//
// loadClient loads (or connects) the provider's client of type C along with
// the namespace to stamp on every produced envelope. It is invoked lazily,
// once per Factory call, mirroring the existing TypedCRUD/Registration
// lifecycle.
func BuildRegistration[T ResourceNamer, C any](
	loadClient func(ctx context.Context) (C, string, error),
	meta RegistrationMeta,
	listFn func(ctx context.Context, client C) ([]T, error),
	getFn func(ctx context.Context, client C, name string) (*T, error),
	opts ...CRUDOption[T, C],
) Registration {
	desc := meta.Descriptor
	return Registration{
		Factory: func(ctx context.Context) (ResourceAdapter, error) {
			client, namespace, err := loadClient(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to load %s adapter: %w", desc.Kind, err)
			}

			crud := &TypedCRUD[T]{
				StripFields: meta.StripFields,
				Namespace:   namespace,
				Descriptor:  desc,
			}

			if listFn != nil {
				crud.ListFn = LimitedListFn(func(ctx context.Context) ([]T, error) { return listFn(ctx, client) })
			}

			if getFn != nil {
				crud.GetFn = func(ctx context.Context, name string) (*T, error) { return getFn(ctx, client, name) }
			} else {
				crud.GetFn = func(_ context.Context, _ string) (*T, error) { return nil, errors.ErrUnsupported }
			}

			for _, opt := range opts {
				opt(client, crud)
			}

			return crud.AsAdapter(), nil
		},
		Descriptor:  desc,
		GVK:         desc.GroupVersionKind(),
		Schema:      SchemaFromType[T](desc),
		Example:     meta.Example,
		URLTemplate: meta.URLTemplate,
	}
}

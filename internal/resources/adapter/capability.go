package adapter

import (
	"context"
	"encoding/json"

	"github.com/grafana/gcx/internal/resources"
)

// Lister is implemented by a Resource[T]'s client when it supports listing.
// Unimplemented ⇒ TypedCRUD.ListFn stays nil ⇒ List resolves to
// errors.ErrUnsupported, mirroring every other capability interface below.
type Lister[T any] interface {
	List(ctx context.Context, opts ListOptions) ([]T, error)
}

// Getter is implemented by a Resource[T]'s client when it supports get-by-name.
type Getter[T any] interface {
	Get(ctx context.Context, name string) (*T, error)
}

// Creator is implemented by a Resource[T]'s client when it supports create.
type Creator[T any] interface {
	Create(ctx context.Context, item *T) (*T, error)
}

// Updater is implemented by a Resource[T]'s client when it supports update-by-name.
type Updater[T any] interface {
	Update(ctx context.Context, name string, item *T) (*T, error)
}

// Deleter is implemented by a Resource[T]'s client when it supports delete-by-name.
type Deleter[T any] interface {
	Delete(ctx context.Context, name string) error
}

// Validator is implemented by a Resource[T]'s client when it supports
// server-side validation. When present, the adapter calls Validate instead
// of Create/Update for `--dry-run` (and `resources validate`); when absent,
// dry-run mutations skip the server call and return ErrDryRunUnverified so
// the resource is reported as skipped, not falsely valid — see
// typedAdapter.dryRunValidate in typed.go.
type Validator[T any] interface {
	Validate(ctx context.Context, items []*T) error
}

// ListOptions carries list-verb knobs for Lister[T].List, mirroring
// TypedCRUD.ListFn's limit parameter and metav1.ListOptions.Limit. Room for
// label/field selectors as the declarative model grows.
type ListOptions struct {
	// Limit caps the number of items returned. Zero means no limit.
	Limit int64
}

// capabilityMeta carries the static per-Resource metadata newCapabilityCRUD
// needs in addition to the capability-asserted client itself.
type capabilityMeta struct {
	Descriptor  resources.Descriptor
	Namespace   string
	StripFields []string
	Example     json.RawMessage
}

// newCapabilityCRUD is THE single audited seam: it type-asserts a
// Resource[T].NewClient result (an `any`) against the capability interfaces
// above and wires the corresponding TypedCRUD[T] Fn fields. This is the only
// place in the codebase that performs a capability-interface assertion — see
// docs/architecture/patterns.md §16 "Sanctioned Exception" for the ruling
// that permits it. An unimplemented capability leaves its Fn nil, which
// TypedCRUD already resolves to errors.ErrUnsupported (or the unverified
// dry-run path for Validator) — no provider ever performs this assertion itself.
func newCapabilityCRUD[T ResourceNamer](client any, meta capabilityMeta) *TypedCRUD[T] {
	crud := &TypedCRUD[T]{
		Descriptor:  meta.Descriptor,
		Namespace:   meta.Namespace,
		StripFields: meta.StripFields,
		Example:     meta.Example,
	}

	if lister, ok := client.(Lister[T]); ok {
		crud.ListFn = func(ctx context.Context, limit int64) ([]T, error) {
			return lister.List(ctx, ListOptions{Limit: limit})
		}
	}
	if getter, ok := client.(Getter[T]); ok {
		crud.GetFn = getter.Get
	}
	if creator, ok := client.(Creator[T]); ok {
		crud.CreateFn = creator.Create
	}
	if updater, ok := client.(Updater[T]); ok {
		crud.UpdateFn = updater.Update
	}
	if deleter, ok := client.(Deleter[T]); ok {
		crud.DeleteFn = deleter.Delete
	}
	if validator, ok := client.(Validator[T]); ok {
		crud.ValidateFn = validator.Validate
	}

	return crud
}

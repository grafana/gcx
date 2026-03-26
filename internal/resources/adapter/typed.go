package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/grafanactl/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TypedObject wraps a domain type T with Kubernetes metadata, producing the
// standard {apiVersion, kind, metadata, spec} envelope when serialized to JSON.
type TypedObject[T ResourceNamer] struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              T `json:"spec"`
}

// TypedCRUD absorbs the boilerplate that every ResourceAdapter implementation
// repeats: marshal T to/from a Kubernetes-style unstructured envelope, strip
// server-managed fields, and dispatch to typed functions.
type TypedCRUD[T ResourceNamer] struct {
	// ListFn lists all items of this type (REQUIRED).
	ListFn func(ctx context.Context) ([]T, error)

	// GetFn returns a single item by name (REQUIRED).
	GetFn func(ctx context.Context, name string) (*T, error)

	// CreateFn creates a new item. Nil means create is unsupported.
	CreateFn func(ctx context.Context, item *T) (*T, error)

	// UpdateFn updates an existing item by name. Nil means update is unsupported.
	UpdateFn func(ctx context.Context, name string, item *T) (*T, error)

	// DeleteFn deletes an item by name. Nil means delete is unsupported.
	DeleteFn func(ctx context.Context, name string) error

	// Namespace is set on every produced envelope's metadata.namespace.
	Namespace string

	// StripFields lists spec-level keys to remove (e.g., "uuid", "id", "readOnly").
	StripFields []string

	// MetadataFn returns extra metadata fields to merge into the envelope.
	// It must never return "name" or "namespace" — those are always set by TypedCRUD.
	MetadataFn func(T) map[string]any

	// Descriptor is the resource descriptor for this type.
	Descriptor resources.Descriptor

	// Aliases are the short names for selector resolution.
	Aliases []string
}

// resourceName extracts the name from a domain object using ResourceIdentity.
func (c *TypedCRUD[T]) resourceName(item T) string {
	return item.GetResourceName()
}

// restoreName restores the identity field on a domain object using ResourceIdentity.SetResourceName
// via type assertion on the pointer (since SetResourceName has pointer receiver).
func (c *TypedCRUD[T]) restoreName(name string, item *T) {
	if setter, ok := any(item).(interface{ SetResourceName(string) }); ok {
		setter.SetResourceName(name)
	}
}

// --- Typed public methods ---

// List returns all items as TypedObject[T] with correct TypeMeta and ObjectMeta.
func (c *TypedCRUD[T]) List(ctx context.Context) ([]TypedObject[T], error) {
	items, err := c.ListFn(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]TypedObject[T], 0, len(items))
	for _, item := range items {
		result = append(result, c.wrapTypedObject(item))
	}
	return result, nil
}

// Get returns a single item by name as a TypedObject[T].
func (c *TypedCRUD[T]) Get(ctx context.Context, name string) (*TypedObject[T], error) {
	item, err := c.GetFn(ctx, name)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("resource %q not found", name)
	}
	obj := c.wrapTypedObject(*item)
	return &obj, nil
}

// Create creates a new item via CreateFn and returns the result as TypedObject[T].
func (c *TypedCRUD[T]) Create(ctx context.Context, obj *TypedObject[T]) (*TypedObject[T], error) {
	if c.CreateFn == nil {
		return nil, errors.ErrUnsupported
	}

	created, err := c.CreateFn(ctx, &obj.Spec)
	if err != nil {
		return nil, err
	}

	result := c.wrapTypedObject(*created)
	return &result, nil
}

// Update updates an existing item by name and returns the result as TypedObject[T].
func (c *TypedCRUD[T]) Update(ctx context.Context, name string, obj *TypedObject[T]) (*TypedObject[T], error) {
	if c.UpdateFn == nil {
		return nil, errors.ErrUnsupported
	}

	updated, err := c.UpdateFn(ctx, name, &obj.Spec)
	if err != nil {
		return nil, err
	}

	result := c.wrapTypedObject(*updated)
	return &result, nil
}

// Delete removes an item by name.
func (c *TypedCRUD[T]) Delete(ctx context.Context, name string) error {
	if c.DeleteFn == nil {
		return errors.ErrUnsupported
	}
	return c.DeleteFn(ctx, name)
}

// wrapTypedObject wraps a domain object T into a TypedObject with correct metadata.
func (c *TypedCRUD[T]) wrapTypedObject(item T) TypedObject[T] {
	return TypedObject[T]{
		TypeMeta: metav1.TypeMeta{
			APIVersion: c.Descriptor.GroupVersion.String(),
			Kind:       c.Descriptor.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.resourceName(item),
			Namespace: c.Namespace,
		},
		Spec: item,
	}
}

// AsAdapter returns a ResourceAdapter backed by this TypedCRUD.
// Note: the returned adapter's Schema() and Example() return nil.
// Schema/example are static registration metadata injected only via
// TypedRegistration.ToRegistration(). Use SchemaForGVK/ExampleForGVK
// for authoritative lookup.
func (c *TypedCRUD[T]) AsAdapter() ResourceAdapter {
	return &typedAdapter[T]{crud: c}
}

// toUnstructured converts a domain object T into an unstructured Kubernetes envelope.
func (c *TypedCRUD[T]) toUnstructured(item T) (unstructured.Unstructured, error) {
	// T -> JSON -> map[string]any (this becomes the spec)
	data, err := json.Marshal(item)
	if err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to marshal item: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return unstructured.Unstructured{}, fmt.Errorf("failed to unmarshal item to map: %w", err)
	}

	// Strip server-managed fields from the spec.
	for _, field := range c.StripFields {
		delete(specMap, field)
	}

	// Build the metadata map.
	metadata := map[string]any{
		"name":      c.resourceName(item),
		"namespace": c.Namespace,
	}

	// Merge extra metadata if provided, but never overwrite name or namespace.
	if c.MetadataFn != nil {
		for k, v := range c.MetadataFn(item) {
			if k == "name" || k == "namespace" {
				continue
			}
			metadata[k] = v
		}
	}

	// Build the Kubernetes-style object envelope.
	obj := map[string]any{
		"apiVersion": c.Descriptor.GroupVersion.String(),
		"kind":       c.Descriptor.Kind,
		"metadata":   metadata,
		"spec":       specMap,
	}

	res := resources.MustFromObject(obj, resources.SourceInfo{})
	return res.ToUnstructured(), nil
}

// fromUnstructured extracts name and *T from an unstructured Kubernetes envelope.
func (c *TypedCRUD[T]) fromUnstructured(obj *unstructured.Unstructured) (string, *T, error) {
	specRaw, ok := obj.Object["spec"]
	if !ok {
		return "", nil, errors.New("object has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return "", nil, errors.New("object spec is not a map")
	}

	data, err := json.Marshal(specMap)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var item T
	if err := json.Unmarshal(data, &item); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal spec into typed object: %w", err)
	}

	name := obj.GetName()
	c.restoreName(name, &item)

	return name, &item, nil
}

// typedAdapter wraps TypedCRUD[T] to implement the ResourceAdapter interface.
type typedAdapter[T ResourceNamer] struct {
	crud    *TypedCRUD[T]
	schema  json.RawMessage
	example json.RawMessage
}

func (a *typedAdapter[T]) Descriptor() resources.Descriptor {
	return a.crud.Descriptor
}

func (a *typedAdapter[T]) Aliases() []string {
	return a.crud.Aliases
}

func (a *typedAdapter[T]) Schema() json.RawMessage {
	return a.schema
}

func (a *typedAdapter[T]) Example() json.RawMessage {
	return a.example
}

func (a *typedAdapter[T]) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	items, err := a.crud.ListFn(ctx)
	if err != nil {
		return nil, err
	}

	result := &unstructured.UnstructuredList{}
	for _, item := range items {
		u, err := a.crud.toUnstructured(item)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, u)
	}

	return result, nil
}

func (a *typedAdapter[T]) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	item, err := a.crud.GetFn(ctx, name)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("resource %q not found", name)
	}

	u, err := a.crud.toUnstructured(*item)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func (a *typedAdapter[T]) Create(ctx context.Context, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	if a.crud.CreateFn == nil {
		return nil, errors.ErrUnsupported
	}

	_, item, err := a.crud.fromUnstructured(obj)
	if err != nil {
		return nil, err
	}

	created, err := a.crud.CreateFn(ctx, item)
	if err != nil {
		return nil, err
	}

	u, err := a.crud.toUnstructured(*created)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func (a *typedAdapter[T]) Update(ctx context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	if a.crud.UpdateFn == nil {
		return nil, errors.ErrUnsupported
	}

	name, item, err := a.crud.fromUnstructured(obj)
	if err != nil {
		return nil, err
	}

	updated, err := a.crud.UpdateFn(ctx, name, item)
	if err != nil {
		return nil, err
	}

	u, err := a.crud.toUnstructured(*updated)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

func (a *typedAdapter[T]) Delete(ctx context.Context, name string, _ metav1.DeleteOptions) error {
	if a.crud.DeleteFn == nil {
		return errors.ErrUnsupported
	}

	return a.crud.DeleteFn(ctx, name)
}

// TypedRegistration bridges TypedCRUD to the existing Registration system.
type TypedRegistration[T ResourceNamer] struct {
	Descriptor resources.Descriptor
	Aliases    []string
	GVK        schema.GroupVersionKind
	Schema     json.RawMessage
	Example    json.RawMessage
	Factory    func(ctx context.Context) (*TypedCRUD[T], error)
}

// ToRegistration converts to a standard Registration.
func (r TypedRegistration[T]) ToRegistration() Registration {
	return Registration{
		Factory: func(ctx context.Context) (ResourceAdapter, error) {
			crud, err := r.Factory(ctx)
			if err != nil {
				return nil, err
			}
			a := &typedAdapter[T]{
				crud:    crud,
				schema:  r.Schema,
				example: r.Example,
			}
			return a, nil
		},
		Descriptor: r.Descriptor,
		Aliases:    r.Aliases,
		GVK:        r.GVK,
		Schema:     r.Schema,
		Example:    r.Example,
	}
}

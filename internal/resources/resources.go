package resources

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/format"
	folderv1beta1 "github.com/grafana/grafana/apps/folder/pkg/apis/folder/v1beta1"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// ResourceManagerKind is the manager kind used for gcx.
	// TODO: change once we have a proper manager kind for gcx.
	ResourceManagerKind = utils.ManagerKindKubectl
	// AnnotationSavedFromUI is the annotation key for resources saved from the UI.
	// TODO: move this to grafana/grafana.
	AnnotationSavedFromUI = "grafana.app/saved-from-ui"
)

// ResourceRef is a unique identifier for a resource.
type ResourceRef string

// Resource is a resource in the Grafana API.
type Resource struct {
	Raw    utils.GrafanaMetaAccessor
	Object unstructured.Unstructured
	Source SourceInfo
}

// MustFromObject creates a new Resource from an object.
// If the object is not a valid Grafana resource, it will panic.
func MustFromObject(obj map[string]any, source SourceInfo) *Resource {
	res := MustFromUnstructured(&unstructured.Unstructured{Object: obj})
	res.SetSource(source)
	return res
}

// MustFromUnstructured creates a new Resource from an unstructured object.
// If the object is not a valid Grafana resource, it will panic.
func MustFromUnstructured(obj *unstructured.Unstructured) *Resource {
	r, err := FromUnstructured(obj)
	if err != nil {
		panic(err)
	}
	return r
}

// FromUnstructured creates a new Resource from an unstructured object.
func FromUnstructured(obj *unstructured.Unstructured) (*Resource, error) {
	meta, err := utils.MetaAccessor(obj)
	if err != nil {
		return nil, err
	}

	return &Resource{
		Raw:    meta,
		Object: *obj,
	}, nil
}

// IsEmpty returns true if the resource is empty.
func (r *Resource) IsEmpty() bool {
	return r.Raw == nil
}

// SetUnstructured sets the resource to the given unstructured object.
func (r *Resource) SetUnstructured(obj *unstructured.Unstructured) error {
	metaAccessor, err := utils.MetaAccessor(obj)
	if err != nil {
		return err
	}

	r.Raw = metaAccessor
	r.Object = *obj

	return nil
}

// ToUnstructured converts the resource to an unstructured object.
func (r *Resource) ToUnstructured() unstructured.Unstructured {
	return r.Object
}

// Ref returns a unique identifier for the resource.
func (r *Resource) Ref() ResourceRef {
	return ResourceRef(
		fmt.Sprintf("%s/%s-%s", r.GroupVersionKind().String(), r.Namespace(), r.Name()),
	)
}

// GroupVersionKind returns the GroupVersionKind of the resource.
func (r *Resource) GroupVersionKind() schema.GroupVersionKind {
	return r.Raw.GetGroupVersionKind()
}

// Namespace returns the namespace of the resource.
func (r *Resource) Namespace() string {
	return r.Raw.GetNamespace()
}

// Name returns the name of the resource.
func (r *Resource) Name() string {
	return r.Raw.GetName()
}

// Labels returns the labels of the resource.
func (r *Resource) Labels() map[string]string {
	return r.Raw.GetLabels()
}

// Annotations returns the annotations of the resource.
func (r *Resource) Annotations() map[string]string {
	return r.Raw.GetAnnotations()
}

// Spec returns the spec of the resource.
func (r *Resource) Spec() (any, error) {
	return r.Raw.GetSpec()
}

// Group returns the group of the resource.
func (r *Resource) Group() string {
	return r.Raw.GetGroupVersionKind().Group
}

// Kind returns the kind of the resource.
func (r *Resource) Kind() string {
	return r.Raw.GetGroupVersionKind().Kind
}

// Version returns the version of the resource.
func (r *Resource) Version() string {
	return r.Raw.GetGroupVersionKind().Version
}

// APIVersion returns the API version of the resource.
func (r *Resource) APIVersion() string {
	return r.Group() + "/" + r.Version()
}

// SetSource sets the source of the resource.
func (r *Resource) SetSource(source SourceInfo) {
	r.Source = source
}

// SourcePath returns the source path of the resource.
func (r *Resource) SourcePath() string {
	return r.Source.Path
}

// SourceFormat returns the source format of the resource.
func (r *Resource) SourceFormat() format.Format {
	return r.Source.Format
}

// IsManaged returns true if the resource is managed by gcx.
func (r *Resource) IsManaged() bool {
	return r.GetManagerKind() == ResourceManagerKind
}

// GetManagerKind returns the kind of the manager that manages the resource.
func (r *Resource) GetManagerKind() utils.ManagerKind {
	m, ok := r.Raw.GetManagerProperties()
	if !ok {
		// If the manager properties are not set,
		// we assume the resource will be managed by gcx.
		return ResourceManagerKind
	}

	return m.Kind
}

// IsFolder returns true if the resource is a folder.
func (r *Resource) IsFolder() bool {
	return r.GroupVersionKind().Group == folderv1beta1.FolderKind().Group() &&
		r.GroupVersionKind().Kind == folderv1beta1.FolderKind().Kind()
}

// GetFolder returns the parent folder UID from the annotation.
// Returns empty string if the folder has no parent (is a root folder).
func (r *Resource) GetFolder() string {
	return r.Raw.GetFolder()
}

// Resources is a collection of resources.
type Resources struct {
	collection    map[ResourceRef]*Resource
	onChangeFuncs []func(resource *Resource)
}

// NewResources creates a new Resources collection.
func NewResources(resources ...*Resource) *Resources {
	r := MakeResources(len(resources))
	r.Add(resources...)
	return r
}

// MakeResources makes a new empty Resources collection of the given size.
func MakeResources(size int) *Resources {
	return &Resources{
		collection: make(map[ResourceRef]*Resource, size),
	}
}

// NewResourcesFromUnstructured creates a new Resources collection from an unstructured list.
func NewResourcesFromUnstructured(resources unstructured.UnstructuredList) (*Resources, error) {
	if len(resources.Items) == 0 {
		return NewResources(), nil
	}

	list := make([]*Resource, 0, len(resources.Items))
	for i := range resources.Items {
		r, err := FromUnstructured(&resources.Items[i])
		if err != nil {
			return nil, err
		}

		list = append(list, r)
	}

	return NewResources(list...), nil
}

// Clear removes all resources from the collection by resetting the underlying map.
// The new map will have the same capacity as the old one.
func (r *Resources) Clear() {
	r.collection = make(map[ResourceRef]*Resource, len(r.collection))
}

// Add adds resources to the collection.
func (r *Resources) Add(resources ...*Resource) {
	for _, resource := range resources {
		r.collection[resource.Ref()] = resource

		for _, cb := range r.onChangeFuncs {
			cb(resource)
		}
	}
}

// OnChange adds a callback that will be called when a resource is added to the collection.
func (r *Resources) OnChange(callback func(resource *Resource)) {
	r.onChangeFuncs = append(r.onChangeFuncs, callback)
}

// Find finds a resource by kind and name.
// TODO: kind + name isn't enough to unambiguously identify a resource.
func (r *Resources) Find(kind string, name string) (*Resource, bool) {
	for _, resource := range r.collection {
		if resource.Kind() == kind && resource.Name() == name {
			return resource, true
		}
	}

	return nil, false
}

// Merge merges another resources collection into the current one.
func (r *Resources) Merge(resources *Resources) {
	_ = resources.ForEach(func(resource *Resource) error {
		r.Add(resource)
		return nil
	})
}

// ForEach iterates over all resources in the collection and calls the callback for each resource.
func (r *Resources) ForEach(callback func(*Resource) error) error {
	for _, resource := range r.collection {
		if err := callback(resource); err != nil {
			return err
		}
	}

	return nil
}

// ForEachConcurrently iterates over all resources in the collection and calls the callback for each resource.
// The callback is called concurrently, up to maxInflight at a time.
func (r *Resources) ForEachConcurrently(
	ctx context.Context, maxInflight int, callback func(context.Context, *Resource) error,
) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxInflight)

	for _, resource := range r.collection {
		g.Go(func() error {
			return callback(ctx, resource)
		})
	}

	return g.Wait()
}

// Len returns the number of resources in the collection.
func (r *Resources) Len() int {
	return len(r.collection)
}

// AsList returns a list of resources from the collection.
func (r *Resources) AsList() []*Resource {
	if r.collection == nil {
		return nil
	}

	list := make([]*Resource, 0, r.Len())
	for _, resource := range r.collection {
		list = append(list, resource)
	}

	return list
}

// GroupByKind groups resources by kind.
func (r *Resources) GroupByKind() map[string]*Resources {
	resourceByKind := map[string]*Resources{}
	_ = r.ForEach(func(resource *Resource) error {
		if _, ok := resourceByKind[resource.Kind()]; !ok {
			resourceByKind[resource.Kind()] = NewResources()
		}

		resourceByKind[resource.Kind()].Add(resource)
		return nil
	})

	return resourceByKind
}

// ToUnstructuredList converts the resources to an unstructured list.
func (r *Resources) ToUnstructuredList() unstructured.UnstructuredList {
	res := unstructured.UnstructuredList{
		Items: make([]unstructured.Unstructured, 0, r.Len()),
	}

	if err := r.ForEach(func(r *Resource) error {
		res.Items = append(res.Items, r.ToUnstructured())
		return nil
	}); err != nil {
		return unstructured.UnstructuredList{}
	}

	return res
}

// SortUnstructured sorts a list of unstructured objects by group, version, kind, and name.
func SortUnstructured(items []unstructured.Unstructured) {
	slices.SortStableFunc(items, func(a, b unstructured.Unstructured) int {
		gva := a.GroupVersionKind()
		gvb := b.GroupVersionKind()

		res := strings.Compare(gva.Group, gvb.Group)
		if res != 0 {
			return res
		}

		res = strings.Compare(gva.Version, gvb.Version)
		if res != 0 {
			return res
		}

		res = strings.Compare(gva.Kind, gvb.Kind)
		if res != 0 {
			return res
		}

		return strings.Compare(a.GetName(), b.GetName())
	})
}

// SourceInfo is information about the source of a resource.
type SourceInfo struct {
	Path   string
	Format format.Format
}

func (s *SourceInfo) String() string {
	return "file://" + s.Path
}

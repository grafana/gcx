package adapter

import (
	"context"
	"fmt"
	"sync"

	"github.com/grafana/gcx/internal/resources"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DynamicClient is the subset of the k8s dynamic client methods needed by the router
// as a fallback for non-provider-backed resource types.
type DynamicClient interface {
	Create(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error)
	Update(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error)
	Get(ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error)
	GetMultiple(ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions) ([]unstructured.Unstructured, error)
	List(ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
	Delete(ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions) error
}

// ResourceClientRouter implements remote.PushClient, remote.PullClient, and remote.DeleteClient.
// For each operation it checks whether the descriptor's GVK maps to a registered adapter;
// if so, it delegates to the adapter. Otherwise, it delegates to the dynamic client.
//
// Adapter instances are lazily initialized: factories are only invoked on first access
// for a given GVK. Each GVK has its own sync.Once so adapters for unrelated GVKs initialize
// concurrently without contention. A RWMutex guards the shared result maps, which can be
// written concurrently when different GVKs initialize at the same time.
type ResourceClientRouter struct {
	dynamic   DynamicClient
	factories map[schema.GroupVersionKind]Factory

	once      map[schema.GroupVersionKind]*sync.Once
	mu        sync.RWMutex
	instances map[schema.GroupVersionKind]ResourceAdapter
	initErrs  map[schema.GroupVersionKind]error
}

// NewResourceClientRouter creates a new ResourceClientRouter.
// factories maps GVKs to their adapter factories. Factories are only invoked on first use.
func NewResourceClientRouter(dynamic DynamicClient, factories map[schema.GroupVersionKind]Factory) *ResourceClientRouter {
	once := make(map[schema.GroupVersionKind]*sync.Once, len(factories))
	for gvk := range factories {
		once[gvk] = &sync.Once{}
	}
	return &ResourceClientRouter{
		dynamic:   dynamic,
		factories: factories,
		once:      once,
		instances: make(map[schema.GroupVersionKind]ResourceAdapter, len(factories)),
		initErrs:  make(map[schema.GroupVersionKind]error, len(factories)),
	}
}

// getAdapter returns the adapter for the given GVK, lazily initializing it if needed.
// Returns nil, nil if no adapter is registered for the GVK.
func (r *ResourceClientRouter) getAdapter(ctx context.Context, gvk schema.GroupVersionKind) (ResourceAdapter, error) {
	o, ok := r.once[gvk]
	if !ok {
		return nil, nil //nolint:nilnil // nil adapter signals "no adapter registered"; callers check for nil.
	}

	o.Do(func() {
		inst, err := r.factories[gvk](ctx)
		r.mu.Lock()
		if err != nil {
			r.initErrs[gvk] = err
		} else {
			r.instances[gvk] = inst
		}
		r.mu.Unlock()
	})

	r.mu.RLock()
	err := r.initErrs[gvk]
	inst := r.instances[gvk]
	r.mu.RUnlock()

	if err != nil {
		return nil, fmt.Errorf("initializing adapter for %s: %w", gvk, err)
	}
	return inst, nil
}

// Create implements remote.PushClient.
func (r *ResourceClientRouter) Create(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	a, err := r.getAdapter(ctx, desc.GroupVersionKind())
	if err != nil {
		return nil, err
	}
	if a != nil {
		return a.Create(ctx, obj, opts)
	}
	return r.dynamic.Create(ctx, desc, obj, opts)
}

// Update implements remote.PushClient.
func (r *ResourceClientRouter) Update(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	a, err := r.getAdapter(ctx, desc.GroupVersionKind())
	if err != nil {
		return nil, err
	}
	if a != nil {
		return a.Update(ctx, obj, opts)
	}
	return r.dynamic.Update(ctx, desc, obj, opts)
}

// Get implements remote.PushClient and remote.PullClient.
func (r *ResourceClientRouter) Get(
	ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	a, err := r.getAdapter(ctx, desc.GroupVersionKind())
	if err != nil {
		return nil, err
	}
	if a != nil {
		return a.Get(ctx, name, opts)
	}
	return r.dynamic.Get(ctx, desc, name, opts)
}

// GetMultiple implements remote.PullClient.
// Adapters don't have a GetMultiple method; we implement it by calling Get concurrently
// via errgroup with bounded parallelism (limit 10).
func (r *ResourceClientRouter) GetMultiple(
	ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions,
) ([]unstructured.Unstructured, error) {
	a, err := r.getAdapter(ctx, desc.GroupVersionKind())
	if err != nil {
		return nil, err
	}
	if a == nil {
		return r.dynamic.GetMultiple(ctx, desc, names, opts)
	}

	res := make([]unstructured.Unstructured, len(names))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for i, name := range names {
		g.Go(func() error {
			obj, err := a.Get(ctx, name, opts)
			if err != nil {
				return err
			}
			res[i] = *obj
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return res, nil
}

// List implements remote.PullClient.
func (r *ResourceClientRouter) List(
	ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	a, err := r.getAdapter(ctx, desc.GroupVersionKind())
	if err != nil {
		return nil, err
	}
	if a != nil {
		return a.List(ctx, opts)
	}
	return r.dynamic.List(ctx, desc, opts)
}

// Delete implements remote.DeleteClient.
func (r *ResourceClientRouter) Delete(
	ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions,
) error {
	a, err := r.getAdapter(ctx, desc.GroupVersionKind())
	if err != nil {
		return err
	}
	if a != nil {
		return a.Delete(ctx, name, opts)
	}
	return r.dynamic.Delete(ctx, desc, name, opts)
}

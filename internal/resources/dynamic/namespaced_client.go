package dynamic

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/pager"
)

// NamespacedClient is a dynamic client with a namespace and a discovery registry.
type NamespacedClient struct {
	namespace string
	client    dynamic.Interface
}

// NewDefaultNamespacedClient creates a new namespaced dynamic client using the default discovery registry.
func NewDefaultNamespacedClient(cfg config.NamespacedRESTConfig) (*NamespacedClient, error) {
	client, err := dynamic.NewForConfig(&cfg.Config)
	if err != nil {
		return nil, err
	}

	return NewNamespacedClient(cfg.Namespace, client), nil
}

// NewNamespacedClient creates a new namespaced dynamic client.
func NewNamespacedClient(namespace string, client dynamic.Interface) *NamespacedClient {
	return &NamespacedClient{
		client:    client,
		namespace: namespace,
	}
}

// List lists resources from the server.
//
// When opts.Limit > 0, a single direct API call is made and the response
// (including its Continue token) is returned as-is. This enables callers
// to detect truncated results and display pagination hints.
//
// When opts.Limit == 0, the client-go pager fetches all pages transparently.
func (c *NamespacedClient) List(
	ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	if opts.Limit > 0 {
		res, err := c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).List(ctx, opts)
		return res, ParseStatusError(err)
	}

	pager := pager.New(func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
		return c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).List(ctx, opts)
	})

	res := unstructured.UnstructuredList{
		Items: make([]unstructured.Unstructured, 0),
	}
	res.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   desc.GroupVersion.Group,
		Version: desc.GroupVersion.Version,
		Kind:    desc.Kind + "List",
	})

	if err := pager.EachListItemWithAlloc(ctx, opts, func(obj runtime.Object) error {
		item, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("expected *unstructured.Unstructured, got %T", obj)
		}

		res.Items = append(res.Items, *item)

		return nil
	}); err != nil {
		return nil, ParseStatusError(err)
	}

	return &res, nil
}

// GetMultiple gets multiple resources from the server.
//
// Kubernetes does not support getting multiple resources by name,
// so instead we list all resources and filter on the client side.
//
// Ideally we'd like to use field selectors,
// but Kubernetes does not support set-based operators in field selectors (only in labels).
func (c *NamespacedClient) GetMultiple(
	ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions,
) ([]unstructured.Unstructured, error) {
	g, ctx := errgroup.WithContext(ctx)

	// TODO: consider using a limit
	// g.SetLimit(maxConcurrentGetRequests)

	res := make([]unstructured.Unstructured, len(names))

	for i, it := range names {
		g.Go(func() error {
			item, err := c.Get(ctx, desc, it, opts)
			if err != nil {
				return err
			}

			// NB: it's important to set via the index,
			// because `append`ing would create a race condition.
			res[i] = *item

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return res, nil
}

// Get gets a resource from the server.
func (c *NamespacedClient) Get(
	ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	res, err := c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).Get(ctx, name, opts)
	return res, ParseStatusError(err)
}

// Create creates a resource on the server.
func (c *NamespacedClient) Create(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	res, err := c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).Create(ctx, obj, opts)
	return res, ParseStatusError(err)
}

// Update updates a resource on the server.
func (c *NamespacedClient) Update(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	res, err := c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).Update(ctx, obj, opts)
	return res, ParseStatusError(err)
}

// Delete deletes a resource on the server.
func (c *NamespacedClient) Delete(
	ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions,
) error {
	err := c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).Delete(ctx, name, opts)
	return ParseStatusError(err)
}

// Apply applies a resource on the server.
func (c *NamespacedClient) Apply(
	ctx context.Context, desc resources.Descriptor, name string, obj *unstructured.Unstructured, opts metav1.ApplyOptions,
) (*unstructured.Unstructured, error) {
	res, err := c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).Apply(ctx, name, obj, opts)
	return res, ParseStatusError(err)
}

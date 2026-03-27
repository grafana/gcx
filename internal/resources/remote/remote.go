package remote

import (
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Processor can be used to modify a resource in-place,
// before it is written or after it is read from local sources.
//
// They can be used to e.g. strip server-side fields from a resource,
// or add extra metadata after a resource has been read from a file.
type Processor interface {
	Process(res *resources.Resource) error
}

// adapterRegistry is the subset of discovery.Registry used to build the router.
type adapterRegistry interface {
	GetAdapter(gvk schema.GroupVersionKind) (adapter.Factory, bool)
}

// buildRouter constructs a ResourceClientRouter from a dynamic client and a registry.
// It iterates all globally-registered adapter registrations and, for each GVK that has
// a corresponding factory in the registry, adds it to the router's factory map.
// GVKs without a registered adapter fall through to the dynamic client.
func buildRouter(dynamicClient adapter.DynamicClient, reg adapterRegistry) *adapter.ResourceClientRouter {
	factories := make(map[schema.GroupVersionKind]adapter.Factory)
	for _, r := range adapter.AllRegistrations() {
		if factory, ok := reg.GetAdapter(r.GVK); ok {
			factories[r.GVK] = factory
		}
	}

	return adapter.NewResourceClientRouter(dynamicClient, factories)
}

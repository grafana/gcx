package resources

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Descriptors is a list of descriptors.
type Descriptors []Descriptor

// Sorted sorts the descriptors by group, version, kind, and name.
func (d Descriptors) Sorted() Descriptors {
	slices.SortStableFunc(d, func(a, b Descriptor) int {
		res := strings.Compare(a.GroupVersion.Group, b.GroupVersion.Group)
		if res != 0 {
			return res
		}

		res = strings.Compare(a.GroupVersion.Version, b.GroupVersion.Version)
		if res != 0 {
			return res
		}

		return strings.Compare(a.Kind, b.Kind)
	})

	return d
}

// Descriptor describes a resource.
type Descriptor struct {
	GroupVersion schema.GroupVersion
	Kind         string
	Singular     string
	Plural       string

	// ClusterScoped is true for kinds that the API server does not serve
	// under a namespace. The zero value means namespaced, so descriptors
	// that providers register statically keep their namespaced behavior.
	ClusterScoped bool
}

// Resource scope names, as Kubernetes uses them in CustomResourceDefinitions.
const (
	ScopeNamespaced = "Namespaced"
	ScopeCluster    = "Cluster"
)

// Scope returns ScopeCluster for cluster-scoped kinds and ScopeNamespaced otherwise.
func (d Descriptor) Scope() string {
	if d.ClusterScoped {
		return ScopeCluster
	}

	return ScopeNamespaced
}

func (d Descriptor) String() string {
	return fmt.Sprintf("%s.%s.%s", d.Plural, d.GroupVersion.Version, d.GroupVersion.Group)
}

// GroupVersionKind returns the GroupVersionKind for the descriptor.
func (d Descriptor) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   d.GroupVersion.Group,
		Version: d.GroupVersion.Version,
		Kind:    d.Kind,
	}
}

// GroupVersionResource returns the GroupVersionResource for the descriptor.
func (d Descriptor) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    d.GroupVersion.Group,
		Version:  d.GroupVersion.Version,
		Resource: d.Plural,
	}
}

// Matches returns true if the descriptor matches the given GroupVersionKind.
func (d Descriptor) Matches(gvk schema.GroupVersionKind) bool {
	return NormalizeGVK(d.GroupVersionKind()) == NormalizeGVK(gvk)
}

package process

import (
	"github.com/grafana/gcx/internal/resources"
)

// NamespaceOverrider is a processor that overrides the namespace of a resource
// with a target namespace. This is useful when pushing resources to a different
// context/org than the one they were pulled from.
type NamespaceOverrider struct {
	targetNamespace string
}

// NewNamespaceOverrider creates a new NamespaceOverrider that will set the
// namespace of all processed resources to the given target namespace.
func NewNamespaceOverrider(targetNamespace string) *NamespaceOverrider {
	return &NamespaceOverrider{
		targetNamespace: targetNamespace,
	}
}

// Process overrides the namespace of the resource with the target namespace.
// If the resource is empty, it returns immediately without error.
func (n *NamespaceOverrider) Process(r *resources.Resource) error {
	if r.IsEmpty() {
		return nil
	}

	// Override the namespace in the unstructured object
	r.Object.SetNamespace(n.targetNamespace)

	return nil
}

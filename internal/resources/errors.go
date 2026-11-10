package resources

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// UnsupportedResourceError reports a well-formed resource selector that the
// server does not serve. It describes server capabilities, not selector syntax.
type UnsupportedResourceError struct {
	Selector string
	Reason   string
}

func (e *UnsupportedResourceError) Error() string {
	return fmt.Sprintf("resource %q: %s", e.Selector, e.Reason)
}

type DuplicatedResourceError struct {
	Ref   ResourceRef
	Files []string
}

func (err DuplicatedResourceError) Error() string {
	return fmt.Sprintf("duplicated resource %q found in %s", err.Ref, strings.Join(err.Files, ", "))
}

type DeprecatedResourceError struct {
	Ref         ResourceRef
	Used        schema.GroupVersion
	Alternative schema.GroupVersion
}

func (err DeprecatedResourceError) Error() string {
	return fmt.Sprintf("apiVersion %q is deprecated; use %q instead", err.Used, err.Alternative)
}

// ClusterScopedMutationError reports a write to a cluster-scoped kind.
// gcx reads cluster-scoped kinds but does not write them yet.
type ClusterScopedMutationError struct {
	Descriptor Descriptor
	Operation  string
}

func (e *ClusterScopedMutationError) Error() string {
	return fmt.Sprintf(
		"cannot %s %s: the kind is cluster-scoped, and gcx supports only read operations (get, pull) on cluster-scoped kinds",
		e.Operation, e.Descriptor.String(),
	)
}

// CheckClusterScopedMutation returns a ClusterScopedMutationError when desc is
// cluster-scoped. It returns nil for namespaced kinds.
func CheckClusterScopedMutation(desc Descriptor, operation string) error {
	if !desc.ClusterScoped {
		return nil
	}

	return &ClusterScopedMutationError{Descriptor: desc, Operation: operation}
}

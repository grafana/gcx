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

// ClusterScopedOperationError reports an operation that gcx does not support
// on a cluster-scoped kind. gcx reads cluster-scoped kinds with get only: it
// does not write them, and it does not pull them because they are not part of
// a stack backup.
type ClusterScopedOperationError struct {
	Descriptor Descriptor
	Operation  string
}

func (e *ClusterScopedOperationError) Error() string {
	return fmt.Sprintf(
		"cannot %s %s: the kind is cluster-scoped, and gcx supports only get on cluster-scoped kinds",
		e.Operation, e.Descriptor.String(),
	)
}

// CheckClusterScopedOperation returns a ClusterScopedOperationError when desc
// is cluster-scoped. It returns nil for namespaced kinds. Callers use it for
// every operation except get.
func CheckClusterScopedOperation(desc Descriptor, operation string) error {
	if !desc.ClusterScoped {
		return nil
	}

	return &ClusterScopedOperationError{Descriptor: desc, Operation: operation}
}

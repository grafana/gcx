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

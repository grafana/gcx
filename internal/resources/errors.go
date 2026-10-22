package resources

import (
	"fmt"
	"strings"
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
	return fmt.Sprintf("duplicated resource '%s' found in %s", err.Ref, strings.Join(err.Files, ", "))
}

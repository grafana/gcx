// Package browse implements the interactive resource explorer behind
// `gcx resources browse`: a fuzzy-filterable list of resource types and
// their objects, with the highlighted entry rendered alongside it.
package browse

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Source supplies the data the browser displays. Every method is called from
// a background command, never from Update, so implementations may block on
// network I/O.
type Source interface {
	// Types returns the resource types to offer at the top level.
	Types(ctx context.Context) (resources.Descriptors, error)
	// List returns every object of the given type. When only some objects
	// could be fetched it returns them together with a *PartialError.
	List(ctx context.Context, desc resources.Descriptor) ([]unstructured.Unstructured, error)
	// Schemas returns the spec schemas of the given types in one bounded
	// request, keyed by Selector. Types without a schema are absent. On error
	// the map may still hold the schemas that did resolve.
	Schemas(ctx context.Context, descs resources.Descriptors) (map[string]map[string]any, error)
}

// PartialError reports that a List returned some objects but others could
// not be fetched.
type PartialError struct {
	Failed int
	Err    error // the first failure
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("%d failed: %v", e.Failed, e.Err)
}

func (e *PartialError) Unwrap() error { return e.Err }

// Opener opens a Grafana deep link, typically in the user's browser.
type Opener func(url string) error

// Selector returns the fully qualified selector for a type, as accepted by
// `gcx resources get` (plural.version.group).
func Selector(d resources.Descriptor) string {
	return d.Plural + "." + d.GroupVersion.Version + "." + d.GroupVersion.Group
}

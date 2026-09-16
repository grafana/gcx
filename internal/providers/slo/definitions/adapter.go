package definitions

import (
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for SLO resources.
	APIVersion = "slo.ext.grafana.app/v1alpha1"
	// Kind is the kind for SLO resources.
	Kind = "SLO"
)

// FileNamer returns a function that produces a file path for an SLO resource.
// The path follows the pattern: SLO/{name}.{format}.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("SLO/%s.%s", res.Raw.GetName(), outputFormat)
	}
}

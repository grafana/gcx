package reports

import (
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

const (
	// APIVersion is the API version for SLO Report resources.
	APIVersion = "slo.ext.grafana.app/v1alpha1"
	// Kind is the kind for SLO Report resources.
	Kind = "Report"
)

// FileNamer returns a function that produces a file path for a Report resource.
// The path follows the pattern: Report/{name}.{format}.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("Report/%s.%s", res.Raw.GetName(), outputFormat)
	}
}

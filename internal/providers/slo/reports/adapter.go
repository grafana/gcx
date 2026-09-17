package reports

import (
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

// FileNamer returns a function that produces a file path for a Report resource.
// The path follows the pattern: Report/{name}.{format}.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("Report/%s.%s", res.Raw.GetName(), outputFormat)
	}
}

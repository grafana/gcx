package definitions

import (
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

// FileNamer returns a function that produces a file path for an SLO resource.
// The path follows the pattern: SLO/{name}.{format}.
func FileNamer(outputFormat string) func(*resources.Resource) string {
	return func(res *resources.Resource) string {
		return fmt.Sprintf("SLO/%s.%s", res.Raw.GetName(), outputFormat)
	}
}

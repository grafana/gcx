package fail

import (
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

// UsageError is the shared command usage error, retained here for callers of
// the fail package. Providers use internal/gcxerrors directly.
type UsageError = gcxerrors.UsageError

// NewCommandUsageError preserves the fail package's command usage helper.
func NewCommandUsageError(cmd *cobra.Command, message string, cause error) *UsageError {
	return gcxerrors.NewCommandUsageError(cmd, message, cause)
}

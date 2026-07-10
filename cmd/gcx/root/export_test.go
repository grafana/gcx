package root

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// NewCommandForTest exposes the internal newCommand constructor for use in
// external (_test) packages. It is only compiled during `go test`.
func NewCommandForTest(version string, pp []providers.Provider) *cobra.Command {
	return newCommand(version, pp)
}

// FlagUsageErrorForTest exposes flagUsageError for external (_test) packages.
func FlagUsageErrorForTest(cmd *cobra.Command, err error, invocationArgs []string) error {
	return flagUsageError(cmd, err, invocationArgs)
}

// SubstituteFlagForTest exposes substituteFlag for external (_test) packages.
func SubstituteFlagForTest(args []string, unknown, candidate string) ([]string, bool) {
	return substituteFlag(args, unknown, candidate)
}

// RedactSensitiveValuesForTest exposes redactSensitiveValues for external
// (_test) packages.
func RedactSensitiveValuesForTest(args []string) []string {
	return redactSensitiveValues(args)
}

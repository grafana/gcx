package root

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

func exitCodesHelpTopic() *cobra.Command {
	var help strings.Builder
	help.WriteString("Exit codes let scripts distinguish outcomes without parsing error messages.\n\n")
	for _, info := range gcxerrors.ExitCodes() {
		fmt.Fprintf(&help, "  %d  %s\n     %s\n     Example: %s\n\n", info.Code, info.Name, info.Description, info.Example)
	}
	return &cobra.Command{
		Use:   "exit-codes",
		Short: "Explain exit codes with example scenarios.",
		Long:  strings.TrimSpace(help.String()),
	}
}

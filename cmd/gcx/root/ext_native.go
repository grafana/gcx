//go:build !wasip1

package root

import (
	"slices"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/ext"
	"github.com/spf13/cobra"
)

func addExtensions(cmd *cobra.Command) { cmd.AddCommand(ext.Command()) }

// RewriteExtensionArgs protects extension-owned flags from Cobra's parser.
func RewriteExtensionArgs(cmd *cobra.Command, args []string) []string {
	trimmed, ok := trimLeadingRootFlags(cmd, args)
	if !ok || len(trimmed) < 2 || trimmed[0] != "ext" {
		return args
	}
	name := trimmed[1]
	if strings.HasPrefix(name, "-") || name == "help" || slices.Contains(ext.Verbs(), name) {
		return args
	}
	boundary := len(args) - len(trimmed) + 2
	out := append([]string(nil), args[:boundary]...)
	out = append(out, "--")
	return append(out, args[boundary:]...)
}

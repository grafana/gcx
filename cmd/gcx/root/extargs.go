package root

import (
	"slices"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/ext"
	"github.com/spf13/cobra"
)

// RewriteExtensionArgs inserts a "--" separator after an extension's name so
// that everything the user typed for the extension reaches it verbatim.
//
// Without it, Cobra parses the extension's own flags against gcx's flag set and
// fails on the first one it does not know. Rewriting the args instead of
// disabling flag parsing on `ext` keeps gcx's global flags (--context, --agent,
// -v) working normally in front of `ext`, which is where they belong.
//
// Returns args unchanged for anything that is not an extension dispatch.
func RewriteExtensionArgs(rootCmd *cobra.Command, args []string) []string {
	trimmed, ok := trimLeadingRootFlags(rootCmd, args)
	if !ok || len(trimmed) < 2 || trimmed[0] != ext.CommandName {
		return args
	}

	name := trimmed[1]
	if strings.HasPrefix(name, "-") || name == "help" || slices.Contains(ext.Verbs(), name) {
		return args
	}
	// The user separated the arguments themselves.
	if len(trimmed) > 2 && trimmed[2] == "--" {
		return args
	}

	// trimmed is a suffix of args, so the offset locates the name in args.
	nameIdx := len(args) - len(trimmed) + 1
	out := make([]string, 0, len(args)+1)
	out = append(out, args[:nameIdx+1]...)
	out = append(out, "--")
	return append(out, args[nameIdx+1:]...)
}

package root

import (
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ValidateArgs detects stray positional arguments on non-runnable group
// commands and converts them into a local unknown-command usage error.
//
// The passed command tree may be mutated during validation because Cobra flag
// parsing updates flag state. Callers that will execute the command afterwards
// should validate against a separate command instance.
func ValidateArgs(rootCmd *cobra.Command, args []string) error {
	if rootCmd == nil {
		return nil
	}

	trimmedArgs, ok := trimLeadingRootFlags(rootCmd, args)
	if !ok {
		return nil
	}

	cmd, remaining, ok := traverseArgs(rootCmd, trimmedArgs)
	if !ok || cmd == nil {
		return nil
	}

	if cmd.Runnable() || !cmd.HasAvailableSubCommands() {
		return nil
	}

	if !parseGroupFlags(cmd, remaining) {
		return nil
	}

	positionals := cmd.Flags().Args()
	if len(positionals) == 0 {
		return nil
	}

	commandPath := strings.TrimSpace(cmd.CommandPath())
	suggestions := []string{}
	if commandPath != "" {
		suggestions = append(suggestions, fmt.Sprintf("Run '%s --help' for full usage and examples", commandPath))
	}

	return &fail.UsageError{
		Message:     formatUnknownGroupCommand(cmd, positionals[0]),
		Suggestions: suggestions,
	}
}

func trimLeadingRootFlags(rootCmd *cobra.Command, args []string) ([]string, bool) {
	fs := pflag.NewFlagSet(rootCmd.Name(), pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.SetInterspersed(false)
	fs.AddFlagSet(rootCmd.PersistentFlags())
	fs.AddFlagSet(rootCmd.Flags())

	if err := fs.Parse(args); err != nil {
		return nil, false
	}

	return fs.Args(), true
}

func traverseArgs(rootCmd *cobra.Command, args []string) (*cobra.Command, []string, bool) {
	cmd, remaining, err := rootCmd.Traverse(args)
	return cmd, remaining, err == nil
}

func parseGroupFlags(cmd *cobra.Command, args []string) bool {
	return cmd.ParseFlags(args) == nil
}

func formatUnknownGroupCommand(cmd *cobra.Command, unknown string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown command %q for %q\n\n", unknown, cmd.CommandPath())
	fmt.Fprintln(&b, "Usage:")
	fmt.Fprintf(&b, "  %s <command>", cmd.CommandPath())
	if cmd.HasAvailableLocalFlags() || cmd.HasAvailableInheritedFlags() {
		fmt.Fprint(&b, " [flags]")
	}
	fmt.Fprintln(&b)

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Available Commands:")
		for _, sub := range cmd.Commands() {
			if !sub.IsAvailableCommand() || sub.Name() == "help" {
				continue
			}
			fmt.Fprintf(&b, "  %-16s %s\n", sub.Name(), sub.Short)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

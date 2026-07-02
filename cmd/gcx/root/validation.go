package root

import (
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/suggest"
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

	// Cobra registers its hidden shell-completion helpers lazily inside
	// ExecuteC, so they are absent from the command tree at validation time.
	// Let them through to Cobra's normal dispatch.
	if len(trimmedArgs) > 0 {
		switch trimmedArgs[0] {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return nil
		}
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

	candidates := subcommandCandidates(cmd, positionals[0])

	suggestions := []string{}
	corrections := []gcxerrors.Correction{}
	for _, sub := range candidates {
		// CommandPath is a space-separated path, not a single token, so it
		// joins as-is; only the subcommand name and user tokens get quoted.
		invocation := cmd.CommandPath() + " " + shellJoin(append([]string{sub.Name()}, positionals[1:]...))
		suggestions = append(suggestions, fmt.Sprintf("Did you mean '%s'?", invocation))
		corrections = append(corrections, gcxerrors.Correction{
			Command: invocation,
			Hint:    sub.Annotations[agent.AnnotationLLMHint],
		})
	}

	commandPath := strings.TrimSpace(cmd.CommandPath())
	if commandPath != "" {
		suggestions = append(suggestions, fmt.Sprintf("Run '%s --help' for full usage and examples", commandPath))
	}

	return &fail.UsageError{
		Message:     formatUnknownGroupCommand(cmd, positionals[0], candidates),
		Suggestions: suggestions,
		Corrections: corrections,
	}
}

// subcommandCandidates fuzzy-matches an unknown token against the group's
// available subcommand names and aliases, returning the matched subcommands
// best-first (deduplicated when a name and alias hit the same command).
func subcommandCandidates(cmd *cobra.Command, unknown string) []*cobra.Command {
	vocabulary := []string{}
	byToken := map[string]*cobra.Command{}
	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() || sub.Name() == "help" {
			continue
		}
		for _, token := range append([]string{sub.Name()}, sub.Aliases...) {
			vocabulary = append(vocabulary, token)
			byToken[strings.ToLower(token)] = sub
		}
	}

	candidates := []*cobra.Command{}
	seen := map[*cobra.Command]bool{}
	for _, token := range suggest.Candidates(unknown, vocabulary) {
		sub := byToken[strings.ToLower(token)]
		if sub == nil || seen[sub] {
			continue
		}
		seen[sub] = true
		candidates = append(candidates, sub)
	}
	return candidates
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

func formatUnknownGroupCommand(cmd *cobra.Command, unknown string, candidates []*cobra.Command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown command %q for %q\n\n", unknown, cmd.CommandPath())
	fmt.Fprintln(&b, "Usage:")
	fmt.Fprintf(&b, "  %s <command>", cmd.CommandPath())
	if cmd.HasAvailableLocalFlags() || cmd.HasAvailableInheritedFlags() {
		fmt.Fprint(&b, " [flags]")
	}
	fmt.Fprintln(&b)

	if len(candidates) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Did you mean this?")
		for _, sub := range candidates {
			fmt.Fprintf(&b, "  %s\n", sub.Name())
		}
	}

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

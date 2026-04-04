package style

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"
)

// HelpFunc returns a custom Cobra help function that renders Long descriptions
// and Examples through glamour markdown rendering when styling is enabled.
// Falls back to Cobra's default help when styling is disabled.
func HelpFunc(defaultHelp func(*cobra.Command, []string)) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if !IsStylingEnabled() {
			defaultHelp(cmd, args)
			return
		}

		w := cmd.OutOrStdout()

		// --- Long description ---
		if cmd.Long != "" {
			rendered, err := glamour.Render(cmd.Long, "dark")
			if err == nil {
				fmt.Fprint(w, rendered)
			} else {
				fmt.Fprintln(w, cmd.Long)
			}
		} else if cmd.Short != "" {
			fmt.Fprintln(w, cmd.Short)
		}
		fmt.Fprintln(w)

		// --- Usage ---
		if cmd.Runnable() {
			fmt.Fprintln(w, "Usage:")
			fmt.Fprintf(w, "  %s\n", cmd.UseLine())
			if cmd.HasAvailableSubCommands() {
				fmt.Fprintf(w, "  %s [command]\n", cmd.CommandPath())
			}
			fmt.Fprintln(w)
		} else if cmd.HasAvailableSubCommands() {
			fmt.Fprintln(w, "Usage:")
			fmt.Fprintf(w, "  %s [command]\n", cmd.CommandPath())
			fmt.Fprintln(w)
		}

		// --- Aliases ---
		if len(cmd.Aliases) > 0 {
			fmt.Fprintln(w, "Aliases:")
			fmt.Fprintf(w, "  %s\n", cmd.NameAndAliases())
			fmt.Fprintln(w)
		}

		// --- Examples ---
		if cmd.HasExample() {
			md := "```\n" + strings.TrimSpace(cmd.Example) + "\n```"
			rendered, err := glamour.Render(md, "dark")
			if err == nil {
				fmt.Fprintln(w, "Examples:")
				fmt.Fprint(w, rendered)
			} else {
				fmt.Fprintln(w, "Examples:")
				fmt.Fprintf(w, "%s\n", cmd.Example)
			}
			fmt.Fprintln(w)
		}

		// --- Available commands ---
		if cmd.HasAvailableSubCommands() {
			fmt.Fprintln(w, "Available Commands:")
			for _, sub := range cmd.Commands() {
				if sub.IsAvailableCommand() || sub.Name() == "help" {
					fmt.Fprintf(w, "  %-16s %s\n", sub.Name(), sub.Short)
				}
			}
			fmt.Fprintln(w)
		}

		// --- Flags ---
		if cmd.HasAvailableLocalFlags() {
			fmt.Fprintln(w, "Flags:")
			fmt.Fprint(w, cmd.LocalFlags().FlagUsages())
			fmt.Fprintln(w)
		}

		if cmd.HasAvailableInheritedFlags() {
			fmt.Fprintln(w, "Global Flags:")
			fmt.Fprint(w, cmd.InheritedFlags().FlagUsages())
			fmt.Fprintln(w)
		}

		// --- Additional help ---
		if cmd.HasHelpSubCommands() {
			fmt.Fprintln(w, "Additional help topics:")
			for _, sub := range cmd.Commands() {
				if sub.IsAdditionalHelpTopicCommand() {
					var buf bytes.Buffer
					fmt.Fprintf(&buf, "  %-16s %s", sub.CommandPath(), sub.Short)
					fmt.Fprintln(w, buf.String())
				}
			}
			fmt.Fprintln(w)
		}

		if cmd.HasAvailableSubCommands() {
			fmt.Fprintf(w, "Use \"%s [command] --help\" for more information about a command.\n", cmd.CommandPath())
		}
	}
}

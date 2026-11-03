package style

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/agent"
	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/spf13/cobra"
)

// jsonDiscoveryTip is the help text shown for commands that support --json field selection.
const jsonDiscoveryTip = "Use --json list to discover available fields, --json field1,field2 to select specific fields."

// relatedSkillFooter returns the "Related skill" footer lines for a command, or
// nil when no skill is mapped to the command or its ancestors.
func relatedSkillFooter(cmd *cobra.Command) []string {
	skills := agent.SkillsForCommand(cmd)
	if len(skills) == 0 {
		return nil
	}
	lines := []string{"Related skill(s):"}
	for _, name := range skills {
		if desc := skillops.ShortDescription(claudeplugin.SkillsFS(), name); desc != "" {
			lines = append(lines, "  "+name+" — "+desc)
		} else {
			lines = append(lines, "  "+name)
		}
	}
	lines = append(
		lines,
		"Read a skill only if it fits your task",
		"These skills are bundled — no install needed",
		"gcx agent skills get <name> -otext",
	)
	return lines
}

// HelpTopic describes a registered root help topic.
type HelpTopic struct {
	Name  string
	Short string
}

// HelpFunc returns a custom Cobra help function that renders Long descriptions
// and Examples through glamour markdown rendering when styling is enabled.
// Root topics are listed alongside Cobra's native additional help topics.
func HelpFunc(defaultHelp func(*cobra.Command, []string), topics ...HelpTopic) func(*cobra.Command, []string) {
	return helpFunc(defaultHelp, IsStylingEnabled, topics...)
}

func helpFunc(defaultHelp func(*cobra.Command, []string), stylingEnabled func() bool, topics ...HelpTopic) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		noColor, _ := cmd.Flags().GetBool("no-color")
		if noColor || !stylingEnabled() {
			defaultHelp(cmd, args)
			w := cmd.OutOrStdout()
			if !cmd.HasParent() && len(topics) > 0 {
				fmt.Fprintln(w, "\nAdditional help topics:")
				for _, topic := range topics {
					fmt.Fprintf(w, "  %-16s %s\n", cmd.CommandPath()+" help "+topic.Name, topic.Short)
				}
				fmt.Fprintln(w)
			}
			// Append JSON discovery tip for commands that support --json.
			if f := cmd.Flags().Lookup("json"); f != nil {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "Tip:")
				fmt.Fprintln(w, "  "+jsonDiscoveryTip)
			}
			if footer := relatedSkillFooter(cmd); footer != nil {
				fmt.Fprintln(w)
				for _, line := range footer {
					fmt.Fprintln(w, line)
				}
			}
			return
		}

		w := cmd.OutOrStdout()

		// Show ASCII logo for the root command only.
		if !cmd.HasParent() {
			if logo := RenderLogo(); logo != "" {
				_, _ = lipgloss.Fprintln(w, logo)
			}
		}

		// --- Long description ---
		if cmd.Long != "" {
			// Word wrap is disabled so long tokens (notably doc URLs) are not
			// hard-broken across lines, which would stop terminals from
			// detecting them as clickable links. This matches the non-styled
			// fallback, which prints Long verbatim.
			rendered, err := renderLong(cmd.Long)
			if err == nil {
				_, _ = lipgloss.Fprint(w, rendered)
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
			rendered, err := renderMarkdown(md, true)
			if err == nil {
				fmt.Fprintln(w, "Examples:")
				_, _ = lipgloss.Fprint(w, rendered)
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

		// --- JSON discovery tip ---
		if f := cmd.Flags().Lookup("json"); f != nil {
			fmt.Fprintln(w, "Tip:")
			fmt.Fprintln(w, "  "+jsonDiscoveryTip)
			fmt.Fprintln(w)
		}

		// --- Related skill footer ---
		if footer := relatedSkillFooter(cmd); footer != nil {
			for _, line := range footer {
				fmt.Fprintln(w, line)
			}
			fmt.Fprintln(w)
		}

		// --- Additional help ---
		writeHelpTopics(w, cmd, topics)

		if cmd.HasAvailableSubCommands() {
			fmt.Fprintf(w, "Use \"%s [command] --help\" for more information about a command.\n", cmd.CommandPath())
		}
	}
}

func writeHelpTopics(w io.Writer, cmd *cobra.Command, topics []HelpTopic) {
	if cmd.HasParent() {
		topics = nil
	}
	if !cmd.HasHelpSubCommands() && len(topics) == 0 {
		return
	}
	fmt.Fprintln(w, "Additional help topics:")
	for _, sub := range cmd.Commands() {
		if sub.IsAdditionalHelpTopicCommand() {
			fmt.Fprintf(w, "  %-16s %s\n", sub.CommandPath(), sub.Short)
		}
	}
	for _, topic := range topics {
		fmt.Fprintf(w, "  %-16s %s\n", cmd.CommandPath()+" help "+topic.Name, topic.Short)
	}
	fmt.Fprintln(w)
}

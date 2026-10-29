package root

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

type helpTopic struct {
	name  string
	short string
	text  string
}

func registerHelpTopics(rootCmd *cobra.Command) {
	topics := []helpTopic{exitCodesHelpTopic()}
	helpCmd, _, _ := rootCmd.Find([]string{"help"})
	defaultRun := helpCmd.Run
	helpCmd.Run = func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			for _, topic := range topics {
				if args[0] == topic.name {
					fmt.Fprintln(cmd.OutOrStdout(), topic.text)
					return
				}
			}
		}
		defaultRun(cmd, args)
	}

	defaultComplete := helpCmd.ValidArgsFunction
	helpCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		completions, directive := defaultComplete(cmd, args, toComplete)
		if len(args) == 0 {
			for _, topic := range topics {
				if strings.HasPrefix(topic.name, toComplete) {
					completions = append(completions, cobra.CompletionWithDesc(topic.name, topic.short))
				}
			}
		}
		return completions, directive
	}
	rootCmd.SetHelpCommand(helpCmd)

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		if cmd == rootCmd {
			fmt.Fprintln(cmd.OutOrStdout(), "\nAdditional help topics:")
			for _, topic := range topics {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s help %s  %s\n", rootCmd.DisplayName(), topic.name, topic.short)
			}
		}
	})
}

func exitCodesHelpTopic() helpTopic {
	var help strings.Builder
	help.WriteString("Exit codes let scripts distinguish outcomes without parsing error messages.\n\n")
	for _, info := range gcxerrors.ExitCodes() {
		fmt.Fprintf(&help, "  %d  %s\n     %s\n     Example: %s\n\n", info.Code, info.Name, info.Description, info.Example)
	}
	return helpTopic{
		name:  "exit-codes",
		short: "Explain exit codes with example scenarios.",
		text:  strings.TrimSpace(help.String()),
	}
}

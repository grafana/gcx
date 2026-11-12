package root

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

type helpTopic struct {
	style.HelpTopic

	text string
}

func registerHelpTopics(rootCmd *cobra.Command) []style.HelpTopic {
	topics := []helpTopic{exitCodesHelpTopic()}
	helpCmd, _, err := rootCmd.Find([]string{"help"})
	if err != nil || helpCmd.Name() != "help" || helpCmd.Run == nil {
		return nil
	}
	defaultRun := helpCmd.Run
	helpCmd.Run = func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			for _, topic := range topics {
				if args[0] == topic.Name {
					fmt.Fprintln(cmd.OutOrStdout(), topic.text)
					return
				}
			}
		}
		defaultRun(cmd, args)
	}

	defaultComplete := helpCmd.ValidArgsFunction
	helpCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		var completions []cobra.Completion
		directive := cobra.ShellCompDirectiveNoFileComp
		if defaultComplete != nil {
			completions, directive = defaultComplete(cmd, args, toComplete)
		}
		if len(args) == 0 {
			for _, topic := range topics {
				if strings.HasPrefix(topic.Name, toComplete) {
					completions = append(completions, cobra.CompletionWithDesc(topic.Name, topic.Short))
				}
			}
		}
		return completions, directive
	}
	rootCmd.SetHelpCommand(helpCmd)

	list := make([]style.HelpTopic, 0, len(topics))
	for _, topic := range topics {
		list = append(list, topic.HelpTopic)
	}
	return list
}

func exitCodesHelpTopic() helpTopic {
	var help strings.Builder
	help.WriteString("Exit codes let scripts distinguish outcomes without parsing error messages.\n\n")
	for _, info := range gcxerrors.ExitCodes() {
		fmt.Fprintf(&help, "  %d  %s\n     %s\n     Example: %s\n\n", info.Code, info.Name, info.Description, info.Example)
	}
	return helpTopic{
		HelpTopic: style.HelpTopic{
			Name:  "exit-codes",
			Short: "Explain exit codes with example scenarios.",
		},
		text: strings.TrimSpace(help.String()),
	}
}

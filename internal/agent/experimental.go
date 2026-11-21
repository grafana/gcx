package agent

import (
	"strings"

	"github.com/spf13/cobra"
)

const experimentalPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

func MarkExperimental(cmd *cobra.Command) {
	if cmd.Annotations[AnnotationStability] != StabilityExperimental {
		long := cmd.Long
		if long == "" {
			long = cmd.Short
		}
		cmd.Short = "[experimental] " + cmd.Short
		cmd.Long = strings.TrimSpace(experimentalPreamble + "\n\n" + long)
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations[AnnotationStability] = StabilityExperimental
	}
	for _, sub := range cmd.Commands() {
		MarkExperimental(sub)
	}
}

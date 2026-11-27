package agent

import "github.com/spf13/cobra"

// ExperimentalPreamble opens the long description of every experimental
// command (docs/design/experimental-commands.md).
const ExperimentalPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

// MarkExperimental marks cmd and every command under it experimental: the
// short description starts with [experimental], the long description with
// ExperimentalPreamble (the short description stands in for an empty one),
// and the command carries the stability annotation. Other annotations are
// kept, and a command already marked is left as it is, so marking a parent
// and then a child it already covers does not mark the child twice.
func MarkExperimental(cmd *cobra.Command) {
	if cmd.Annotations[AnnotationStability] != StabilityExperimental {
		long := cmd.Long
		if long == "" {
			long = cmd.Short
		}
		cmd.Short = "[experimental] " + cmd.Short
		cmd.Long = ExperimentalPreamble + "\n\n" + long
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations[AnnotationStability] = StabilityExperimental
	}
	for _, sub := range cmd.Commands() {
		MarkExperimental(sub)
	}
}

package agent

import "github.com/spf13/cobra"

// ExperimentalPreamble opens the long description of every experimental
// command (docs/design/experimental-commands.md).
const ExperimentalPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

// MarkExperimental marks cmd experimental: its short description starts with
// [experimental], its long description with ExperimentalPreamble (the short
// description stands in for an empty one), and it carries the stability
// annotation. Other annotations are kept.
func MarkExperimental(cmd *cobra.Command) {
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

package commandutil

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// ShouldDefaultDetailToYAML reports whether a detail command should switch
// from its list-oriented default format to YAML.
//
// Agent mode must keep JSON as the implicit default unless the user explicitly
// requested another format.
func ShouldDefaultDetailToYAML(cmd *cobra.Command) bool {
	if agent.IsAgentMode() {
		return false
	}

	flags := cmd.Flags()
	return !flags.Changed("output") && !flags.Changed("json")
}

// ValidateDetailOutputFormat rejects table-oriented output formats for a
// single-item detail view when no matching detail table codec exists.
func ValidateDetailOutputFormat(cmd *cobra.Command, outputFormat, singular string, exampleArgs ...string) error {
	switch outputFormat {
	case "table", "wide":
		example := append([]string{cmd.CommandPath()}, exampleArgs...)
		example = append(example, "-o", "json")
		return fmt.Errorf(
			"%s does not support -o %s for a single %s; rerun without -o for YAML or use JSON instead: %s",
			cmd.CommandPath(), outputFormat, singular, strings.Join(example, " "),
		)
	default:
		return nil
	}
}

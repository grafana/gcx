package output

import (
	"slices"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// ErrorInBand reports whether failures belong in the stdout result, using
// the executed command's explicit output flags and the current agent mode.
func ErrorInBand(humanOutputFlagActive, jsonFlagActive bool) bool {
	return (agent.IsAgentMode() && !humanOutputFlagActive) || jsonFlagActive
}

// IsJSONFlagActive reports whether the executed command declares --json and
// the user explicitly set it.
func IsJSONFlagActive(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("json")
	return f != nil && f.Changed
}

// IsHumanOutputFlagActive reports whether cmd has an explicitly set -o that selects
// the stdout format and is neither json nor agents, with no --json/--jq field
// selection or filtering. It is false where -o does not own stdout: a -o that
// is not an output-format flag (FormatFlagAnnotation, e.g. a
// destination directory), pinned-format commands (resources pull/edit) and
// artifact formats (-o pprof) keep their JSON receipt on stdout.
func IsHumanOutputFlagActive(cmd *cobra.Command) bool {
	for _, name := range []string{"json", "jq"} {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return false
		}
	}
	f := cmd.Flags().Lookup("output")
	if f == nil || !f.Changed {
		return false
	}
	if _, isFormat := f.Annotations[FormatFlagAnnotation]; !isFormat {
		return false
	}
	if _, pinned := f.Annotations[PinnedFormatAnnotation]; pinned {
		return false
	}
	value := f.Value.String()
	if slices.Contains(f.Annotations[ArtifactFormatsAnnotation], value) {
		return false
	}
	return !IsJSONFormat(value)
}

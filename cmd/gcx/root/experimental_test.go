package root_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// The marker and preamble are written as literal prose at each call site so
// that help text stays greppable; these tests are what keep the wording, the
// placement, and the agent.stability annotation in agreement.
// See docs/design/experimental-commands.md.
const (
	experimentalMarker   = "[experimental]"
	experimentalPreamble = "This command is experimental. It may be removed, or its subcommands, " +
		"flags and responses may change without following the normal semantic versioning conventions."
)

func isExperimental(cmd *cobra.Command) bool {
	return cmd.Annotations[agent.AnnotationStability] == agent.StabilityExperimental
}

// hasExperimentalShort reports whether the short description carries the marker
// in the one position the standard allows.
func hasExperimentalShort(cmd *cobra.Command) bool {
	return strings.HasPrefix(cmd.Short, experimentalMarker+" ")
}

// TestExperimental_MarkerIsShortDescriptionPrefix rejects the marker anywhere
// but the start of the short description, where readers scanning a command list
// would miss it.
func TestExperimental_MarkerIsShortDescriptionPrefix(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !strings.Contains(cmd.Short, experimentalMarker) {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if !hasExperimentalShort(cmd) {
				t.Errorf("short description must begin with %q, got %q", experimentalMarker+" ", cmd.Short)
			}
		})
	})
}

// TestExperimental_MarkedCommandsCarryStabilityAnnotation keeps the prose and
// the machine-readable metadata from drifting apart.
func TestExperimental_MarkedCommandsCarryStabilityAnnotation(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !hasExperimentalShort(cmd) {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if !isExperimental(cmd) {
				t.Errorf("short description is marked %s but %s annotation is %q, want %q",
					experimentalMarker, agent.AnnotationStability,
					cmd.Annotations[agent.AnnotationStability], agent.StabilityExperimental)
			}
		})
	})
}

// TestExperimental_AnnotatedCommandsAreAdvertised is the other direction: every
// annotated command must say so in its own short description. Help, the command
// catalog and `gcx commands --flat` each show one command's short description
// without its ancestors', so a child cannot inherit the marker.
func TestExperimental_AnnotatedCommandsAreAdvertised(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !isExperimental(cmd) {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if !hasExperimentalShort(cmd) {
				t.Errorf("annotated %s but its short description does not carry %q",
					agent.StabilityExperimental, experimentalMarker)
			}
		})
	})
}

// TestExperimental_LongDescriptionCarriesPreamble checks the wording users are
// shown when they ask for the full help. Unlike the marker, the preamble is
// required on every experimental command, including those inside a subtree
// marked only at its root: --help on a child is reached directly, so it cannot
// rely on the parent to carry the warning.
func TestExperimental_LongDescriptionCarriesPreamble(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !isExperimental(cmd) {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			long := strings.TrimSpace(cmd.Long)
			if long == "" {
				t.Errorf("no long description; it must begin with %q", experimentalPreamble)
				return
			}
			// The preamble is wrapped across lines in source, so compare on
			// collapsed whitespace.
			if !strings.HasPrefix(strings.Join(strings.Fields(long), " "), experimentalPreamble) {
				t.Errorf("long description must begin with %q, got %q", experimentalPreamble, firstLine(long))
			}
		})
	})
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

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

// nearestMarkedAncestor returns the closest ancestor whose short description is
// marked, or nil when the command is not inside a marked subtree.
func nearestMarkedAncestor(cmd *cobra.Command) *cobra.Command {
	for parent := cmd.Parent(); parent != nil; parent = parent.Parent() {
		if hasExperimentalShort(parent) {
			return parent
		}
	}
	return nil
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

// TestExperimental_AnnotatedCommandsAreAdvertised is the other direction: an
// annotated command must say so itself, or sit under a command that does.
func TestExperimental_AnnotatedCommandsAreAdvertised(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !isExperimental(cmd) {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if !hasExperimentalShort(cmd) && nearestMarkedAncestor(cmd) == nil {
				t.Errorf("annotated %s but neither its short description nor any ancestor's carries %q",
					agent.StabilityExperimental, experimentalMarker)
			}
		})
	})
}

// TestExperimental_LongDescriptionCarriesPreamble checks the wording users are
// shown when they ask for the full help.
func TestExperimental_LongDescriptionCarriesPreamble(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !hasExperimentalShort(cmd) {
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

// TestExperimental_SubtreeMarkedOnlyAtItsRoot enforces the one-marker rule: a
// command under an already-marked command must not repeat the marker.
func TestExperimental_SubtreeMarkedOnlyAtItsRoot(t *testing.T) {
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !hasExperimentalShort(cmd) {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if ancestor := nearestMarkedAncestor(cmd); ancestor != nil {
				t.Errorf("%q is already marked, so this command must not repeat the marker", ancestor.CommandPath())
			}
		})
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

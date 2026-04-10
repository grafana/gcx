package root_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func isLeaf(cmd *cobra.Command) bool {
	return cmd.RunE != nil || cmd.Run != nil
}

//nolint:gochecknoglobals // constant-like lookup table for test validation
var validTokenCosts = map[string]bool{
	"small":  true,
	"medium": true,
	"large":  true,
}

//nolint:gochecknoglobals // constant-like skip list for test validation
var skipTokenCost = map[string]bool{
	"gcx completion bash":       true,
	"gcx completion fish":       true,
	"gcx completion powershell": true,
	"gcx completion zsh":        true,
	"gcx help":                  true,
}

func buildRootCmd() *cobra.Command {
	return root.NewCommandForTest("v0.0.0-test", providers.All())
}

func TestConsistency_AllLeafCommandsHaveTokenCost(t *testing.T) {
	rootCmd := buildRootCmd()

	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		path := cmd.CommandPath()
		if skipTokenCost[path] {
			return
		}
		t.Run(path, func(t *testing.T) {
			cost, ok := cmd.Annotations[agent.AnnotationTokenCost]
			if !ok || cost == "" {
				t.Errorf("missing %s annotation", agent.AnnotationTokenCost)
				return
			}
			if !validTokenCosts[cost] {
				t.Errorf("invalid token cost %q (want small, medium, or large)", cost)
			}
		})
	})
}

func TestConsistency_NonSmallCommandsHaveLLMHint(t *testing.T) {
	rootCmd := buildRootCmd()

	for _, cost := range []string{"medium", "large"} {
		t.Run(cost, func(t *testing.T) {
			agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
				if !isLeaf(cmd) || cmd.Hidden {
					return
				}
				if cmd.Annotations[agent.AnnotationTokenCost] != cost {
					return
				}
				path := cmd.CommandPath()
				t.Run(path, func(t *testing.T) {
					if cmd.Annotations[agent.AnnotationLLMHint] == "" {
						t.Errorf("token_cost is %q but missing %s annotation", cost, agent.AnnotationLLMHint)
					}
				})
			})
		})
	}
}

func TestConsistency_LLMHintRequiresTokenCost(t *testing.T) {
	rootCmd := buildRootCmd()

	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		path := cmd.CommandPath()
		t.Run(path, func(t *testing.T) {
			if cmd.Annotations[agent.AnnotationLLMHint] != "" && cmd.Annotations[agent.AnnotationTokenCost] == "" {
				t.Errorf("has %s but missing %s", agent.AnnotationLLMHint, agent.AnnotationTokenCost)
			}
		})
	})
}

func TestConsistency_OnlyKnownAnnotationKeys(t *testing.T) {
	knownKeys := map[string]bool{
		agent.AnnotationTokenCost:          true,
		agent.AnnotationLLMHint:            true,
		agent.AnnotationRequiredScope:      true,
		agent.AnnotationRequiredRole:       true,
		agent.AnnotationRequiredAction:     true,
		cobra.BashCompOneRequiredFlag:      true,
		cobra.BashCompCustom:               true,
		cobra.BashCompFilenameExt:          true,
		cobra.BashCompSubdirsInDir:         true,
		cobra.CommandDisplayNameAnnotation: true,
	}

	rootCmd := buildRootCmd()

	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		for key := range cmd.Annotations {
			t.Run(path+"/"+key, func(t *testing.T) {
				if !knownKeys[key] {
					t.Errorf("unknown annotation key %q", key)
				}
			})
		}
	})
}

// TestConsistency_NoOrphanedRegistryEntries verifies every key in the
// centralized annotation registry matches an actual command in the tree.
func TestConsistency_NoOrphanedRegistryEntries(t *testing.T) {
	rootCmd := buildRootCmd()

	// Build a set of all command paths in the tree.
	paths := make(map[string]bool)
	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		paths[cmd.CommandPath()] = true
	})

	for _, regPath := range agent.AnnotationRegistryPaths() {
		t.Run(regPath, func(t *testing.T) {
			if !paths[regPath] {
				t.Errorf("registry entry %q does not match any command in the tree", regPath)
			}
		})
	}
}

package root_test

import (
	"regexp"
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

// validTokenCostQualified matches the qualified form some commands use to
// describe their flag-dependent cost surface (per the OnCall alert-groups
// spec FR-111 through FR-114): a bare enum value optionally followed by a
// parenthesised qualifier, e.g. `small (large with --all)` or
// `medium (small with --slim)`. The qualifier carries actionable guidance
// for an LLM reading the annotation; the bare enum prefix preserves the
// underlying classification.
var validTokenCostQualified = regexp.MustCompile(`^(small|medium|large) \(.+\)$`) // permissive qualifier to allow future phrasings beyond "X with --flag" form

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
			if !validTokenCosts[cost] && !validTokenCostQualified.MatchString(cost) {
				t.Errorf("invalid token cost %q (want small, medium, or large; or `<level> (<qualifier>)`)", cost)
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
		agent.AnnotationSkill:              true,
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

// TestConsistency_SkillMappingResolvesToCommands verifies every key in the
// command-area-to-skill registry matches an actual command in the tree.
func TestConsistency_SkillMappingResolvesToCommands(t *testing.T) {
	rootCmd := buildRootCmd()

	paths := make(map[string]bool)
	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		paths[cmd.CommandPath()] = true
	})

	for _, skillPath := range agent.CommandSkillPaths() {
		t.Run(skillPath, func(t *testing.T) {
			if !paths[skillPath] {
				t.Errorf("skill mapping key %q does not match any command in the tree", skillPath)
			}
		})
	}
}

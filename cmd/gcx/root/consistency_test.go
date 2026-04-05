package root_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// walkCommands recursively calls fn on every command in the tree.
func walkCommands(cmd *cobra.Command, fn func(*cobra.Command)) {
	fn(cmd)
	for _, sub := range cmd.Commands() {
		walkCommands(sub, fn)
	}
}

// isLeaf returns true if cmd is an executable leaf command (has RunE or Run).
func isLeaf(cmd *cobra.Command) bool {
	return cmd.RunE != nil || cmd.Run != nil
}

var validTokenCosts = map[string]bool{
	"small":  true,
	"medium": true,
	"large":  true,
}

// skipTokenCost lists commands where token_cost does not apply.
var skipTokenCost = map[string]bool{
	"gcx completion bash":       true, // shell completion generator
	"gcx completion fish":       true, // shell completion generator
	"gcx completion powershell": true, // shell completion generator
	"gcx completion zsh":        true, // shell completion generator
}

func buildRootCmd() *cobra.Command {
	return root.NewCommandForTest("v0.0.0-test", providers.All())
}

func TestConsistency_AllLeafCommandsHaveTokenCost(t *testing.T) {
	rootCmd := buildRootCmd()

	walkCommands(rootCmd, func(cmd *cobra.Command) {
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

func TestConsistency_LargeCommandsHaveLLMHint(t *testing.T) {
	rootCmd := buildRootCmd()

	walkCommands(rootCmd, func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		path := cmd.CommandPath()
		t.Run(path, func(t *testing.T) {
			cost := cmd.Annotations[agent.AnnotationTokenCost]
			if cost != "large" {
				return
			}
			hint := cmd.Annotations[agent.AnnotationLLMHint]
			if hint == "" {
				t.Errorf("token_cost is \"large\" but missing %s annotation", agent.AnnotationLLMHint)
			}
		})
	})
}

func TestConsistency_MediumCommandsHaveLLMHint(t *testing.T) {
	rootCmd := buildRootCmd()

	walkCommands(rootCmd, func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		path := cmd.CommandPath()
		t.Run(path, func(t *testing.T) {
			cost := cmd.Annotations[agent.AnnotationTokenCost]
			if cost != "medium" {
				return
			}
			hint := cmd.Annotations[agent.AnnotationLLMHint]
			if hint == "" {
				t.Errorf("token_cost is \"medium\" but missing %s annotation", agent.AnnotationLLMHint)
			}
		})
	})
}

func TestConsistency_LLMHintRequiresTokenCost(t *testing.T) {
	rootCmd := buildRootCmd()

	walkCommands(rootCmd, func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		path := cmd.CommandPath()
		t.Run(path, func(t *testing.T) {
			hint := cmd.Annotations[agent.AnnotationLLMHint]
			cost := cmd.Annotations[agent.AnnotationTokenCost]
			if hint != "" && cost == "" {
				t.Errorf("has %s but missing %s", agent.AnnotationLLMHint, agent.AnnotationTokenCost)
			}
		})
	})
}

func TestConsistency_OnlyKnownAnnotationKeys(t *testing.T) {
	knownKeys := map[string]bool{
		agent.AnnotationTokenCost:      true,
		agent.AnnotationLLMHint:        true,
		agent.AnnotationRequiredScope:  true,
		agent.AnnotationRequiredRole:   true,
		agent.AnnotationRequiredAction: true,
		"cobra_annotation_bash_completion_one_required_flag":         true,
		cobra.CommandDisplayNameAnnotation:                           true,
		"cobra_annotation_bash_completion_custom":                    true,
		"cobra_annotation_bash_completion_completion_fn_annotations": true,
	}

	rootCmd := buildRootCmd()

	walkCommands(rootCmd, func(cmd *cobra.Command) {
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

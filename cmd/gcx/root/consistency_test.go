package root_test

import (
	"regexp"
	"strings"
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
		agent.AnnotationAvailability:       true,
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

// TestConsistency_CloudOnlyPathsResolveToCommands verifies every path in the
// Grafana Cloud-only registry matches an actual command in the tree, catching
// renamed or removed command groups.
func TestConsistency_CloudOnlyPathsResolveToCommands(t *testing.T) {
	rootCmd := buildRootCmd()

	paths := make(map[string]bool)
	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		paths[cmd.CommandPath()] = true
	})

	for _, cloudPath := range agent.CloudOnlyPaths() {
		t.Run(cloudPath, func(t *testing.T) {
			if !paths[cloudPath] {
				t.Errorf("cloud-only path %q does not match any command in the tree", cloudPath)
			}
		})
	}
}

// TestConsistency_AvailabilityValuesValid verifies the availability annotation
// only ever carries the single supported value.
func TestConsistency_AvailabilityValuesValid(t *testing.T) {
	rootCmd := buildRootCmd()

	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		val := cmd.Annotations[agent.AnnotationAvailability]
		if val == "" {
			return
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			if val != agent.AvailabilityCloudOnly {
				t.Errorf("invalid availability %q, want %q", val, agent.AvailabilityCloudOnly)
			}
		})
	})
}

// TestConsistency_PublicDashboardsHintsMatchCommandArity guards against the
// class of bug reported on PR #924: an LLMHint whose leading positional
// tokens don't match the command's actual cobra.Args arity, or whose flag
// tokens omit a flag the command marks required. Either causes an agent
// following the hint to hit a hard cobra error on the first call.
func TestConsistency_PublicDashboardsHintsMatchCommandArity(t *testing.T) {
	rootCmd := buildRootCmd()

	cmdsByPath := make(map[string]*cobra.Command)
	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		cmdsByPath[cmd.CommandPath()] = cmd
	})

	cases := []struct {
		path        string
		positionals int      // leading hint tokens before the first flag
		wantFlags   []string // flags the hint must reference
	}{
		{path: "gcx public-dashboards get", positionals: 1},
		{path: "gcx public-dashboards create", positionals: 0, wantFlags: []string{"--dashboard-uid", "-f"}},
		{path: "gcx public-dashboards update", positionals: 1, wantFlags: []string{"--dashboard-uid", "-f"}},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			cmd, ok := cmdsByPath[tc.path]
			if !ok {
				t.Fatalf("command %q not found in tree", tc.path)
			}
			hint := cmd.Annotations[agent.AnnotationLLMHint]
			if hint == "" {
				t.Fatalf("missing %s annotation", agent.AnnotationLLMHint)
			}

			positionals := 0
			for tok := range strings.FieldsSeq(hint) {
				if strings.HasPrefix(tok, "-") {
					break
				}
				positionals++
			}
			if positionals != tc.positionals {
				t.Errorf("hint %q implies %d positional arg(s), want %d", hint, positionals, tc.positionals)
			}

			// Validate the implied positional count against the command's own
			// arity check (e.g. cobra.ExactArgs), not just the fixture above.
			// A nil Args means cobra falls back to ArbitraryArgs (no check).
			if cmd.Args != nil {
				dummyArgs := make([]string, positionals)
				for i := range dummyArgs {
					dummyArgs[i] = "x"
				}
				if err := cmd.Args(cmd, dummyArgs); err != nil {
					t.Errorf("hint implies %d positional arg(s) but the command rejects them: %v", positionals, err)
				}
			}

			for _, flag := range tc.wantFlags {
				if !strings.Contains(hint, flag) {
					t.Errorf("hint %q does not reference required flag %q", hint, flag)
				}
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

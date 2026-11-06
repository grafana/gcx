package irm_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
)

// TestOpModelStructuralConformance asserts the structural shape of the
// experimental opmodel/shares trees: every leaf declares an Args validator
// and the experimental stability annotation; entity mutations carry the full
// actor flag set and take <container> first, while authenticated updates and reads carry
// no caller-supplied actor flags. Catches a new verb silently dropping the
// pattern.
func TestOpModelStructuralConformance(t *testing.T) {
	t.Parallel()

	mutationLeaves := map[string]bool{
		"containers update-observation":            true,
		"containers update-action":                 true,
		"containers update-claim-recovery":         true,
		"containers create-observation":            true,
		"containers promote-observation":           true,
		"containers promote-observation-to-action": true,
		"containers verify-observation":            true,
		"containers create-claim":                  true,
		"containers assess-claim":                  true,
		"containers create-action":                 true,
		"containers coordinate-action":             true,
		"containers attach-evidence-link":          true,
		"containers verify-evidence-link":          true,
		"containers withdraw-evidence-link":        true,
		"containers update-entity-text":            true,
	}
	readLeaves := map[string]bool{
		"containers get":         true,
		"containers list-events": true,
	}
	compositeLeaves := map[string]bool{
		"publish": true,
	}

	seen := map[string]bool{}
	for _, root := range []*cobra.Command{irm.NewTestOpModelCmd(), irm.NewTestSharesCmd()} {
		visitRootLeaves(root, func(path string, leaf *cobra.Command) {
			seen[path] = true
			t.Run(root.Name()+"/"+path, func(t *testing.T) {
				t.Parallel()
				if leaf.Args == nil {
					t.Error("every leaf must declare an Args validator")
				}
				if leaf.Annotations["agent.stability"] != "experimental" {
					t.Error("every opmodel-family leaf must be marked experimental")
				}
				if leaf.Flags().Lookup("output") == nil {
					t.Error("every leaf must route output through the codec system")
				}

				switch {
				case mutationLeaves[path]:
					if args := strings.Fields(leaf.Use); len(args) < 2 || args[1] != "<container>" {
						t.Errorf("mutation leaf must take <container> as its first argument, got %q", leaf.Use)
					}
					for _, flag := range []string{"actor-kind", "actor-id", "actor-name"} {
						if leaf.Flags().Lookup(flag) == nil {
							t.Errorf("mutation leaf missing --%s", flag)
						}
					}
				case readLeaves[path]:
					for _, flag := range []string{"actor-kind", "actor-id", "actor-name"} {
						if leaf.Flags().Lookup(flag) != nil {
							t.Errorf("read leaf must not expose --%s", flag)
						}
					}
				case compositeLeaves[path]:
					flags := []string{"drafted-by-agent", "drafted-by-agent-name"}
					if path == "publish" {
						flags = []string{"filename", "allow-uncurated", "agent-id", "agent-name"}
					}
					for _, flag := range flags {
						if leaf.Flags().Lookup(flag) == nil {
							t.Errorf("%s leaf missing --%s", path, flag)
						}
					}
					for _, flag := range []string{"actor-kind", "actor-id", "actor-name", "human"} {
						if leaf.Flags().Lookup(flag) != nil {
							t.Errorf("%s leaf must not expose --%s", path, flag)
						}
					}
				default:
					t.Errorf("leaf %q is not classified as mutation, read, or composite; classify it", path)
				}
			})
		})
	}

	for path := range mutationLeaves {
		if !seen[path] {
			t.Errorf("mutation leaf %q missing from the tree", path)
		}
	}
	for path := range readLeaves {
		if !seen[path] {
			t.Errorf("read leaf %q missing from the tree", path)
		}
	}
	for path := range compositeLeaves {
		if !seen[path] {
			t.Errorf("composite leaf %q missing from the tree", path)
		}
	}
}

// visitRootLeaves visits every leaf under root with its path relative to
// root (noun + verb for nested groups, bare verb for direct children). The
// root group's own name is not part of the path.
func visitRootLeaves(root *cobra.Command, visit func(path string, leaf *cobra.Command)) {
	for _, child := range root.Commands() {
		walkLeaves(child, "", visit)
	}
}

func walkLeaves(cmd *cobra.Command, prefix string, visit func(path string, leaf *cobra.Command)) {
	path := strings.Fields(cmd.Use)[0]
	if prefix != "" {
		path = prefix + " " + path
	}
	children := cmd.Commands()
	if len(children) == 0 {
		visit(path, cmd)
		return
	}
	for _, child := range children {
		walkLeaves(child, path, visit)
	}
}

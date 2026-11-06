package irm_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
)

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
		"containers upsert-model":                  true,
	}
	readLeaves := map[string]bool{
		"containers get":         true,
		"containers list-events": true,
	}

	seen := map[string]bool{}
	visitRootLeaves(irm.NewTestOpModelCmd(), func(path string, leaf *cobra.Command) {
		seen[path] = true
		t.Run(path, func(t *testing.T) {
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
			default:
				t.Errorf("leaf %q is not classified as mutation or read; classify it", path)
			}
		})
	})

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
}

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

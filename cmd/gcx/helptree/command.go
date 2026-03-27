package helptree

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// Command returns the "help-tree" command that renders a compact text tree
// of the CLI hierarchy, optimized for agent context injection.
func Command(root *cobra.Command) *cobra.Command {
	var depth int

	cmd := &cobra.Command{
		Use:   "help-tree [COMMAND]",
		Short: "Print a compact command tree for agent context injection",
		Long: `Outputs a token-efficient text tree of the CLI hierarchy with inline args,
flags, and agent hints. Designed for injecting into agent context windows.

Use a positional argument to show only a subtree (e.g., "gcx help-tree resources").
Use --depth to limit nesting depth.`,
		Args: cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "gcx help-tree resources --depth 2",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := RenderOptions{MaxDepth: depth}

			target := root
			if len(args) > 0 {
				target = findSubtree(root, args[0])
				if target == nil {
					return fmt.Errorf("unknown command: %s", args[0])
				}
			}

			output := RenderTree(target, opts)
			fmt.Fprint(cmd.OutOrStdout(), output)
			return nil
		},
	}

	cmd.Flags().IntVar(&depth, "depth", 0, "Maximum depth of the tree (0 = unlimited)")

	return cmd
}

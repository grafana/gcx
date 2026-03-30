package helptree

import (
	"errors"
	"fmt"
	goio "io"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type helptreeOpts struct {
	IO    cmdio.Options
	Depth int
	root  *cobra.Command
}

func (o *helptreeOpts) setup(flags *pflag.FlagSet) {
	flags.IntVar(&o.Depth, "depth", 0, "Maximum nesting depth (1 = root + direct children, 0 = unlimited)")
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &treeTextCodec{opts: o})
	o.IO.BindFlags(flags)
}

func (o *helptreeOpts) Validate() error {
	return o.IO.Validate()
}

// treeNode is the structured representation for JSON/YAML output.
type treeNode struct {
	Name     string      `json:"name"`
	Short    string      `json:"short,omitempty"`
	Args     string      `json:"args,omitempty"`
	Hint     string      `json:"hint,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
}

func buildTreeNode(cmd *cobra.Command, depth int, opts RenderOptions) *treeNode {
	node := &treeNode{
		Name:  cmd.Name(),
		Short: cmd.Short,
		Args:  extractArgs(cmd.Use),
	}
	if hint := cmd.Annotations[agent.AnnotationLLMHint]; hint != "" {
		node.Hint = hint
	}
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return node
	}
	for _, sub := range cmd.Commands() {
		if !sub.Hidden {
			node.Children = append(node.Children, buildTreeNode(sub, depth+1, opts))
		}
	}
	return node
}

// treeTextCodec renders the tree as compact indented text.
type treeTextCodec struct {
	opts *helptreeOpts
}

func (c *treeTextCodec) Format() format.Format { return "text" }

func (c *treeTextCodec) Encode(output goio.Writer, _ any) error {
	renderOpts := RenderOptions{MaxDepth: c.opts.Depth}
	text := RenderTree(c.opts.root, renderOpts)
	_, err := fmt.Fprint(output, text)
	return err
}

func (c *treeTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("help-tree text codec does not support decoding")
}

// Command returns the "help-tree" command that renders a compact text tree
// of the CLI hierarchy, optimized for agent context injection.
func Command(root *cobra.Command) *cobra.Command {
	opts := &helptreeOpts{root: root}

	cmd := &cobra.Command{
		Use:   "help-tree [COMMAND...]",
		Short: "Print a compact command tree for agent context injection",
		Long: `Outputs a token-efficient text tree of the CLI hierarchy with inline args,
flags, and agent hints. Designed for injecting into agent context windows.

Use positional arguments to show only a subtree (e.g., "gcx help-tree resources get").
Use --depth to limit nesting depth.`,
		Args: cobra.ArbitraryArgs,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "gcx help-tree resources --depth 2",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			target := root
			if len(args) > 0 {
				path := strings.Join(args, " ")
				target = findSubtree(root, path)
				if target == nil {
					return fmt.Errorf("unknown command: %s", path)
				}
			}
			opts.root = target

			renderOpts := RenderOptions{MaxDepth: opts.Depth}
			node := buildTreeNode(target, 0, renderOpts)
			return opts.IO.Encode(cmd.OutOrStdout(), node)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

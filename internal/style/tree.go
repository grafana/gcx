package style

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// TreeNode represents a resource in a tree hierarchy.
type TreeNode struct {
	Name     string
	Kind     string
	Children []TreeNode
}

// Kind-specific colors for tree rendering.
//
//nolint:gochecknoglobals
var (
	colorFolder    = lipgloss.Color("#EAB839")
	colorDashboard = ColorPrimary      // #6E9FFF
	colorAlert     = GradientBrandFrom // #FF9900
	colorDefault   = ColorMuted        // #7D8085
)

// RenderTree writes an indented tree view to w using unicode connectors.
func RenderTree(w io.Writer, roots []TreeNode) {
	for i, root := range roots {
		isLast := i == len(roots)-1
		renderNode(w, root, "", isLast)
	}
}

func renderNode(w io.Writer, node TreeNode, prefix string, isLast bool) {
	var connector, childPrefix string
	switch {
	case prefix == "":
		// Root-level nodes have no connector prefix.
		connector = ""
		childPrefix = ""
	case isLast:
		connector = prefix + "└── "
		childPrefix = prefix + "    "
	default:
		connector = prefix + "├── "
		childPrefix = prefix + "│   "
	}

	label := formatNodeLabel(node)
	if prefix == "" {
		fmt.Fprintln(w, label)
	} else {
		fmt.Fprintln(w, connector+label)
	}

	for i, child := range node.Children {
		isChildLast := i == len(node.Children)-1
		renderNode(w, child, childPrefix, isChildLast)
	}
}

func formatNodeLabel(node TreeNode) string {
	styled := IsStylingEnabled()
	if !styled {
		return node.Kind + "/" + node.Name
	}

	kindStyle := lipgloss.NewStyle().Foreground(kindColor(node.Kind))
	return kindStyle.Render(node.Kind) + "/" + node.Name
}

func kindColor(kind string) lipgloss.Color {
	switch kind {
	case "Folder":
		return colorFolder
	case "Dashboard":
		return colorDashboard
	case "AlertRule", "AlertGroup":
		return colorAlert
	default:
		return colorDefault
	}
}

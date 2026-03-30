package helptree

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// RenderOptions controls tree rendering behavior.
type RenderOptions struct {
	MaxDepth int // 0 = unlimited
}

// RenderTree renders the command tree as a compact indented text string.
func RenderTree(cmd *cobra.Command, opts RenderOptions) string {
	var buf strings.Builder
	renderNode(&buf, cmd, 0, opts)
	return buf.String()
}

// renderNode writes a single command node and recurses into visible subcommands.
func renderNode(buf *strings.Builder, cmd *cobra.Command, depth int, opts RenderOptions) {
	indent := strings.Repeat("  ", depth)

	// Collect visible subcommands.
	var visibleSubs []*cobra.Command
	for _, sub := range cmd.Commands() {
		if !sub.Hidden {
			visibleSubs = append(visibleSubs, sub)
		}
	}

	isLeaf := len(visibleSubs) == 0

	// Build the line.
	line := indent + cmd.Name()

	if isLeaf {
		// Leaf: show args and flags inline.
		if args := extractArgs(cmd.Use); args != "" {
			line += " " + args
		}
		if flags := formatFlags(cmd); flags != "" {
			line += " " + flags
		}
	} else if cmd.Short != "" {
		// Branch: show description.
		line += " — " + cmd.Short
	}

	// Append hint annotation.
	if hint := cmd.Annotations[agent.AnnotationLLMHint]; hint != "" {
		line += "  # hint: " + hint
	}

	fmt.Fprintln(buf, line)

	// Recurse into subcommands if depth allows.
	// MaxDepth 1 means root + 1 level of children, so stop recursing when depth >= MaxDepth.
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return
	}
	for _, sub := range visibleSubs {
		renderNode(buf, sub, depth+1, opts)
	}
}

// extractArgs extracts the argument pattern from a cobra Use string.
func extractArgs(use string) string {
	_, args, found := strings.Cut(use, " ")
	if !found {
		return ""
	}
	return args
}

// formatFlags formats all non-inherited flags of a command in compact inline notation.
func formatFlags(cmd *cobra.Command) string {
	var parts []string
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		parts = append(parts, formatFlag(f))
	})
	return strings.Join(parts, " ")
}

// formatFlag formats a single flag in compact notation.
//
// Examples:
//
//	-o=(json|yaml)              string with shorthand and enum
//	--on-error=(ignore|fail)    string with enum, no shorthand
//	-n=INT                      int with shorthand
//	--dry-run                   bool
//	-b=STR                      string with shorthand, no enum
//	-v                          count with shorthand
func formatFlag(f *pflag.Flag) string {
	// Name prefix: prefer shorthand for compactness.
	name := "--" + f.Name
	if f.Shorthand != "" {
		name = "-" + f.Shorthand
	}

	// Bool and count flags: just the name.
	switch f.Value.Type() {
	case "bool":
		return name
	case "count":
		return name
	}

	// Check for enum values in the usage string.
	if enum := detectEnum(f.Usage); enum != "" {
		return name + "=" + enum
	}

	// Fall back to type shorthand.
	typeName := typeShorthand(f.Value.Type())
	return name + "=" + typeName
}

var enumPattern = regexp.MustCompile(`(?i)one of[:\s]+([\w-]+(?:\s*,\s*[\w-]+)*)`)

// detectEnum extracts enum values from a flag's usage string.
// Recognizes patterns like "One of: json, yaml, text" and returns "(json|yaml|text)".
// Returns "" if no enum pattern is detected.
func detectEnum(usage string) string {
	matches := enumPattern.FindStringSubmatch(usage)
	if len(matches) < 2 {
		return ""
	}
	parts := strings.Split(matches[1], ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return "(" + strings.Join(parts, "|") + ")"
}

// typeShorthand maps Go/cobra type names to compact display names.
func typeShorthand(typeName string) string {
	switch typeName {
	case "string":
		return "STR"
	case "int", "int32", "int64":
		return "INT"
	case "float32", "float64":
		return "NUM"
	case "duration":
		return "DUR"
	case "stringSlice":
		return "STR,..."
	default:
		return strings.ToUpper(typeName)
	}
}

// findSubtree locates a subcommand by space-separated name path.
func findSubtree(root *cobra.Command, path string) *cobra.Command {
	parts := strings.Fields(path)
	current := root
	for _, name := range parts {
		var found *cobra.Command
		for _, sub := range current.Commands() {
			if sub.Name() == name {
				found = sub
				break
			}
		}
		if found == nil {
			return nil
		}
		current = found
	}
	return current
}

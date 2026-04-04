package fail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/style"
)

// DetailedError is used to describe errors in a human-friendly way.
// It can be used to format errors as follows:
//
//	Error: File not found
//	│
//	│ could not read './cmd/config/testdata/config.yamls'
//	│
//	├─ Details:
//	│
//	│ open ./cmd/config/testdata/config.yamls: no such file or directory
//	│
//	├─ Suggestions:
//	│
//	│ • Check for typos in the command's arguments
//	│
//	├─ Learn more:
//	│
//	│ https://example.com/docs/errors.html#some-error
//	│
//	└─
type DetailedError struct {
	// Summary is a one-liner that briefly describes the error.
	// This field is expected to NOT be empty.
	Summary string

	// Details holds additional information on the error.
	// Optional.
	Details string

	// Parent holds a reference to a parent error.
	// Optional.
	Parent error

	// Suggestions holds list of suggestions related to the error.
	// Optional.
	Suggestions []string

	// DocsLink holds a link to a documentation page related to the error.
	// Optional.
	DocsLink string

	// ExitCode indicates which exit code should be used as a result of this error.
	// If nil, 1 should be used.
	// Optional.
	ExitCode *int
}

func (e DetailedError) Error() string {
	buffer := strings.Builder{}

	styled := style.IsStylingEnabled()

	renderRed := func(s string) string {
		if !styled {
			return s
		}
		return lipgloss.NewStyle().Foreground(style.ColorError).Render(s)
	}
	renderBlue := func(s string) string {
		if !styled {
			return s
		}
		return lipgloss.NewStyle().Foreground(style.ColorPrimary).Render(s)
	}

	// Build the inner content with box-drawing connectors.
	inner := strings.Builder{}
	if styled {
		inner.WriteString(style.Gradient("Error:", style.GradientAccentFrom, style.GradientAccentTo) + " " + e.Summary + "\n")
	} else {
		inner.WriteString(renderRed("Error: ") + e.Summary + "\n")
	}

	if e.Details != "" {
		lines := strings.Split(e.Details, "\n")
		inner.WriteString("│\n")
		for _, line := range lines {
			inner.WriteString("│ " + line + "\n")
		}
	}

	formattedParent := ""
	showParent := e.Parent != nil
	if e.Parent != nil {
		// Will pretty-print YAML-related errors and leave the other ones as-is.
		formattedParent = yaml.FormatError(e.Parent, styled, true)
		showParent = !sameRenderedMessage(e.Details, formattedParent)
	}

	if showParent {
		fmt.Fprintf(&inner, "│\n├─ %s\n│\n", renderBlue("Details:"))
		for line := range strings.SplitSeq(formattedParent, "\n") {
			inner.WriteString("│ " + line + "\n")
		}
	}

	if len(e.Suggestions) != 0 {
		fmt.Fprintf(&inner, "│\n├─ %s\n│\n", renderBlue("Suggestions:"))

		for _, suggestion := range e.Suggestions {
			inner.WriteString("│ • " + suggestion + "\n")
		}
	}

	if e.DocsLink != "" {
		fmt.Fprintf(&inner, "│\n├─ %s\n│\n│ %s\n", renderBlue("Learn more:"), e.DocsLink)
	}

	inner.WriteString("│\n└─")

	// Wrap in a bordered panel when styled.
	if styled {
		panel := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(style.ColorBorder).
			Padding(0, 1)
		buffer.WriteString(panel.Render(inner.String()) + "\n")
	} else {
		buffer.WriteString(inner.String() + "\n")
	}

	return buffer.String()
}

func sameRenderedMessage(details string, parent string) bool {
	normalize := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		return strings.TrimSpace(s)
	}

	normalizedDetails := normalize(details)
	if normalizedDetails == "" {
		return false
	}

	return normalizedDetails == normalize(parent)
}

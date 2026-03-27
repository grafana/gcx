package fail

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/goccy/go-yaml"
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

	red := color.New(color.FgRed).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()

	buffer.WriteString(red("Error: ") + e.Summary + "\n")

	if e.Details != "" {
		lines := strings.Split(e.Details, "\n")
		buffer.WriteString("│\n")
		for _, line := range lines {
			buffer.WriteString("│ " + line + "\n")
		}
	}

	if e.Parent != nil {
		buffer.WriteString(fmt.Sprintf("│\n├─ %s\n│\n", blue("Details:")))

		// Will pretty-print YAML-related errors and leave the other ones as-is.
		formattedErr := yaml.FormatError(e.Parent, !color.NoColor, true)

		for line := range strings.SplitSeq(formattedErr, "\n") {
			buffer.WriteString("│ " + line + "\n")
		}
	}

	if len(e.Suggestions) != 0 {
		buffer.WriteString(fmt.Sprintf("│\n├─ %s\n│\n", blue("Suggestions:")))

		for _, suggestion := range e.Suggestions {
			buffer.WriteString("│ • " + suggestion + "\n")
		}
	}

	if e.DocsLink != "" {
		buffer.WriteString(fmt.Sprintf("│\n├─ %s\n│\n│ %s\n", blue("Learn more:"), e.DocsLink))
	}

	buffer.WriteString("│\n└─\n")

	return buffer.String()
}

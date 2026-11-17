//nolint:wrapcheck
package linter

import (
	"context"
	"fmt"
	"io"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// Reporter formats and publishes linter reports.
type Reporter interface {
	// Publish formats and publishes a report to any appropriate target
	Publish(ctx context.Context, out io.Writer, report Report) error
}

var _ Reporter = (*CompactReporter)(nil)

// CompactReporter reports violations in a compact table.
type CompactReporter struct {
}

// Publish prints a compact report to the configured output.
func (reporter CompactReporter) Publish(_ context.Context, out io.Writer, r Report) error {
	if len(r.Violations) == 0 {
		_, err := fmt.Fprintln(out)

		return err
	}

	table := tablewriter.NewTable(out)
	defer func() { _ = table.Close() }()

	table.Header([]string{"Resource type", "Location", "Severity", "Rule", "Details"})

	for _, violation := range r.Violations {
		err := table.Append([]string{
			violation.ResourceType,
			violation.Location.String(),
			violation.Severity,
			violation.Rule,
			violation.Details,
		})
		if err != nil {
			return err
		}
	}

	summary := fmt.Sprintf("%d %s linted, %d %s found.",
		r.Summary.FilesScanned, pluralize("file", r.Summary.FilesScanned),
		r.Summary.NumViolations, pluralize("violation", r.Summary.NumViolations),
	)

	if err := table.Render(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(out, summary)

	return err
}

var _ Reporter = (*PrettyReporter)(nil)

// PrettyReporter is a Reporter for representing reports as tables.
type PrettyReporter struct {
}

// Publish prints a pretty report to the configured output.
//
//nolint:nestif
func (reporter PrettyReporter) Publish(_ context.Context, out io.Writer, r Report) error {
	if err := printPrettyViolationsTable(out, r.Violations); err != nil {
		return err
	}

	if len(r.Violations) > 0 {
		_, _ = fmt.Fprintln(out)
	}

	numsWarning, numsError := 0, 0

	for _, violation := range r.Violations {
		switch violation.Severity {
		case "warning":
			numsWarning++
		case "error", "fatal":
			numsError++
		}
	}

	footer := fmt.Sprintf("%d %s linted.", r.Summary.FilesScanned, pluralize("file", r.Summary.FilesScanned))

	if r.Summary.NumViolations == 0 {
		footer += " No violations found."
	} else {
		footer += fmt.Sprintf(" %d %s ", r.Summary.NumViolations, pluralize("violation", r.Summary.NumViolations))

		if numsWarning > 0 {
			footer += fmt.Sprintf("(%d %s, %d %s) found",
				numsError, pluralize("error", numsError), numsWarning, pluralize("warning", numsWarning),
			)
		} else {
			footer += "found"
		}

		if r.Summary.FilesScanned > 1 && r.Summary.FilesFailed > 0 {
			footer += fmt.Sprintf(" in %d %s.", r.Summary.FilesFailed, pluralize("file", r.Summary.FilesFailed))
		} else {
			footer += "."
		}
	}

	_, err := fmt.Fprint(out, footer+"\n")

	return err
}

func printPrettyViolationsTable(out io.Writer, violations []Violation) error {
	table := tablewriter.NewTable(
		out,
		tablewriter.WithRendition(tw.Rendition{
			Borders: tw.Border{
				Top:    tw.Off,
				Bottom: tw.Off,
				Left:   tw.Off,
				Right:  tw.Off,
			},
			Symbols: tw.NewSymbolCustom("").WithColumn(""),
		}),
		tablewriter.WithConfig(tablewriter.Config{
			Row: tw.CellConfig{
				Formatting: tw.CellFormatting{AutoWrap: tw.WrapNormal}, // Wrap long content
			},
		}),
	)
	defer func() { _ = table.Close() }()

	numViolations := len(violations)

	yellow := color.New(color.FgYellow).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	for i, violation := range violations {
		description := red(violation.Description)
		if violation.Severity == "warning" {
			description = yellow(violation.Description)
		}

		_ = table.Append([]string{yellow("Rule:"), violation.Rule})

		// if there is no support for color, then we show the level in addition
		// so that the level of the violation is still clear
		if color.NoColor {
			_ = table.Append([]string{"Severity:", violation.Severity})
		}

		_ = table.Append([]string{yellow("Description:"), description})
		_ = table.Append([]string{yellow("Resource type:"), violation.ResourceType})
		_ = table.Append([]string{yellow("Category:"), violation.Category})
		_ = table.Append([]string{yellow("Location:"), cyan(violation.Location.String())})
		_ = table.Append([]string{yellow("Details:"), violation.Details})

		documentation := violation.DocumentationURL()
		if documentation != "" {
			_ = table.Append([]string{yellow("Documentation:"), cyan(violation.DocumentationURL())})
		}

		if i+1 < numViolations {
			_ = table.Append([]string{""})
		}
	}

	return table.Render()
}

func pluralize(singular string, count int) string {
	if count == 1 {
		return singular
	}

	return singular + "s"
}

package resources

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// maxFieldsInTable bounds the field paths shown per row in the text table;
// structured output always carries every field change.
const maxFieldsInTable = 3

type diffOpts struct {
	IO cmdio.Options
}

func (opts *diffOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &diffTextCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)
}

func (opts *diffOpts) Validate() error {
	return opts.IO.Validate()
}

func diffCmd() *cobra.Command {
	opts := &diffOpts{}

	cmd := &cobra.Command{
		Use:   "diff BEFORE AFTER",
		Short: "[experimental] Compare two resource captures of the same stack",
		Long: experimentalObservationPreamble + `

Diff compares two directories written by 'gcx resources capture' and reports
the resources that were added, modified or deleted between them. It works
offline and does not contact Grafana.

Both captures must be finished, unaltered, from the same stack, taken by the
same caller, and use the same normalization version; otherwise the command
fails. Permissions decide what a caller can see, so captures by different
callers are not comparable. Modified resources
list each changed field as a JSON pointer; array order is significant.

A result is reported only where coverage supports it. A resource that is absent
from an incomplete capture is indeterminate, not added or deleted, and a type
outside the scope of either capture, or read with different API versions, is
indeterminate as a whole. The 'complete' field is true only when every type was
compared on complete reads.

Secret values are redacted in captures, so a changed secret value cannot be
observed. A matching result is evidence about observable resource state, not
proof that a workflow succeeded.`,
		Example: `
	# Compare two captures
	gcx resources diff ./before ./after

	# Structured output for agents and evaluation harnesses
	gcx resources diff ./before ./after -o json`,
		Args: cobra.ExactArgs(2),
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "BEFORE AFTER -o json; check .complete and .types before trusting added/deleted results. Added and deleted entries carry the full object; narrow with --jq '.changes[] | {type,name,change,paths: [.fields[]?.path]}'",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			before, err := loadCapture(args[0])
			if err != nil {
				return err
			}
			after, err := loadCapture(args[1])
			if err != nil {
				return err
			}

			result, err := observation.Compare(before, after, args[0], args[1])
			if incompatible, ok := errors.AsType[*observation.IncompatibleError](err); ok {
				return gcxerrors.DetailedError{
					Summary: "Captures are not comparable",
					Details: incompatible.Reason,
					Parent:  err,
				}
			}
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

func loadCapture(path string) (*observation.Capture, error) {
	capture, err := observation.Load(path)
	if invalid, ok := errors.AsType[*observation.InvalidCaptureError](err); ok {
		return nil, gcxerrors.DetailedError{
			Summary: "Invalid capture",
			Details: fmt.Sprintf("%s: %s", invalid.Path, invalid.Reason),
			Parent:  err,
		}
	}
	return capture, err
}

type diffTextCodec struct{}

func (c *diffTextCodec) Format() format.Format { return "text" }

func (c *diffTextCodec) Encode(w io.Writer, data any) error {
	result, ok := data.(*observation.DiffResult)
	if !ok {
		return errors.New("invalid data type for text codec")
	}

	s := result.Summary
	fmt.Fprintf(w, "%d added, %d modified, %d deleted, %d unchanged, %d indeterminate\n",
		s.Added, s.Modified, s.Deleted, s.Unchanged, s.Indeterminate)
	if !result.Complete {
		fmt.Fprintln(w, "The comparison is incomplete: some results cannot be determined.")
	}

	var gaps []observation.TypeComparison
	for _, t := range result.Types {
		if t.Status != observation.TypeCompared {
			gaps = append(gaps, t)
		}
	}
	if len(gaps) > 0 {
		fmt.Fprintln(w)
		t := style.NewTable("TYPE", "STATUS", "REASON")
		for _, gap := range gaps {
			t.Row(gap.Type, gap.Status, strings.Join(gap.Reasons, "; "))
		}
		if err := t.Render(w); err != nil {
			return err
		}
	}

	if len(result.Changes) > 0 {
		fmt.Fprintln(w)
		t := style.NewTable("TYPE", "NAME", "CHANGE", "DETAIL")
		for _, change := range result.Changes {
			t.Row(change.Type, change.Name, change.Change, changeDetail(change))
		}
		if err := t.Render(w); err != nil {
			return err
		}
	}
	return nil
}

func changeDetail(change observation.ResourceChange) string {
	if change.Change == observation.ChangeIndeterminate {
		return change.Reason
	}
	if len(change.Fields) == 0 {
		return ""
	}
	paths := make([]string, 0, maxFieldsInTable)
	for i, field := range change.Fields {
		if i == maxFieldsInTable {
			paths = append(paths, fmt.Sprintf("(+%d more)", len(change.Fields)-maxFieldsInTable))
			break
		}
		paths = append(paths, field.Path)
	}
	return strings.Join(paths, ", ")
}

func (c *diffTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

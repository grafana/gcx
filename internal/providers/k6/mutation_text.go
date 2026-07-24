package k6

// This file holds the human "text" codecs for k6's finite mutation and
// status commands. Each mutation command encodes a cmdio.SingleMutation
// result through the codec system: the default text codec below reproduces
// the exact styled one-liner the command has always printed (byte-identical
// human stdout), while agent mode (agents codec) and explicit -o json/yaml
// get the structured document for free.

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

// successLineCodec is a "text" codec that renders a single styled success
// line (the same rendering cmdio.Success has always produced) for a
// command's structured result value.
type successLineCodec struct {
	render func(v any) (string, error)
}

func (c *successLineCodec) Format() format.Format { return "text" }

func (c *successLineCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

func (c *successLineCodec) Encode(w io.Writer, v any) error {
	msg, err := c.render(v)
	if err != nil {
		return err
	}
	cmdio.Success(w, "%s", msg)
	return nil
}

// singleMutationTextCodec builds the "text" codec for a command whose result
// is a cmdio.SingleMutation: render receives the mutation and returns the
// unstyled message; the codec prints it as the familiar "✔ <message>" line.
func singleMutationTextCodec(render func(m cmdio.SingleMutation) string) *successLineCodec {
	return &successLineCodec{render: func(v any) (string, error) {
		m, ok := v.(cmdio.SingleMutation)
		if !ok {
			return "", fmt.Errorf("invalid data type for text codec: expected SingleMutation, got %T", v)
		}
		return render(m), nil
	}}
}

// testRunStatusTextCodec renders a single TestRunStatus as the exact
// hand-rolled key:value block `gcx k6 test-run status` has always printed.
type testRunStatusTextCodec struct{}

func (c *testRunStatusTextCodec) Format() format.Format { return "text" }

func (c *testRunStatusTextCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

func (c *testRunStatusTextCodec) Encode(w io.Writer, v any) error {
	run, ok := v.(TestRunStatus)
	if !ok {
		return fmt.Errorf("invalid data type for text codec: expected TestRunStatus, got %T", v)
	}
	_, err := fmt.Fprintf(w, "Run ID:  %d\nStatus:  %s\nResult:  %s\nCreated: %s\nEnded:   %s\n",
		run.ID, run.Status, resultStatusString(run.ResultStatus), run.Created, run.Ended)
	return err
}

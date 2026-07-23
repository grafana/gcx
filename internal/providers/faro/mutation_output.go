package faro

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

// successLineCodec is the "text" codec for faro mutation results. It renders
// the exact styled success one-liner the command has always printed (via
// cmdio.Success), so the default human stdout stays byte-identical while
// agent mode and explicit -o json/yaml receive the structured result value.
type successLineCodec struct {
	render func(v any) (string, error)
}

// Format returns the codec's format identifier.
func (c *successLineCodec) Format() format.Format { return "text" }

// Decode is not supported for the text format.
func (c *successLineCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

// Encode renders the success one-liner for the mutation result.
func (c *successLineCodec) Encode(w io.Writer, v any) error {
	line, err := c.render(v)
	if err != nil {
		return err
	}
	cmdio.Success(w, "%s", line)
	return nil
}

// singleMutationLine adapts a SingleMutation-specific message renderer into
// the successLineCodec render signature, with the type assertion done once.
func singleMutationLine(render func(m cmdio.SingleMutation) string) func(v any) (string, error) {
	return func(v any) (string, error) {
		m, ok := v.(cmdio.SingleMutation)
		if !ok {
			return "", fmt.Errorf("invalid data type for text codec: expected SingleMutation, got %T", v)
		}
		return render(m), nil
	}
}

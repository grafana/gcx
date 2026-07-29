package irm

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

// singleMutationTextCodec is the human "text" codec for cmdio.SingleMutation
// results emitted by IRM mutation commands (OnCall CRUD delete, incidents
// close, incidents activity add). Each command supplies a render function
// that reproduces exactly the one-line styled message it has always printed,
// so default human stdout stays byte-identical to the pre-codec output while
// agent mode and explicit -o json/yaml get the structured document.
//
// This codec is for the shared cmdio result family only. The alert-group
// action verbs (oncall_actions.go) keep their own locked single/bulk
// envelopes and do not route through it.
type singleMutationTextCodec struct {
	render func(w io.Writer, m cmdio.SingleMutation)
}

func (c *singleMutationTextCodec) Format() format.Format { return "text" }

func (c *singleMutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *singleMutationTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for text codec: expected SingleMutation")
	}
	c.render(w, m)
	return nil
}

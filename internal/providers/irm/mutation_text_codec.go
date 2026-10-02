package irm

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
)

// mutationResultTextCodec is the human "text" codec for the mutation results
// that IRM commands emit (OnCall CRUD delete, OnCall update-position,
// incidents close, incidents activity add). Each command supplies a render
// function that reproduces exactly the one-line styled message it has always
// printed, so default human stdout stays byte-identical to the pre-codec
// output while agent mode and explicit -o json/yaml get the structured
// document.
//
// The type parameter T names the result type that the command encodes. It is
// cmdio.SingleMutation for the shared result family, or a type that embeds one
// and adds a field. The alert-group action verbs (oncall_actions.go) keep
// their own locked single/bulk envelopes and do not route through this codec.
type mutationResultTextCodec[T any] struct {
	render func(w io.Writer, m T)
}

func (c *mutationResultTextCodec[T]) Format() format.Format { return "text" }

func (c *mutationResultTextCodec[T]) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *mutationResultTextCodec[T]) Encode(w io.Writer, v any) error {
	m, ok := v.(T)
	if !ok {
		var want T
		return fmt.Errorf("invalid data type for text codec: expected %T, got %T", want, v)
	}
	c.render(w, m)
	return nil
}

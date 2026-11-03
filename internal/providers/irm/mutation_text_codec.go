package irm

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
)

// mutationResultTextCodec formats a command's mutation result as human text.
// The render function retains each command's success message.
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

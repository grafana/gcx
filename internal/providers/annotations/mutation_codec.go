package annotations

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

type mutationTextCodec struct {
	line func(cmdio.SingleMutation) string
}

func (c mutationTextCodec) Format() format.Format { return "text" }

func (c mutationTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for text codec: expected SingleMutation")
	}
	cmdio.Success(w, "%s", c.line(m))
	return nil
}

func (mutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func setupMutationOutput(opts *cmdio.Options, flags *pflag.FlagSet, line func(cmdio.SingleMutation) string) {
	opts.RegisterCustomCodec("text", mutationTextCodec{line: line})
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}

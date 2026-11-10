package ml

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

type mutationTextCodec struct{}

func (mutationTextCodec) Format() format.Format { return "text" }
func (mutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}
func (mutationTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(cmdio.SingleMutation)
	if !ok {
		return fmt.Errorf("invalid mutation result type: %T", value)
	}
	if result.DryRun {
		_, err := fmt.Fprintf(w, "Dry run: %s %q; %s\n", result.Target.Kind, result.Target.Name, result.Error)
		return err
	}
	if result.Action == "" {
		return errors.New("mutation action is required")
	}
	_, err := fmt.Fprintf(w, "%s %s %q\n", strings.ToUpper(result.Action[:1])+result.Action[1:], result.Target.Kind, result.Target.Name)
	return err
}

package alert

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

// singleMutationTextCodec is the human "text" codec for the SingleMutation
// results produced by this family's mutation commands (contact-points /
// mute-timings / templates delete, notification-policies set/reset). It
// renders exactly the one styled success line each command has always
// printed, keeping default human stdout byte-identical, while agent mode
// (agents codec) and explicit -o json/yaml get the structured
// cmdio.SingleMutation document.
type singleMutationTextCodec struct {
	// line builds the success message (without the styled prefix) from the
	// encoded mutation.
	line func(m cmdio.SingleMutation) string
}

func (c *singleMutationTextCodec) Format() format.Format { return "text" }

func (c *singleMutationTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for text codec: expected SingleMutation")
	}
	cmdio.Success(w, "%s", c.line(m))
	return nil
}

func (c *singleMutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

// All SingleMutation results in this family leave Changed nil ("cannot
// tell"): Grafana's provisioning API acknowledges deletes and replace-style
// writes without reporting whether server state actually differed, and
// claiming changed=true would overclaim on idempotent re-runs.

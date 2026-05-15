package output

import (
	"io"

	"github.com/grafana/gcx/internal/format"
)

// NewAgentsCodecForTesting exposes the unexported agentsCodec for unit tests.
// Uses os.Stderr as the error writer.
func NewAgentsCodecForTesting() format.Codec {
	return newAgentsCodec(nil)
}

// NewAgentsCodecWithErrWriter exposes the agentsCodec with a custom errWriter for testing.
func NewAgentsCodecWithErrWriter(w io.Writer) format.Codec {
	return newAgentsCodec(w)
}

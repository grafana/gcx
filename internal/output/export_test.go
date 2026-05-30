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

// NewNDJSONCodecForTesting exposes the unexported ndjsonCodec for unit tests.
// Uses os.Stderr as the error writer.
func NewNDJSONCodecForTesting() format.Codec {
	return newNDJSONCodec(nil)
}

// NewNDJSONCodecWithErrWriter exposes the ndjsonCodec with a custom errWriter for testing.
func NewNDJSONCodecWithErrWriter(w io.Writer) format.Codec {
	return newNDJSONCodec(w)
}

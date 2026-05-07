package output

import "github.com/grafana/gcx/internal/format"

// NewAgentsCodecForTesting exposes the unexported agentsCodec for unit tests.
func NewAgentsCodecForTesting() format.Codec {
	return newAgentsCodec()
}

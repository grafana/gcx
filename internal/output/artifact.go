package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
)

// EmitArtifactResult writes the terminal result for an artifact-writing
// command whose -o/--output flag selects the FILE format rather than the
// stdout format (the pull/edit family, which pins its default via
// Options.PinDefaultFormat). Such commands cannot route their terminal
// output through the codec registry — the flag is taken — so this helper is
// the single shared implementation of their terminal protocol:
//
//   - agent mode: the receipt (e.g. ArtifactReceipt) is written to w as one
//     compact JSON document;
//   - otherwise: the human renderer runs, preserving the command's existing
//     human output byte-for-byte.
//
// Do not branch on agent mode in individual artifact commands — route their
// terminal output through this helper so the protocol stays in one place.
func EmitArtifactResult(w io.Writer, receipt any, human func(io.Writer) error) error {
	if agent.IsAgentMode() {
		data, err := json.Marshal(receipt)
		if err != nil {
			return fmt.Errorf("artifact receipt: marshal: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	return human(w)
}

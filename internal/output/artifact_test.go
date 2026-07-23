package output_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitArtifactResult(t *testing.T) {
	receipt := cmdio.NewArtifactReceipt("pulled", "json")
	receipt.Summary = cmdio.MutationSummary{Succeeded: 3}

	t.Run("agent mode writes one JSON document", func(t *testing.T) {
		agent.SetFlag(true)
		t.Cleanup(func() { agent.SetFlag(false) })

		var buf bytes.Buffer
		require.NoError(t, cmdio.EmitArtifactResult(&buf, receipt, func(io.Writer) error {
			t.Fatal("human renderer must not run in agent mode")
			return nil
		}))

		var got map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		assert.Equal(t, "gcx.artifact_receipt", got["type"])
		assert.Equal(t, "json", got["format"])
	})

	t.Run("human mode runs the renderer verbatim", func(t *testing.T) {
		agent.SetFlag(false)
		t.Cleanup(func() { agent.SetFlag(false) })

		var buf bytes.Buffer
		require.NoError(t, cmdio.EmitArtifactResult(&buf, receipt, func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "3 resources pulled, 0 errors")
			return err
		}))
		assert.Equal(t, "3 resources pulled, 0 errors\n", buf.String())
	})
}

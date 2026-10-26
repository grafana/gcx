package ml

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/require"
)

func TestMutationTextCodec(t *testing.T) {
	for _, action := range []string{"created", "updated", "deleted"} {
		t.Run(action, func(t *testing.T) {
			var out bytes.Buffer
			result := cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: "Job", Name: "one"})
			require.NoError(t, mutationTextCodec{}.Encode(&out, result))
			require.Contains(t, out.String(), `Job "one"`)
			require.NotContains(t, out.String(), "schema_version")
		})
	}
}

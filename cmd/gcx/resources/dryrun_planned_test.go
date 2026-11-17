package resources //nolint:testpackage // White-box test for the unexported result builder and text codec.

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
)

func TestBatchMutation_DryRunPlanned(t *testing.T) {
	summary := &remote.OperationSummary{}
	summary.RecordSkipped("not server-verified")
	summary.RecordSkipped("not server-verified")
	summary.RecordPlanned(remote.OperationSuccess{RequestedName: "checkout-42", Kind: "FaroApp", Action: "updated", Name: "checkout-42", SourcePath: "a.json"})
	summary.RecordPlanned(remote.OperationSuccess{RequestedName: "new", Kind: "FaroApp", Action: "created"})

	result := batchMutationFromSummary("pushed", summary, true)
	require.Len(t, result.Planned, 2)
	require.Equal(t, "updated", result.Planned[0].Action)
	require.Equal(t, "checkout-42", result.Planned[0].Target.Name)
	require.Nil(t, result.Successes, "a dry run has no real writes")

	var out bytes.Buffer
	require.NoError(t, (&mutationSummaryCodec{}).Encode(&out, result))
	require.Contains(t, out.String(), "would update FaroApp/checkout-42\n")
	require.Contains(t, out.String(), "would create FaroApp/new\n")
}

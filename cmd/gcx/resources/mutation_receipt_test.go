package resources_test

import (
	"encoding/json"
	"testing"

	resourcescmd "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestBatchMutationEffectiveIdentity(t *testing.T) {
	summary := &remote.OperationSummary{}
	summary.RecordSuccess()
	summary.RecordApplied(remote.OperationSuccess{RequestedName: "requested-id", Action: "created", Kind: "SLO", Name: "server-uuid", UID: "native-uid", Namespace: "stack"})
	result := resourcescmd.BatchMutationForTest("pushed", summary, false)
	require.Len(t, result.Successes, 1)
	require.Equal(t, "server-uuid", result.Successes[0].Target.ID)
	require.Equal(t, "server-uuid", result.Successes[0].Target.Name)
	require.Equal(t, "native-uid", result.Successes[0].Target.UID)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"gcx.mutation_batch","schema_version":"1","action":"pushed","summary":{"succeeded":1,"failed":0},"failures":[],"successes":[{"requested":{"kind":"SLO","name":"requested-id","id":"requested-id"},"action":"created","target":{"kind":"SLO","name":"server-uuid","id":"server-uuid","uid":"native-uid","namespace":"stack"}}]}`, string(data))
	require.Empty(t, resourcescmd.BatchMutationForTest("pushed", summary, true).Successes)
}

func TestPushSuccessesRequireExplicitOption(t *testing.T) {
	summary := &remote.OperationSummary{}
	summary.RecordSuccess()
	summary.RecordApplied(remote.OperationSuccess{RequestedName: "requested", Action: "created", Kind: "SLO", Name: "server-uuid"})
	flags := pflag.NewFlagSet("push", pflag.ContinueOnError)
	opts := resourcescmd.NewPushOptsForTest(flags)
	require.False(t, opts.IncludeSuccesses)
	require.Empty(t, resourcescmd.PushMutationForTest(opts, summary).Successes)
	require.NoError(t, flags.Parse([]string{"--include-successes"}))
	require.True(t, opts.IncludeSuccesses)
	require.Len(t, resourcescmd.PushMutationForTest(opts, summary).Successes, 1)
	opts.DryRun = true
	require.Empty(t, resourcescmd.PushMutationForTest(opts, summary).Successes)
	// The presentation choice does not alter captured identities.
	require.Len(t, summary.Successes(), 1)
}

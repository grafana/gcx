package resources_test

import (
	"encoding/json"
	"testing"

	resourcescmd "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
)

func TestBatchMutationEffectiveIdentity(t *testing.T) {
	summary := &remote.OperationSummary{}
	summary.RecordSuccess()
	summary.RecordApplied(remote.OperationSuccess{RequestedName: "requested-id", Action: "created", Kind: "SLO", Name: "server-uuid", UID: "native-uid", Namespace: "stack"})
	result := resourcescmd.BatchMutationForTest("pushed", summary, false)
	require.Len(t, result.Successes, 1)
	require.Equal(t, "server-uuid", result.Successes[0].Target.Name)
	require.Equal(t, "native-uid", result.Successes[0].Target.UID)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"gcx.mutation_batch","schema_version":"1","action":"pushed","summary":{"succeeded":1,"failed":0},"failures":[],"successes":[{"requested":{"kind":"SLO","name":"requested-id"},"action":"created","target":{"kind":"SLO","name":"server-uuid","uid":"native-uid","namespace":"stack"}}]}`, string(data))
}

package logs //nolint:testpackage // Tests dropRuleUpdatePayload JSON shape for disabled:false.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropRuleUpdatePayload_serializesDisabledFalse(t *testing.T) {
	t.Parallel()
	dr := &DropRule{
		Version: 1,
		Name:    "n",
		Body: DropRuleBodyV1{
			DropRate:       0.1,
			StreamSelector: "{}",
			Levels:         []string{"info"},
		},
		Disabled: false,
	}
	b, err := dropRuleUpdatePayload(dr)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Contains(t, m, "disabled")
	require.Equal(t, false, m["disabled"])
}

func TestDropRuleCreatePayload_serializesDisabledFalse(t *testing.T) {
	t.Parallel()
	dr := &DropRule{
		SegmentID: GlobalDropRuleSegmentID,
		Version:   1,
		Name:      "n",
		Body: DropRuleBodyV1{
			DropRate:       0.1,
			StreamSelector: "{}",
			Levels:         []string{"info"},
		},
		Disabled: false,
	}
	b, err := dropRuleCreatePayload(dr)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Contains(t, m, "disabled")
	require.Equal(t, false, m["disabled"])
}

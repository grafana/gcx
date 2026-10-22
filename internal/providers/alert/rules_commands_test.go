package alert_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestRuleDetailTableCodec_InvalidType(t *testing.T) {
	codec := alert.RuleDetailTable().Codec("table")
	var buf bytes.Buffer
	err := codec.Encode(&buf, []alert.RuleStatus{})
	require.Error(t, err)
}

func TestRuleDetailTableGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule alert.RuleStatus
	}{
		{name: "paused", rule: alert.RuleStatus{UID: "rule-1", Name: "API errors", State: alert.StatePending, Health: "ok", IsPaused: true}},
		{name: "active", rule: alert.RuleStatus{UID: "rule-2", Name: "Database latency", State: alert.StateFiring, Health: "ok"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, alert.RuleDetailTable().Codec("table").Encode(&buf, &tc.rule))
			testutils.Golden(t, "rule_detail_"+tc.name, buf.String())
		})
	}
}

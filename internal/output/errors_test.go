package output_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
)

func TestErrorInBand(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		agentMode, humanOutput, jsonFlag, want bool
	}{
		{name: "human default"},
		{name: "agent default", agentMode: true, want: true},
		{name: "agent human format", agentMode: true, humanOutput: true},
		{name: "json flag", jsonFlag: true, want: true},
		{name: "json flag overrides human format", humanOutput: true, jsonFlag: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })
			assert.Equal(t, tc.want, output.ErrorInBand(tc.humanOutput, tc.jsonFlag))
		})
	}
}

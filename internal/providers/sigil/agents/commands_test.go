package agents_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/sigil/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTableCodec_Encode(t *testing.T) {
	now := time.Date(2026, 4, 2, 18, 0, 0, 0, time.UTC)

	items := []agents.Agent{
		{AgentName: "my-agent", VersionCount: 3, GenerationCount: 100, ToolCount: 5, LatestSeenAt: now,
			TokenEstimate: agents.TokenEstimate{Total: 654}},
		{AgentName: "other-agent", VersionCount: 1, GenerationCount: 10, ToolCount: 0, LatestSeenAt: time.Time{}},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"NAME", "VERSIONS", "GENERATIONS", "TOOLS", "LAST SEEN", "my-agent", "3", "100", "5", "2026-04-02 18:00"},
		},
		{
			name: "wide includes TOKENS and FIRST SEEN",
			wide: true,
			want: []string{"TOKENS", "FIRST SEEN", "654"},
		},
		{
			name: "zero time shows dash",
			wide: false,
			want: []string{"other-agent", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &agents.ListTableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestListTableCodec_WrongType(t *testing.T) {
	codec := &agents.ListTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Agent")
}

func TestListTableCodec_Format(t *testing.T) {
	tests := []struct {
		wide   bool
		expect string
	}{
		{false, "table"},
		{true, "wide"},
	}
	for _, tc := range tests {
		codec := &agents.ListTableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestListTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &agents.ListTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}

func TestVersionsTableCodec_Encode(t *testing.T) {
	versions := []agents.AgentVersion{
		{EffectiveVersion: "sha256:abcdef1234567890", GenerationCount: 50, ToolCount: 2,
			TokenEstimate: agents.TokenEstimate{Total: 100},
			FirstSeenAt:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			LastSeenAt:    time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
	}

	codec := &agents.VersionsTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, versions))

	output := buf.String()
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "sha256:abcdef12...") // truncated at 15 chars
	assert.NotContains(t, output, "1234567890")      // full hash not shown
	assert.Contains(t, output, "50")
	assert.Contains(t, output, "2026-03-01 00:00")
}

func TestVersionsTableCodec_WrongType(t *testing.T) {
	codec := &agents.VersionsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []AgentVersion")
}

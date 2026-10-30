package agents_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/agents"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestListTableGolden(t *testing.T) {
	var row agents.Agent
	require.NoError(t, json.Unmarshal([]byte(`{"agent_name": "assistant", "version_count": 3, "generation_count": 123, "tool_count": 4, "token_estimate": {"total": 321}, "first_seen_at": "2026-09-01T12:30:00Z", "latest_seen_at": "2026-09-02T13:45:00Z"}`), &row))
	testutils.AssertTableGolden(t, agents.ListTable(), row, "table", "wide")
}

func TestVersionsTableGolden(t *testing.T) {
	var row agents.AgentVersion
	require.NoError(t, json.Unmarshal([]byte(`{"effective_version": "v1", "generation_count": 15, "tool_count": 2, "token_estimate": {"total": 95}, "first_seen_at": "2026-09-01T12:30:00Z", "last_seen_at": "2026-09-02T13:45:00Z"}`), &row))
	testutils.AssertTableGolden(t, agents.VersionsTable(), row, "table")
}

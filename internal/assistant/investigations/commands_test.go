package investigations_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTableCodec_Encode(t *testing.T) {
	summaries := []investigations.InvestigationSummary{
		{
			ID:        "inv-1",
			Title:     "High CPU investigation",
			State:     "running",
			Source:    &investigations.Source{UserID: "admin"},
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			ID:    "inv-2",
			Title: "",
			State: "completed",
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, summaries))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "TITLE")
		assert.Contains(t, out, "STATUS")
		assert.Contains(t, out, "UPDATED")
		assert.NotContains(t, out, "CREATED BY")
		assert.Contains(t, out, "inv-1")
		assert.Contains(t, out, "High CPU investigation")
		assert.Contains(t, out, "-") // empty title
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.ListTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, summaries))
		out := buf.String()
		assert.Contains(t, out, "CREATED BY")
		assert.Contains(t, out, "CREATED")
		assert.Contains(t, out, "admin")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []InvestigationSummary")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

func TestListTableCodec_TitleTruncation(t *testing.T) {
	summaries := []investigations.InvestigationSummary{
		{
			ID:    "inv-1",
			Title: "This is a very long title that should be truncated at forty characters",
			State: "running",
		},
	}

	var buf bytes.Buffer
	codec := &investigations.ListTableCodec{}
	require.NoError(t, codec.Encode(&buf, summaries))
	assert.Contains(t, buf.String(), "...")
}

func TestTodosTableCodec_Encode(t *testing.T) {
	todos := []investigations.Todo{
		{ID: "t-1", Title: "Check alerts", Status: "completed", Assignee: "agent"},
		{ID: "t-2", Title: "Analyze logs", Status: "in_progress"},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.TodosTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, todos))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "TITLE")
		assert.Contains(t, out, "STATUS")
		assert.NotContains(t, out, "ASSIGNEE")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.TodosTableCodec{Wide: true}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, todos))
		out := buf.String()
		assert.Contains(t, out, "ASSIGNEE")
		assert.Contains(t, out, "agent")
		assert.Contains(t, out, "-") // empty assignee
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.TodosTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
	})
}

func TestTimelineTableCodec_Encode(t *testing.T) {
	agents := []investigations.TimelineAgent{
		{
			AgentID:      "a-1",
			AgentName:    "investigation_lead",
			Status:       "completed",
			MessageCount: 15,
			StartTime:    1700000000000,
			LastActivity: 1700000300000,
		},
		{
			AgentID:      "a-2",
			AgentName:    "prometheus_specialist",
			Status:       "in_progress",
			MessageCount: 3,
			StartTime:    1700000100000,
			LastActivity: 1700000200000,
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.TimelineTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, agents))
		out := buf.String()
		assert.Contains(t, out, "AGENT ID")
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "STATUS")
		assert.Contains(t, out, "MESSAGES")
		assert.NotContains(t, out, "LAST ACTIVITY")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.TimelineTableCodec{Wide: true}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, agents))
		out := buf.String()
		assert.Contains(t, out, "LAST ACTIVITY")
		assert.Contains(t, out, "STARTED")
		assert.Contains(t, out, "investigation_lead")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.TimelineTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, 42)
		require.Error(t, err)
	})
}

func TestApprovalsTableCodec_Encode(t *testing.T) {
	approvals := []investigations.Approval{
		{
			ID:        "a-1",
			Status:    "pending",
			Approver:  "user@grafana.com",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:     "a-2",
			Status: "approved",
		},
	}

	codec := &investigations.ApprovalsTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, approvals))
	out := buf.String()
	assert.Contains(t, out, "ID")
	assert.Contains(t, out, "STATUS")
	assert.Contains(t, out, "APPROVER")
	assert.Contains(t, out, "CREATED")
	assert.Contains(t, out, "user@grafana.com")
	assert.Contains(t, out, "-") // empty approver

	t.Run("wrong type", func(t *testing.T) {
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
	})
}

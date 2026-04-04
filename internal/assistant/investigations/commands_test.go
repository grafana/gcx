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
			Status:    "running",
			CreatedBy: "admin",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			ID:     "inv-2",
			Title:  "",
			Status: "completed",
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
			ID:     "inv-1",
			Title:  "This is a very long title that should be truncated at forty characters",
			Status: "running",
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
	entries := []investigations.TimelineEntry{
		{
			Timestamp: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			Type:      "task_started",
			Summary:   "Started checking alerts",
			Actor:     "agent",
		},
		{
			Timestamp: time.Date(2026, 4, 1, 10, 5, 0, 0, time.UTC),
			Type:      "task_completed",
			Summary:   "Finished alert check",
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.TimelineTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, entries))
		out := buf.String()
		assert.Contains(t, out, "TIMESTAMP")
		assert.Contains(t, out, "TYPE")
		assert.Contains(t, out, "SUMMARY")
		assert.NotContains(t, out, "ACTOR")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.TimelineTableCodec{Wide: true}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, entries))
		out := buf.String()
		assert.Contains(t, out, "ACTOR")
		assert.Contains(t, out, "agent")
		assert.Contains(t, out, "-") // empty actor
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

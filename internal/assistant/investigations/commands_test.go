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
		assert.Contains(t, err.Error(), "expected []InvestigationSummary or *LodestoneList")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

func TestListTableCodec_EncodeLodestone(t *testing.T) {
	list := &investigations.LodestoneList{
		Investigations: []investigations.LodestoneInvestigationSummary{
			{
				ID:        "inv-1",
				Title:     "High CPU investigation",
				State:     "in_progress",
				ChatID:    "chat-1",
				Source:    &investigations.LodestoneSource{UserID: "admin"},
				CreatedAt: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			},
			{
				ID:          "inv-2",
				State:       "completed",
				OwnerUserID: "owner-2",
			},
		},
		Total: 2,
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.ListTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, list))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "STATUS")
		assert.NotContains(t, out, "CREATED BY")
		assert.Contains(t, out, "inv-1")
		assert.Contains(t, out, "High CPU investigation")
		assert.Contains(t, out, "inv-2")
	})

	t.Run("wide falls back to owner when source is absent", func(t *testing.T) {
		codec := &investigations.ListTableCodec{Wide: true}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, list))
		out := buf.String()
		assert.Contains(t, out, "CREATED BY")
		assert.Contains(t, out, "admin")   // inv-1: source.userId wins
		assert.Contains(t, out, "owner-2") // inv-2: ownerUserId fallback
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

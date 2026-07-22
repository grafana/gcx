package investigations_test

import (
	"bytes"
	"strings"
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

func TestProfilesTableCodec_Encode(t *testing.T) {
	profiles := &investigations.LodestoneProfiles{
		Profiles: []investigations.LodestoneProfile{
			{
				ID:          "default",
				DisplayName: "Default",
				Description: "Standard investigation profile",
				Default:     true,
				MaxSteps:    30,
				ToolNames:   []string{"prometheus_query_handler", "search_skills"},
				Hash:        "abc123",
			},
			{ID: "deep-dive", DisplayName: "Deep dive", MaxSteps: 80},
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, profiles))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "DEFAULT")
		assert.Contains(t, out, "MAX STEPS")
		assert.Contains(t, out, "TOOLS")
		assert.NotContains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "default")
		assert.Contains(t, out, "true")
		assert.Contains(t, out, "30")
		assert.Contains(t, out, "2") // tool count
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, profiles))
		out := buf.String()
		assert.Contains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "Standard investigation profile")
		// The empty description renders as "-" in the trailing DESCRIPTION
		// column of the deep-dive row.
		var deepDiveRow string
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "deep-dive") {
				deepDiveRow = line
			}
		}
		require.NotEmpty(t, deepDiveRow)
		assert.True(t, strings.HasSuffix(strings.TrimRight(deepDiveRow, " "), "-"), "row: %q", deepDiveRow)
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected *LodestoneProfiles")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

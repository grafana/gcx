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

func TestEvidenceTableCodec_Encode(t *testing.T) {
	resp := &investigations.EvidenceResponse{
		Evidence: []investigations.EvidenceItem{
			{
				PanelID:   "p3",
				Tool:      "prometheus",
				Query:     "rate(http_requests_total{job=\"api\",cluster=\"prod\"}[5m])",
				Epoch:     2,
				Time:      "2026-07-20T10:00:00Z",
				ToolUseID: "toolu_1",
			},
			{PanelID: "p4", Tool: "loki", Query: "short", Epoch: 3, Time: "2026-07-20T10:05:00Z"},
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, resp))
		out := buf.String()
		assert.Contains(t, out, "PANEL")
		assert.Contains(t, out, "TOOL")
		assert.Contains(t, out, "QUERY")
		assert.Contains(t, out, "EPOCH")
		assert.Contains(t, out, "TIME")
		assert.NotContains(t, out, "TOOL USE ID")
		assert.NotContains(t, out, "toolu_1")
		assert.Contains(t, out, "p3")
		assert.Contains(t, out, "prometheus")
		// Long queries are truncated at 40 runes in the default table.
		assert.Contains(t, out, "...")
		assert.NotContains(t, out, "cluster=\"prod\"")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, resp))
		out := buf.String()
		assert.Contains(t, out, "TOOL USE ID")
		assert.Contains(t, out, "toolu_1")
		// Wide shows the full query and "-" for a missing tool use ID.
		assert.Contains(t, out, "rate(http_requests_total{job=\"api\",cluster=\"prod\"}[5m])")
		assert.Regexp(t, `(?m)^p4\s+loki\s+short\s+3\s+2026-07-20T10:05:00Z\s+-$`, out)
	})

	t.Run("multi-line query flattened", func(t *testing.T) {
		multiline := &investigations.EvidenceResponse{
			Evidence: []investigations.EvidenceItem{
				{PanelID: "p1", Tool: "prometheus", Query: "sum by (pod) (\n\trate(errors_total[5m])\n)", Epoch: 1, Time: "2026-07-20T10:00:00Z"},
			},
		}
		for _, codec := range []*investigations.EvidenceTableCodec{{}, {Wide: true}} {
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, multiline))
			out := buf.String()
			// Header + one row: embedded newlines/tabs must not split the row.
			assert.Len(t, strings.Split(strings.TrimRight(out, "\n"), "\n"), 2)
			if codec.Wide {
				assert.Contains(t, out, "sum by (pod) ( rate(errors_total[5m]) )")
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, &investigations.EvidenceResponse{Evidence: []investigations.EvidenceItem{}}))
		assert.Contains(t, buf.String(), "PANEL")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected *EvidenceResponse")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.EvidenceTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

package query_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatFrameTable_Basic(t *testing.T) {
	fields := []query.FrameField{
		{Name: "a", Type: "number"},
		{Name: "b", Type: "string"},
	}
	values := [][]any{
		{float64(1), float64(2)},
		{"x", "y"},
	}

	var buf bytes.Buffer
	require.NoError(t, query.FormatFrameTable(&buf, fields, values))

	out := buf.String()
	assert.Contains(t, out, "a")
	assert.Contains(t, out, "b")
	assert.Contains(t, out, "1")
	assert.Contains(t, out, "x")
	assert.Contains(t, out, "y")
}

func TestFormatFrameTable_TimeFieldFormatsAsRFC3339(t *testing.T) {
	fields := []query.FrameField{
		{Name: "timestamp", Type: "time"},
		{Name: "value", Type: "number"},
	}
	// 1700000000000 ms = 2023-11-14T22:13:20Z
	values := [][]any{
		{float64(1700000000000)},
		{float64(42)},
	}

	var buf bytes.Buffer
	require.NoError(t, query.FormatFrameTable(&buf, fields, values))
	assert.Contains(t, buf.String(), "2023-11-14T22:13:20Z")
}

func TestFormatFrameTable_RaggedColumns(t *testing.T) {
	fields := []query.FrameField{
		{Name: "a", Type: "number"},
		{Name: "b", Type: "string"},
	}
	values := [][]any{
		{float64(1), float64(2), float64(3)},
		{"x"},
	}

	var buf bytes.Buffer
	require.NoError(t, query.FormatFrameTable(&buf, fields, values))
	out := buf.String()
	for _, want := range []string{"1", "2", "3", "x"} {
		assert.Contains(t, out, want, "expected %q in output", want)
	}
}

func TestFormatFrameTable_NoRows(t *testing.T) {
	fields := []query.FrameField{{Name: "a", Type: "number"}}
	var buf bytes.Buffer
	require.NoError(t, query.FormatFrameTable(&buf, fields, nil))
	assert.Contains(t, buf.String(), "a")
}

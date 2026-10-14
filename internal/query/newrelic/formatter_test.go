package newrelic_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func formattedFrames(t *testing.T, resp *newrelic.QueryResponse) map[string]string {
	t.Helper()
	var table, wide bytes.Buffer
	require.NoError(t, newrelic.FormatTable(&table, resp))
	require.NoError(t, newrelic.FormatWide(&wide, resp))
	return map[string]string{"table": table.String(), "wide": wide.String()}
}

func TestFormatRowsByRecord(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	fields := []dataframe.Field{
		{Name: "count", Type: "number", Labels: nil, Config: nil},
		{Name: "status", Type: "string", Labels: nil, Config: nil},
	}
	resp := &newrelic.QueryResponse{Frames: []dataframe.Frame{{
		Schema: dataframe.Schema{RefId: "", Name: "transactions", Meta: nil, Fields: fields},
		Data:   dataframe.Data{Values: [][]any{{42, 43}, {"ok", "failed"}}, Nanos: nil},
	}}}
	for name, out := range formattedFrames(t, resp) {
		t.Run(name, func(t *testing.T) {
			assert.Regexp(t, `(?m)^COUNT\s+STATUS$`, out)
			assert.Regexp(t, `(?m)^42\s+ok$`, out)
			assert.Regexp(t, `(?m)^43\s+failed$`, out)
		})
	}
}

func TestFormatRaggedUnnamedFrames(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	resp := &newrelic.QueryResponse{Frames: []dataframe.Frame{
		{Schema: dataframe.Schema{Fields: []dataframe.Field{{Name: "value"}, {Name: "short"}, {Name: "missing"}}}, Data: dataframe.Data{Values: [][]any{{42, 43}, {"first"}}}},
		{Schema: dataframe.Schema{Fields: []dataframe.Field{{Name: "value"}}}, Data: dataframe.Data{Values: [][]any{{"second"}}}},
	}}
	for name, out := range formattedFrames(t, resp) {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, out, "Frame 1")
			assert.Contains(t, out, "Frame 2")
			assert.Regexp(t, `(?m)^42\s+first`, out)
			assert.Regexp(t, `(?m)^43\s*$`, out)
			assert.Contains(t, out, "second")
		})
	}
}

func TestFormatNumericAndTimeFields(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	resp := &newrelic.QueryResponse{Frames: []dataframe.Frame{{
		Schema: dataframe.Schema{Fields: []dataframe.Field{
			{Name: "count", Type: "number"},
			{Name: "time", Type: "time"},
			{Name: "ratio", Type: "number"},
		}},
		Data: dataframe.Data{Values: [][]any{{float64(1234567)}, {float64(1778976000000)}, {float64(0.25)}}},
	}}}
	for name, out := range formattedFrames(t, resp) {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, out, "1234567")
			assert.Contains(t, out, "2026-05-17T00:00:00Z")
			assert.Contains(t, out, "0.25")
			assert.NotContains(t, out, "e+")
		})
	}
}

func TestFormatEmpty(t *testing.T) {
	for _, format := range []func(*bytes.Buffer, *newrelic.QueryResponse) error{
		func(b *bytes.Buffer, r *newrelic.QueryResponse) error { return newrelic.FormatTable(b, r) },
		func(b *bytes.Buffer, r *newrelic.QueryResponse) error { return newrelic.FormatWide(b, r) },
	} {
		var out bytes.Buffer
		require.NoError(t, format(&out, &newrelic.QueryResponse{}))
		assert.Contains(t, out.String(), "No data")
	}
}

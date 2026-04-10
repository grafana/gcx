package loki_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertGrafanaResponse_NewFieldNames(t *testing.T) {
	// Grafana Loki plugin returns "Time" and "Line", not "timestamp" and "body".
	// Confirmed against live thirdact context.
	resp := loki.ConvertGrafanaResponse(&loki.GrafanaQueryResponse{
		Results: map[string]loki.GrafanaResult{
			"A": {
				Frames: []loki.DataFrame{
					{
						Schema: loki.DataFrameSchema{
							Fields: []loki.Field{
								{Name: "labels", Type: "other"},
								{Name: "Time", Type: "time"},
								{Name: "Line", Type: "string"},
								{Name: "tsNs", Type: "string"},
								{Name: "id", Type: "string"},
							},
						},
						Data: loki.DataFrameData{
							Values: [][]any{
								{map[string]any{"job": "grafana", "service_name": "my-svc"}},
								{float64(1711893600000)},
								{"level=info msg=\"HTTP request\" status=200"},
								{"1711893600000000000"},
								{"1711893600000000000_abc123"},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, resp.Data.Result, 1)
	entry := resp.Data.Result[0]

	// Stream labels must be populated, not null
	assert.Equal(t, map[string]string{"job": "grafana", "service_name": "my-svc"}, entry.Stream)

	// Values must contain actual log content, not ID hashes
	require.Len(t, entry.Values, 1)
	assert.Equal(t, "level=info msg=\"HTTP request\" status=200", entry.Values[0][1])
}

func TestConvertGrafanaResponse_OldFieldNames(t *testing.T) {
	// Backward compat: "timestamp", "body", "labels" field names.
	resp := loki.ConvertGrafanaResponse(&loki.GrafanaQueryResponse{
		Results: map[string]loki.GrafanaResult{
			"A": {
				Frames: []loki.DataFrame{
					{
						Schema: loki.DataFrameSchema{
							Fields: []loki.Field{
								{Name: "labels", Type: "other"},
								{Name: "timestamp", Type: "time"},
								{Name: "body", Type: "string"},
							},
						},
						Data: loki.DataFrameData{
							Values: [][]any{
								{map[string]any{"app": "test"}},
								{float64(1711893600000)},
								{"hello world"},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, resp.Data.Result, 1)
	assert.Equal(t, map[string]string{"app": "test"}, resp.Data.Result[0].Stream)
	assert.Equal(t, "hello world", resp.Data.Result[0].Values[0][1])
}

func TestConvertGrafanaResponse_MultipleStreams(t *testing.T) {
	// Two log entries with different labels should produce two stream entries.
	resp := loki.ConvertGrafanaResponse(&loki.GrafanaQueryResponse{
		Results: map[string]loki.GrafanaResult{
			"A": {
				Frames: []loki.DataFrame{
					{
						Schema: loki.DataFrameSchema{
							Fields: []loki.Field{
								{Name: "labels", Type: "other"},
								{Name: "Time", Type: "time"},
								{Name: "Line", Type: "string"},
							},
						},
						Data: loki.DataFrameData{
							Values: [][]any{
								{
									map[string]any{"job": "a"},
									map[string]any{"job": "b"},
									map[string]any{"job": "a"},
								},
								{float64(1000), float64(2000), float64(3000)},
								{"line-a1", "line-b1", "line-a2"},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, resp.Data.Result, 2)

	// First stream: job=a with 2 entries
	assert.Equal(t, map[string]string{"job": "a"}, resp.Data.Result[0].Stream)
	assert.Len(t, resp.Data.Result[0].Values, 2)

	// Second stream: job=b with 1 entry
	assert.Equal(t, map[string]string{"job": "b"}, resp.Data.Result[1].Stream)
	assert.Len(t, resp.Data.Result[1].Values, 1)
}

func TestConvertGrafanaResponse_LegacyFormat(t *testing.T) {
	// Legacy format: no "labels" field, labels in field metadata.
	resp := loki.ConvertGrafanaResponse(&loki.GrafanaQueryResponse{
		Results: map[string]loki.GrafanaResult{
			"A": {
				Frames: []loki.DataFrame{
					{
						Schema: loki.DataFrameSchema{
							Fields: []loki.Field{
								{Name: "time", Type: "time"},
								{Name: "value", Type: "string", Labels: map[string]string{"job": "legacy"}},
							},
						},
						Data: loki.DataFrameData{
							Values: [][]any{
								{float64(1711893600000)},
								{"legacy log line"},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, resp.Data.Result, 1)
	assert.Equal(t, map[string]string{"job": "legacy"}, resp.Data.Result[0].Stream)
	assert.Equal(t, "legacy log line", resp.Data.Result[0].Values[0][1])
}

func TestConvertGrafanaResponse_LegacyFormat_PrefersContentField(t *testing.T) {
	// When falling to legacy format with "Line" and "id" fields,
	// should prefer "Line" for content over "id".
	resp := loki.ConvertGrafanaResponse(&loki.GrafanaQueryResponse{
		Results: map[string]loki.GrafanaResult{
			"A": {
				Frames: []loki.DataFrame{
					{
						Schema: loki.DataFrameSchema{
							Fields: []loki.Field{
								{Name: "time", Type: "time"},
								{Name: "Line", Type: "string"},
								{Name: "id", Type: "string"},
								{Name: "tsNs", Type: "string"},
							},
						},
						Data: loki.DataFrameData{
							Values: [][]any{
								{float64(1711893600000)},
								{"the actual log line"},
								{"1711893600000000000_abc123"},
								{"1711893600000000000"},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, resp.Data.Result, 1)
	assert.Equal(t, "the actual log line", resp.Data.Result[0].Values[0][1])
}

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want map[string]string
	}{
		{
			name: "nil returns empty map",
			in:   nil,
			want: map[string]string{},
		},
		{
			name: "map[string]any",
			in:   map[string]any{"job": "grafana", "level": "info"},
			want: map[string]string{"job": "grafana", "level": "info"},
		},
		{
			name: "map[string]string",
			in:   map[string]string{"job": "grafana"},
			want: map[string]string{"job": "grafana"},
		},
		{
			name: "JSON string",
			in:   `{"job":"grafana","instance":"localhost"}`,
			want: map[string]string{"job": "grafana", "instance": "localhost"},
		},
		{
			name: "empty string returns empty map",
			in:   "",
			want: map[string]string{},
		},
		{
			name: "non-parseable type returns empty map",
			in:   42,
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loki.ParseLabels(tt.in)
			assert.NotNil(t, got, "parseLabels must never return nil")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStreamLabelsNeverNull(t *testing.T) {
	// Even with nil labels, JSON output must not contain "stream":null.
	resp := loki.ConvertGrafanaResponse(&loki.GrafanaQueryResponse{
		Results: map[string]loki.GrafanaResult{
			"A": {
				Frames: []loki.DataFrame{
					{
						Schema: loki.DataFrameSchema{
							Fields: []loki.Field{
								{Name: "labels", Type: "other"},
								{Name: "Time", Type: "time"},
								{Name: "Line", Type: "string"},
							},
						},
						Data: loki.DataFrameData{
							Values: [][]any{
								{nil}, // nil labels
								{float64(1000)},
								{"a log line"},
							},
						},
					},
				},
			},
		},
	})

	require.Len(t, resp.Data.Result, 1)
	assert.NotNil(t, resp.Data.Result[0].Stream)

	// Verify JSON serialization doesn't contain "stream":null
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"stream":null`)
}

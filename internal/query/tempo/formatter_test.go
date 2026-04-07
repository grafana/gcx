package tempo_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSearchTable(t *testing.T) {
	resp := &tempo.SearchResponse{
		Traces: []tempo.SearchTrace{
			{
				TraceID:           "abc123",
				RootServiceName:   "frontend",
				RootTraceName:     "GET /api/users",
				StartTimeUnixNano: "1700000000000000000", // 2023-11-14T22:13:20Z
				DurationMs:        42,
			},
			{
				TraceID:           "def456",
				RootServiceName:   "backend",
				RootTraceName:     "POST /api/orders",
				StartTimeUnixNano: "1700003600000000000",
				DurationMs:        1500,
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatSearchTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	// Header
	assert.Contains(t, out, "TRACE_ID")
	assert.Contains(t, out, "SERVICE")
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "DURATION")
	assert.Contains(t, out, "START")

	// Row data
	assert.Contains(t, out, "abc123")
	assert.Contains(t, out, "frontend")
	assert.Contains(t, out, "GET /api/users")
	assert.Contains(t, out, "42ms")

	assert.Contains(t, out, "def456")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "1.50s") // 1500ms formatted as seconds

	// Timestamp should be RFC3339 in local timezone
	expectedDate := time.Unix(0, 1700000000000000000).Format(time.RFC3339)[:11] // "YYYY-MM-DDT" prefix
	assert.Contains(t, out, expectedDate)
}

func TestFormatSearchTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := tempo.FormatSearchTable(&buf, &tempo.SearchResponse{})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "TRACE_ID")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 1)
}

func TestFormatTagsTable(t *testing.T) {
	resp := &tempo.TagsResponse{
		Scopes: []tempo.TagScope{
			{Name: "resource", Tags: []string{"service.name", "host.name"}},
			{Name: "span", Tags: []string{"http.method", "http.status_code"}},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatTagsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SCOPE")
	assert.Contains(t, out, "TAG")
	assert.Contains(t, out, "resource")
	assert.Contains(t, out, "service.name")
	assert.Contains(t, out, "host.name")
	assert.Contains(t, out, "span")
	assert.Contains(t, out, "http.method")

	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 5) // 1 header + 4 data rows
}

func TestFormatTagValuesTable(t *testing.T) {
	resp := &tempo.TagValuesResponse{
		TagValues: []tempo.TagValue{
			{Type: "string", Value: "frontend"},
			{Type: "string", Value: "backend"},
			{Type: "int", Value: float64(200)},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatTagValuesTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "VALUE")
	assert.Contains(t, out, "string")
	assert.Contains(t, out, "frontend")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "int")
	assert.Contains(t, out, "200")
}

func TestFormatMetricsTable_Range(t *testing.T) {
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "web"}},
				},
				Samples: []tempo.MetricsSample{
					{TimestampMs: "1700000000000", Value: 42.5},
					{TimestampMs: "1700000060000", Value: 43.0},
				},
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "LABELS")
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "VALUE")
	assert.Contains(t, out, "42.5")
	assert.Contains(t, out, "43")
	assert.Contains(t, out, "1700000000000")
	assert.Contains(t, out, "1700000060000")
}

func TestFormatMetricsTable_Instant(t *testing.T) {
	val := float64(99)
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "api"}},
				},
				TimestampMs: "1700003600000",
				Value:       &val,
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "99")
	assert.Contains(t, out, "1700003600000")
}

func TestFormatMetricsTable_InstantWithoutTimestamp(t *testing.T) {
	val := float64(99)
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "api"}},
				},
				Value: &val,
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "LABELS")
	assert.Contains(t, out, "VALUE")
	assert.NotContains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "99")
}

func TestFormatMetricsTable_EmptySeries(t *testing.T) {
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "web"}},
				},
				// No Samples and no Value — row should be skipped
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 1) // Only header
}

func TestFormatMetricsLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels []tempo.MetricsLabel
		want   string
	}{
		{
			name:   "empty labels",
			labels: nil,
			want:   "{}",
		},
		{
			name: "single label",
			labels: []tempo.MetricsLabel{
				{Key: "service", Value: map[string]any{"stringValue": "web"}},
			},
			want: `{service="web"}`,
		},
		{
			name: "multiple labels sorted by key",
			labels: []tempo.MetricsLabel{
				{Key: "zone", Value: map[string]any{"stringValue": "us-east"}},
				{Key: "service", Value: map[string]any{"stringValue": "api"}},
			},
			want: `{service="api", zone="us-east"}`,
		},
		{
			name: "intValue extraction",
			labels: []tempo.MetricsLabel{
				{Key: "status", Value: map[string]any{"intValue": "200"}},
			},
			want: `{status="200"}`,
		},
		{
			name: "boolValue extraction",
			labels: []tempo.MetricsLabel{
				{Key: "error", Value: map[string]any{"boolValue": true}},
			},
			want: `{error="true"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tempo.FormatMetricsLabels(tc.labels)
			assert.Equal(t, tc.want, got)
		})
	}
}

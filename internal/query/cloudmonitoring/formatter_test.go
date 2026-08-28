package cloudmonitoring_test

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testResponse() *cloudmonitoring.QueryResponse {
	v := 0.42
	return &cloudmonitoring.QueryResponse{
		Frames: []cloudmonitoring.Frame{{
			Name:       "cpu/utilization",
			Labels:     map[string]string{"resource.type": "gce_instance"},
			Timestamps: []time.Time{time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)},
			Values:     []*float64{&v},
		}},
	}
}

func TestFormatTable(t *testing.T) {
	t.Run("renders rows", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, cloudmonitoring.FormatTable(&buf, testResponse()))
		out := buf.String()
		assert.Contains(t, out, "cpu/utilization")
		assert.Contains(t, out, "0.42")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, cloudmonitoring.FormatTable(&buf, &cloudmonitoring.QueryResponse{}))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatWide(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, cloudmonitoring.FormatWide(&buf, testResponse()))
	assert.Contains(t, buf.String(), `resource.type="gce_instance"`)
}

func TestFormatProjects(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, cloudmonitoring.FormatProjects(&buf, []cloudmonitoring.Project{{ID: "p1", Name: "Project One"}}))
	assert.Contains(t, buf.String(), "p1")

	buf.Reset()
	require.NoError(t, cloudmonitoring.FormatProjects(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatMetricDescriptors(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, cloudmonitoring.FormatMetricDescriptors(&buf, []cloudmonitoring.MetricDescriptor{
		{Type: "compute.googleapis.com/instance/cpu/utilization", MetricKind: "GAUGE", ValueType: "DOUBLE", Unit: "10^2.%"},
	}))
	out := buf.String()
	assert.Contains(t, out, "GAUGE")
	assert.Contains(t, out, "10^2.%")

	buf.Reset()
	require.NoError(t, cloudmonitoring.FormatMetricDescriptors(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

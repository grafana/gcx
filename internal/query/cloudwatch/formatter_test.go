package cloudwatch_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestResponse(name string, ts []time.Time, vals []*float64) *cloudwatch.QueryResponse {
	return &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			{
				Name:       name,
				Timestamps: ts,
				Values:     vals,
			},
		},
	}
}

func pf(f float64) *float64 {
	v := f
	return &v
}

func ts(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestFormatTable_Populated(t *testing.T) {
	resp := makeTestResponse("", []time.Time{ts("2026-05-17T00:00:00Z")}, []*float64{pf(42.5)})

	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatTable(&buf, resp))
	assert.Contains(t, buf.String(), "TIMESTAMP")
	assert.Contains(t, buf.String(), "VALUE")
	assert.Contains(t, buf.String(), "2026-05-17")
	assert.Contains(t, buf.String(), "42.5")
	assert.NotContains(t, buf.String(), "1778") // no raw millisecond timestamps
}

func TestFormatTable_MismatchedSliceLengthsNoPanic(t *testing.T) {
	// Frame with 2 timestamps but only 1 value must not panic; the formatter
	// stops at the shorter slice (mirrors internal/graph/cloudwatch.go).
	resp := &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			{
				Timestamps: []time.Time{ts("2026-05-17T00:00:00Z"), ts("2026-05-17T00:01:00Z")},
				Values:     []*float64{pf(1.0)},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatTable(&buf, resp))
	require.NoError(t, cloudwatch.FormatWide(&buf, resp))
}

func TestFormatTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatTable(&buf, &cloudwatch.QueryResponse{}))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatTable_NilValue(t *testing.T) {
	resp := makeTestResponse("", []time.Time{ts("2026-05-17T00:00:00Z")}, []*float64{nil})
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatTable(&buf, resp))
	// nil value becomes empty string in output — no panic
}

func TestFormatWide_HasLabelColumn(t *testing.T) {
	resp := &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			{
				Name:       "CPUUtilization",
				Labels:     map[string]string{"InstanceId": "i-abc"},
				Timestamps: []time.Time{ts("2026-05-17T00:00:00Z")},
				Values:     []*float64{pf(10.0)},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatWide(&buf, resp))
	assert.Contains(t, buf.String(), "LABEL")
	assert.Contains(t, buf.String(), "InstanceId")
}

func TestFormatWide_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatWide(&buf, &cloudwatch.QueryResponse{}))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatNamespaces_Populated(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatNamespaces(&buf, []string{"AWS/EC2", "AWS/Lambda"}))
	assert.Contains(t, buf.String(), "NAMESPACE")
	assert.Contains(t, buf.String(), "AWS/EC2")
	assert.Contains(t, buf.String(), "AWS/Lambda")
}

func TestFormatNamespaces_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatNamespaces(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatMetrics_Populated(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatMetrics(&buf, []cloudwatch.Metric{
		{Name: "CPUUtilization", Namespace: "AWS/EC2"},
	}))
	assert.Contains(t, buf.String(), "NAMESPACE")
	assert.Contains(t, buf.String(), "METRIC")
	assert.Contains(t, buf.String(), "CPUUtilization")
	assert.Contains(t, buf.String(), "AWS/EC2")
}

func TestFormatMetrics_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatMetrics(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatDimensions_Populated(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatDimensions(&buf, []string{"InstanceId", "AutoScalingGroupName"}))
	assert.Contains(t, buf.String(), "DIMENSION")
	assert.Contains(t, buf.String(), "InstanceId")
}

func TestFormatDimensions_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatDimensions(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatRegions_Populated(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatRegions(&buf, []string{"us-east-1", "eu-west-1"}))
	assert.Contains(t, buf.String(), "REGION")
	assert.Contains(t, buf.String(), "us-east-1")
}

func TestFormatRegions_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatRegions(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatAccounts_Populated(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatAccounts(&buf, []cloudwatch.Account{
		{ID: "123456", Label: "Prod", ARN: "arn:aws:iam::123456:root"},
	}))
	assert.Contains(t, buf.String(), "ID")
	assert.Contains(t, buf.String(), "LABEL")
	assert.Contains(t, buf.String(), "ARN")
	assert.Contains(t, buf.String(), "123456")
	assert.Contains(t, buf.String(), "Prod")
}

func TestFormatAccounts_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatAccounts(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatTable_RFC3339Timestamps(t *testing.T) {
	resp := makeTestResponse("", []time.Time{ts("2026-05-17T10:00:00Z")}, []*float64{pf(1.0)})
	var buf bytes.Buffer
	require.NoError(t, cloudwatch.FormatTable(&buf, resp))
	assert.Contains(t, buf.String(), "2026-")
}

package cloudwatch_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQueryResponse(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		assert func(*testing.T, *cloudwatch.QueryResponse, error)
	}{
		{
			name: "time series",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"CPUUtilization","type":"number","labels":{"InstanceId":"i-abc"}}]},"data":{"values":[[1747000000000,1747000060000],[12.5,15.3]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				assert.Len(t, resp.Frames[0].Timestamps, 2)
				require.NotNil(t, resp.Frames[0].Values[0])
				assert.InDelta(t, 12.5, *resp.Frames[0].Values[0], 0.001)
				assert.Equal(t, "i-abc", resp.Frames[0].Labels["InstanceId"])
			},
		},
		{
			name: "multi-frame",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number"}]},"data":{"values":[[1747000000000],[1.0]]}},{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number","labels":{"InstanceId":"i-xyz"}}]},"data":{"values":[[1747000000000],[2.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Len(t, resp.Frames, 2)
			},
		},
		{
			name: "empty values placeholder frame is dropped",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number"}]},"data":{"values":[[],[]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Empty(t, resp.Frames)
			},
		},
		{
			name: "error result",
			body: []byte(`{"results":{"A":{"error":"metric not found","errorSource":"downstream","status":400}}}`),
			assert: func(t *testing.T, _ *cloudwatch.QueryResponse, err error) {
				t.Helper()
				var apiErr *queryerror.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, "cloudwatch", apiErr.Datasource)
				assert.Equal(t, "query", apiErr.Operation)
				assert.Equal(t, 400, apiErr.StatusCode)
				assert.Equal(t, "metric not found", apiErr.Message)
				assert.Equal(t, "downstream", apiErr.ErrorSource)
			},
		},
		{
			name: "error result with default status",
			body: []byte(`{"results":{"A":{"error":"boom"}}}`),
			assert: func(t *testing.T, _ *cloudwatch.QueryResponse, err error) {
				t.Helper()
				var apiErr *queryerror.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 400, apiErr.StatusCode)
			},
		},
		{
			name: "missing A",
			body: []byte(`{"results":{}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Empty(t, resp.Frames)
			},
		},
		{
			name: "malformed JSON",
			body: []byte(`not json`),
			assert: func(t *testing.T, _ *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.Error(t, err)
			},
		},
		{
			name: "nullable values",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number"}]},"data":{"values":[[1747000000000,1747000060000],[null,5.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				assert.Nil(t, resp.Frames[0].Values[0])
				require.NotNil(t, resp.Frames[0].Values[1])
				assert.InDelta(t, 5.0, *resp.Frames[0].Values[1], 0.001)
			},
		},
		{
			// Non-numeric timestamp must drop the row, not emit time.UnixMilli(0) (1970-01-01).
			name: "string timestamp skipped",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number"}]},"data":{"values":[["not-a-number",1747000000000],[1.0,2.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				require.Len(t, resp.Frames[0].Timestamps, 1)
				require.NotNil(t, resp.Frames[0].Values[0])
				assert.InDelta(t, 2.0, *resp.Frames[0].Values[0], 0.001)
			},
		},
		{
			// Boolean (or unsupported type) in the value column must drop the row,
			// not pair the timestamp with a fabricated zero.
			name: "unparseable value drops row",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number"}]},"data":{"values":[[1747000000000,1747000060000],[true,5.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				require.Len(t, resp.Frames[0].Timestamps, 1)
				require.NotNil(t, resp.Frames[0].Values[0])
				assert.InDelta(t, 5.0, *resp.Frames[0].Values[0], 0.001)
			},
		},
		{
			name: "displayNameFromDS",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"value","type":"number","config":{"displayNameFromDS":"CPUUtilization (Average)"}}]},"data":{"values":[[1747000000000],[42.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				assert.Equal(t, "CPUUtilization (Average)", resp.Frames[0].Name)
			},
		},
		{
			// Schema declares 3 fields but Data carries 2; older code indexed
			// Data.Values[2] and panicked. Frame must be dropped.
			name: "schema/data mismatch treated as empty",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"displayName","type":"string"},{"name":"v","type":"number"}]},"data":{"values":[[1747000000000],[1.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Empty(t, resp.Frames)
			},
		},
		{
			// CloudWatch frames can carry multiple numeric columns. The parser
			// stops on the first time/value pair so labels/displayName stay
			// attached to that column. Dropping the break would silently surface
			// the wrong series.
			name: "multi-numeric field break",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"first","type":"number","labels":{"k":"first"},"config":{"displayNameFromDS":"first-series"}},{"name":"second","type":"number","labels":{"k":"second"},"config":{"displayNameFromDS":"second-series"}}]},"data":{"values":[[1747000000000],[1.0],[2.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				assert.Equal(t, "first", resp.Frames[0].Labels["k"])
				assert.Equal(t, "first-series", resp.Frames[0].Name)
				require.NotNil(t, resp.Frames[0].Values[0])
				assert.InDelta(t, 1.0, *resp.Frames[0].Values[0], 0.001)
			},
		},
		{
			// Untyped field whose Name is "Value" must still produce a series.
			name: "Value field by name",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"Value"}]},"data":{"values":[[1747000000000],[42.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				require.NotNil(t, resp.Frames[0].Values[0])
				assert.InDelta(t, 42.0, *resp.Frames[0].Values[0], 0.001)
			},
		},
		{
			name: "timestamp milliseconds",
			body: []byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"time","type":"time"},{"name":"v","type":"number"}]},"data":{"values":[[1747000000000],[1.0]]}}]}}}`),
			assert: func(t *testing.T, resp *cloudwatch.QueryResponse, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Len(t, resp.Frames, 1)
				assert.Equal(t, time.UnixMilli(1747000000000).UTC(), resp.Frames[0].Timestamps[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			resp, err := cloudwatch.ParseQueryResponse(tt.body)
			tt.assert(t, resp, err)
		})
	}
}

func TestParseNamespaces(t *testing.T) {
	t.Run("parses flat value list", func(t *testing.T) {
		body := []byte(`[{"value":"AWS/EC2"},{"value":"AWS/Lambda"}]`)
		result, err := cloudwatch.ParseNamespaces(body)
		require.NoError(t, err)
		assert.Equal(t, []string{"AWS/EC2", "AWS/Lambda"}, result)
	})

	t.Run("empty list", func(t *testing.T) {
		result, err := cloudwatch.ParseNamespaces([]byte(`[]`))
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := cloudwatch.ParseNamespaces([]byte(`not json`))
		require.Error(t, err)
	})

	t.Run("wrong inner shape returns error", func(t *testing.T) {
		body := []byte(`[{"value":"AWS/EC2"},{"value":{"unexpected":"obj"}}]`)
		_, err := cloudwatch.ParseNamespaces(body)
		require.Error(t, err)
	})
}

func TestParseMetrics(t *testing.T) {
	t.Run("parses nested value shape", func(t *testing.T) {
		body := []byte(`[{"value":{"name":"CPUUtilization","namespace":"AWS/EC2"}},{"value":{"name":"Invocations","namespace":"AWS/Lambda"}}]`)
		result, err := cloudwatch.ParseMetrics(body)
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "CPUUtilization", result[0].Name)
		assert.Equal(t, "AWS/EC2", result[0].Namespace)
	})
}

func TestParseDimensionKeys(t *testing.T) {
	t.Run("parses flat value list", func(t *testing.T) {
		body := []byte(`[{"value":"InstanceId"},{"value":"AutoScalingGroupName"}]`)
		result, err := cloudwatch.ParseDimensionKeys(body)
		require.NoError(t, err)
		assert.Equal(t, []string{"InstanceId", "AutoScalingGroupName"}, result)
	})
}

func TestParseRegions(t *testing.T) {
	t.Run("parses nested name shape", func(t *testing.T) {
		body := []byte(`[{"value":{"name":"us-east-1"}},{"value":{"name":"eu-west-1"}}]`)
		result, err := cloudwatch.ParseRegions(body)
		require.NoError(t, err)
		assert.Equal(t, []string{"us-east-1", "eu-west-1"}, result)
	})

	t.Run("drops items with empty name", func(t *testing.T) {
		body := []byte(`[{"value":{"name":""}},{"value":{"name":"us-east-1"}}]`)
		result, err := cloudwatch.ParseRegions(body)
		require.NoError(t, err)
		assert.Equal(t, []string{"us-east-1"}, result)
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := cloudwatch.ParseRegions([]byte(`not json`))
		require.Error(t, err)
	})
}

func TestParseAccounts(t *testing.T) {
	t.Run("parses account shape", func(t *testing.T) {
		body := []byte(`[{"value":{"id":"123456789","label":"My Account","arn":"arn:aws:iam::123456789:root"}}]`)
		result, err := cloudwatch.ParseAccounts(body)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "123456789", result[0].ID)
		assert.Equal(t, "My Account", result[0].Label)
		assert.Equal(t, "arn:aws:iam::123456789:root", result[0].ARN)
	})
}

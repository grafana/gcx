package cloudwatch_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQueryResponse_TimeSeries(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"frames": [{
					"schema": {
						"fields": [
							{"name": "time", "type": "time"},
							{"name": "CPUUtilization", "type": "number", "labels": {"InstanceId": "i-abc"}}
						]
					},
					"data": {
						"values": [
							[1747000000000, 1747000060000],
							[12.5, 15.3]
						]
					}
				}]
			}
		}
	}`)

	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	require.Len(t, resp.Frames, 1)
	assert.Len(t, resp.Frames[0].Timestamps, 2)
	assert.Len(t, resp.Frames[0].Values, 2)
	require.NotNil(t, resp.Frames[0].Values[0])
	assert.InDelta(t, 12.5, *resp.Frames[0].Values[0], 0.001)
	assert.Equal(t, "i-abc", resp.Frames[0].Labels["InstanceId"])
}

func TestParseQueryResponse_MultiFrame(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"frames": [
					{
						"schema": {"fields": [{"name":"time","type":"time"},{"name":"v","type":"number"}]},
						"data": {"values": [[1747000000000],[1.0]]}
					},
					{
						"schema": {"fields": [{"name":"time","type":"time"},{"name":"v","type":"number","labels":{"InstanceId":"i-xyz"}}]},
						"data": {"values": [[1747000000000],[2.0]]}
					}
				]
			}
		}
	}`)

	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	assert.Len(t, resp.Frames, 2)
}

func TestParseQueryResponse_EmptyValues(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"frames": [{
					"schema": {"fields": [{"name":"time","type":"time"},{"name":"v","type":"number"}]},
					"data": {"values": [[],[]]}
				}]
			}
		}
	}`)

	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	require.Len(t, resp.Frames, 1)
	assert.Empty(t, resp.Frames[0].Timestamps)
}

func TestParseQueryResponse_ErrorResult(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"error": "metric not found",
				"status": 400
			}
		}
	}`)

	_, err := cloudwatch.ParseQueryResponse(body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metric not found")
}

func TestParseQueryResponse_MissingA(t *testing.T) {
	body := []byte(`{"results": {}}`)
	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	assert.Empty(t, resp.Frames)
}

func TestParseQueryResponse_MalformedJSON(t *testing.T) {
	_, err := cloudwatch.ParseQueryResponse([]byte(`not json`))
	require.Error(t, err)
}

func TestParseQueryResponse_NullableValues(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"frames": [{
					"schema": {"fields": [{"name":"time","type":"time"},{"name":"v","type":"number"}]},
					"data": {"values": [[1747000000000, 1747000060000],[null, 5.0]]}
				}]
			}
		}
	}`)

	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	require.Len(t, resp.Frames, 1)
	assert.Nil(t, resp.Frames[0].Values[0])
	require.NotNil(t, resp.Frames[0].Values[1])
	assert.InDelta(t, 5.0, *resp.Frames[0].Values[1], 0.001)
}

func TestParseQueryResponse_DisplayNameFromDS(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"frames": [{
					"schema": {
						"fields": [
							{"name":"time","type":"time"},
							{"name":"value","type":"number","config":{"displayNameFromDS":"CPUUtilization (Average)"}}
						]
					},
					"data": {"values": [[1747000000000],[42.0]]}
				}]
			}
		}
	}`)

	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	require.Len(t, resp.Frames, 1)
	assert.Equal(t, "CPUUtilization (Average)", resp.Frames[0].Name)
}

func TestParseQueryResponse_TimestampMilliseconds(t *testing.T) {
	body := []byte(`{
		"results": {
			"A": {
				"frames": [{
					"schema": {"fields": [{"name":"time","type":"time"},{"name":"v","type":"number"}]},
					"data": {"values": [[1747000000000],[1.0]]}
				}]
			}
		}
	}`)

	resp, err := cloudwatch.ParseQueryResponse(body)
	require.NoError(t, err)
	require.Len(t, resp.Frames, 1)
	expected := time.UnixMilli(1747000000000).UTC()
	assert.Equal(t, expected, resp.Frames[0].Timestamps[0])
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

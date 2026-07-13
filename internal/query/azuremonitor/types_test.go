package azuremonitor_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQueryResponse_ValueCoercion(t *testing.T) {
	t.Run("string-encoded numbers are parsed", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{
			{Name: "Time", Type: "time"},
			{Name: "Transactions", Type: "number"},
		}
		f.Data.Values = []any{
			[]any{"1747000000000", 1747000060000},
			[]any{"10.5", int64(20)},
		}
		body := queryResultBody(t, testResultEntry{Frames: []testFrame{f}})

		resp, err := azuremonitor.ParseQueryResponse(body)
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		require.Len(t, resp.Frames[0].Values, 2)
		require.NotNil(t, resp.Frames[0].Values[0])
		assert.InDelta(t, 10.5, *resp.Frames[0].Values[0], 0.001)
		require.NotNil(t, resp.Frames[0].Values[1])
		assert.InDelta(t, 20.0, *resp.Frames[0].Values[1], 0.001)
	})

	t.Run("unparseable value drops the row, not a fabricated zero", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{
			{Name: "Time", Type: "time"},
			{Name: "Transactions", Type: "number"},
		}
		f.Data.Values = []any{
			[]any{1747000000000.0, 1747000060000.0},
			[]any{"not-a-number", 2.0},
		}
		body := queryResultBody(t, testResultEntry{Frames: []testFrame{f}})

		resp, err := azuremonitor.ParseQueryResponse(body)
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		require.Len(t, resp.Frames[0].Timestamps, 1)
		require.Len(t, resp.Frames[0].Values, 1)
		assert.InDelta(t, 2.0, *resp.Frames[0].Values[0], 0.001)
	})
}

func TestParseTableResponse(t *testing.T) {
	t.Run("multiple frames with matching columns are appended", func(t *testing.T) {
		var f1, f2 testFrame
		for _, f := range []*testFrame{&f1, &f2} {
			f.Schema.Fields = []testField{{Name: "name", Type: "string"}}
		}
		f1.Data.Values = []any{[]any{"a"}}
		f2.Data.Values = []any{[]any{"b"}}
		body := queryResultBody(t, testResultEntry{Frames: []testFrame{f1, f2}})

		resp, err := azuremonitor.ParseTableResponse(body, "logs")
		require.NoError(t, err)
		require.Len(t, resp.Rows, 2)
	})

	t.Run("frame with mismatched columns is skipped", func(t *testing.T) {
		var f1, f2 testFrame
		f1.Schema.Fields = []testField{{Name: "name", Type: "string"}}
		f1.Data.Values = []any{[]any{"a"}}
		f2.Schema.Fields = []testField{{Name: "other", Type: "number"}}
		f2.Data.Values = []any{[]any{1.0}}
		body := queryResultBody(t, testResultEntry{Frames: []testFrame{f1, f2}})

		resp, err := azuremonitor.ParseTableResponse(body, "logs")
		require.NoError(t, err)
		require.Len(t, resp.Rows, 1)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, err := azuremonitor.ParseTableResponse([]byte(`{bad`), "logs")
		require.Error(t, err)
	})
}

func TestQueryErrorSimplification(t *testing.T) {
	// Real error strings as produced by the Azure Monitor plugin backend,
	// captured from live queries.
	parseErr := func(t *testing.T, errStr string) string {
		t.Helper()
		body := queryResultBody(t, testResultEntry{Error: errStr, Status: 400})
		_, err := azuremonitor.ParseTableResponse(body, "logs")
		require.Error(t, err)
		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		return apiErr.Message
	}

	t.Run("nested KQL semantic error reduced to deepest message", func(t *testing.T) {
		raw := `request failed, status: 400 Bad Request, body: {"error":{"message":"The request had some invalid properties","code":"BadArgumentError","correlationId":"c157","innererror":{"code":"SemanticError","message":"A semantic error occurred.","innererror":{"code":"SEM0100","message":"'take' operator: Failed to resolve table or column expression named 'NotARealTable'"}}}}`
		msg := parseErr(t, raw)
		assert.Equal(t, "SEM0100: 'take' operator: Failed to resolve table or column expression named 'NotARealTable'", msg)
	})

	t.Run("flat ARM error keeps code and message", func(t *testing.T) {
		raw := `request failed, status: 400 Bad Request, body: {"error":{"code":"InvalidSubscriptionId","message":"The provided subscription identifier 'x' is malformed or invalid."}}`
		msg := parseErr(t, raw)
		assert.Equal(t, "InvalidSubscriptionId: The provided subscription identifier 'x' is malformed or invalid.", msg)
	})

	t.Run("flat metrics error shape without envelope", func(t *testing.T) {
		raw := `request failed, status: 400 Bad Request, body: {"code":"BadRequest","message":"Failed to find metric configuration for provider: Microsoft.Storage, metric: NotARealMetric"}`
		msg := parseErr(t, raw)
		assert.Equal(t, "BadRequest: Failed to find metric configuration for provider: Microsoft.Storage, metric: NotARealMetric", msg)
	})

	t.Run("non-JSON body passes through unchanged", func(t *testing.T) {
		raw := `request failed, status: 502 Bad Gateway, body: upstream unavailable`
		assert.Equal(t, raw, parseErr(t, raw))
	})

	t.Run("plain error passes through unchanged", func(t *testing.T) {
		raw := "user is not authenticated with Azure AD"
		assert.Equal(t, raw, parseErr(t, raw))
	})
}

func TestParseARMListItems_Malformed(t *testing.T) {
	// A JSON string where an object is expected fails to unmarshal into the
	// item struct; each parser must surface that instead of dropping data.
	bad := []json.RawMessage{json.RawMessage(`"not-an-object"`)}

	t.Run("subscriptions", func(t *testing.T) {
		_, err := azuremonitor.ParseSubscriptions(bad)
		require.Error(t, err)
	})

	t.Run("resource groups", func(t *testing.T) {
		_, err := azuremonitor.ParseResourceGroups(bad)
		require.Error(t, err)
	})

	t.Run("resources", func(t *testing.T) {
		_, err := azuremonitor.ParseResources(bad)
		require.Error(t, err)
	})

	t.Run("metric definitions", func(t *testing.T) {
		_, err := azuremonitor.ParseMetricDefinitions(bad)
		require.Error(t, err)
	})
}

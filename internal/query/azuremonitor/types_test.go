package azuremonitor_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/query/azuremonitor"
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

	t.Run("unparseable timestamp skips the row", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{
			{Name: "Time", Type: "time"},
			{Name: "Transactions", Type: "number"},
		}
		f.Data.Values = []any{
			[]any{true, 1747000060000.0},
			[]any{1.0, 2.0},
		}
		body := queryResultBody(t, testResultEntry{Frames: []testFrame{f}})

		resp, err := azuremonitor.ParseQueryResponse(body)
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		require.Len(t, resp.Frames[0].Timestamps, 1)
		assert.InDelta(t, 2.0, *resp.Frames[0].Values[0], 0.001)
	})

	t.Run("frame without a time field is dropped", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{
			{Name: "a", Type: "number"},
			{Name: "b", Type: "number"},
		}
		f.Data.Values = []any{[]any{1.0}, []any{2.0}}
		body := queryResultBody(t, testResultEntry{Frames: []testFrame{f}})

		resp, err := azuremonitor.ParseQueryResponse(body)
		require.NoError(t, err)
		assert.Empty(t, resp.Frames)
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

package azuremonitor_test

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testResponse() *azuremonitor.QueryResponse {
	v := 42.0
	return &azuremonitor.QueryResponse{
		Frames: []azuremonitor.Frame{
			{
				Name:       "Transactions {GetBlob}",
				Labels:     map[string]string{"apiname": "GetBlob"},
				Unit:       "short",
				Timestamps: []time.Time{time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)},
				Values:     []*float64{&v},
			},
		},
	}
}

func TestFormatTable(t *testing.T) {
	t.Run("renders rows", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatTable(&buf, testResponse()))
		out := buf.String()
		assert.Contains(t, out, "TIMESTAMP")
		assert.Contains(t, out, "42")
		assert.Contains(t, out, "Transactions {GetBlob}")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatTable(&buf, &azuremonitor.QueryResponse{}))
		assert.Contains(t, buf.String(), "No data")
	})

	t.Run("nil value renders empty cell", func(t *testing.T) {
		resp := &azuremonitor.QueryResponse{
			Frames: []azuremonitor.Frame{{
				Timestamps: []time.Time{time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)},
				Values:     []*float64{nil},
			}},
		}
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatTable(&buf, resp))
	})
}

func TestFormatWide(t *testing.T) {
	t.Run("renders rows", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatWide(&buf, testResponse()))
		out := buf.String()
		assert.Contains(t, out, "UNIT")
		assert.Contains(t, out, "short")
		assert.Contains(t, out, `apiname="GetBlob"`)
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatWide(&buf, &azuremonitor.QueryResponse{}))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatTableResponse(t *testing.T) {
	t.Run("renders native columns", func(t *testing.T) {
		resp := &azuremonitor.TableResponse{
			Columns: []azuremonitor.Column{{Name: "TimeGenerated", Type: "time"}, {Name: "Name", Type: "string"}, {Name: "DurationMs", Type: "number"}},
			Rows: [][]any{
				{"2026-05-17T00:00:00Z", "timer", 146.89},
				{"2026-05-17T00:01:00Z", "timer", nil},
			},
		}
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatTableResponse(&buf, resp))
		out := buf.String()
		assert.Contains(t, out, "TIMEGENERATED")
		assert.Contains(t, out, "DURATIONMS")
		assert.Contains(t, out, "timer")
		assert.Contains(t, out, "146.89")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatTableResponse(&buf, &azuremonitor.TableResponse{}))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatSubscriptions(t *testing.T) {
	t.Run("renders rows", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatSubscriptions(&buf, []azuremonitor.Subscription{
			{ID: "sub-1", Name: "Dev"},
		}))
		out := buf.String()
		assert.Contains(t, out, "sub-1")
		assert.Contains(t, out, "Dev")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatSubscriptions(&buf, nil))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatResourceGroups(t *testing.T) {
	t.Run("renders rows", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatResourceGroups(&buf, []azuremonitor.ResourceGroup{
			{Name: "my-rg", Location: "uksouth"},
		}))
		out := buf.String()
		assert.Contains(t, out, "my-rg")
		assert.Contains(t, out, "uksouth")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatResourceGroups(&buf, nil))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatResources(t *testing.T) {
	t.Run("renders rows", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatResources(&buf, []azuremonitor.Resource{
			{Name: "mystorage", Type: "Microsoft.Storage/storageAccounts", Location: "uksouth"},
		}))
		out := buf.String()
		assert.Contains(t, out, "mystorage")
		assert.Contains(t, out, "Microsoft.Storage/storageAccounts")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatResources(&buf, nil))
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatMetricDefinitions(t *testing.T) {
	t.Run("renders definitions", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatMetricDefinitions(&buf, []azuremonitor.MetricDefinition{
			{Name: "Transactions", PrimaryAggregation: "Total", Unit: "Count", Dimensions: []string{"ResponseType", "ApiName"}},
		}))
		out := buf.String()
		assert.Contains(t, out, "Transactions")
		assert.Contains(t, out, "Total")
		assert.Contains(t, out, "ResponseType,ApiName")
	})

	t.Run("no data", func(t *testing.T) {
		var buf strings.Builder
		require.NoError(t, azuremonitor.FormatMetricDefinitions(&buf, nil))
		assert.Contains(t, buf.String(), "No data")
	})
}

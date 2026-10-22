package fleet_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorJSONCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name: "Fleet protojson camel case",
			payload: `{"id":"c-1","collectorType":"COLLECTOR_TYPE_ALLOY","localAttributes":{"collector.version":"1.10.2"},` +
				`"remoteAttributes":{"env":"production"},"createdAt":"2026-08-01T10:00:00Z","updatedAt":"2026-09-18T11:12:13Z","markedInactiveAt":"2026-09-18T12:00:00Z"}`,
		},
		{
			name: "gcx snake case",
			payload: `{"id":"c-1","collector_type":"COLLECTOR_TYPE_ALLOY","local_attributes":{"collector.version":"1.10.2"},` +
				`"remote_attributes":{"env":"production"},"created_at":"2026-08-01T10:00:00Z","updated_at":"2026-09-18T11:12:13Z","marked_inactive_at":"2026-09-18T12:00:00Z"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var collector fleet.Collector
			require.NoError(t, json.Unmarshal([]byte(tt.payload), &collector))
			assert.Equal(t, "COLLECTOR_TYPE_ALLOY", collector.CollectorType)
			assert.Equal(t, "1.10.2", collector.LocalAttributes["collector.version"])
			assert.Equal(t, "production", collector.RemoteAttributes["env"])
			require.NotNil(t, collector.CreatedAt)
			assert.Equal(t, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), *collector.CreatedAt)
			require.NotNil(t, collector.UpdatedAt)
			require.NotNil(t, collector.MarkedInactiveAt)
		})
	}
}

func TestCollectorMarshalUsesStableSnakeCase(t *testing.T) {
	updatedAt := time.Date(2026, 9, 18, 11, 12, 13, 0, time.UTC)
	data, err := json.Marshal(fleet.Collector{
		ID:              "c-1",
		CollectorType:   "COLLECTOR_TYPE_ALLOY",
		LocalAttributes: map[string]string{"collector.version": "1.10.2"},
		UpdatedAt:       &updatedAt,
	})
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	assert.Contains(t, document, "collector_type")
	assert.Contains(t, document, "local_attributes")
	assert.Contains(t, document, "updated_at")
	assert.NotContains(t, document, "collectorType")
	assert.NotContains(t, document, "localAttributes")
	assert.NotContains(t, document, "updatedAt")
}

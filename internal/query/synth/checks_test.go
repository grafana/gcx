package synth_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListChecks(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/proxy/uid/sm-uid/sm/check/list", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery, "plain ListChecks must not send includeAlerts")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"id":42,"job":"my-job","target":"https://example.com","frequency":60000,"timeout":3000,"enabled":true,"settings":{"http":{}},"probes":[1,2],"tenantId":1}
		]`))
	}))

	list, err := client.ListChecks(context.Background(), "sm-uid")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(42), list[0].ID)
	assert.Equal(t, "my-job", list[0].Job)
	assert.True(t, list[0].Enabled)
	assert.Nil(t, list[0].Alerts, "plain ListChecks should not populate Alerts")
}

func TestClient_ListChecksWithAlerts(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/proxy/uid/sm-uid/sm/check/list", r.URL.Path)
		assert.Equal(t, "includeAlerts=true", r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{
				"id":42,"job":"my-job","target":"https://example.com","frequency":60000,
				"timeout":3000,"enabled":true,"settings":{"http":{}},"probes":[1,2],"tenantId":1,
				"alerts":[
					{"name":"ProbeFailedExecutionsTooHigh","threshold":1,"period":"5m","status":"OK","created":1764701744,"modified":1764701744}
				]
			}
		]`))
	}))

	list, err := client.ListChecksWithAlerts(context.Background(), "sm-uid")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Len(t, list[0].Alerts, 1)
	assert.Equal(t, "ProbeFailedExecutionsTooHigh", list[0].Alerts[0].Name)
	assert.Equal(t, float64(1), list[0].Alerts[0].Threshold)
	assert.Equal(t, "5m", list[0].Alerts[0].Period)
	assert.Equal(t, "OK", list[0].Alerts[0].Status)
}

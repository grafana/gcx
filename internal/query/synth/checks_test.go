package synth_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/synth"
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
	assert.InEpsilon(t, float64(1), list[0].Alerts[0].Threshold, 1e-9)
	assert.Equal(t, "5m", list[0].Alerts[0].Period)
	assert.Equal(t, "OK", list[0].Alerts[0].Status)
}

func TestClient_ListChecksFiltered_QueryString(t *testing.T) {
	trueVal, falseVal := true, false
	tests := []struct {
		name string
		opts synth.ListChecksOptions
		want url.Values
	}{
		{
			name: "empty opts sends no query string",
			opts: synth.ListChecksOptions{},
			want: url.Values{},
		},
		{
			name: "search only",
			opts: synth.ListChecksOptions{Search: "api-prod"},
			want: url.Values{"search": []string{"api-prod"}},
		},
		{
			name: "enabled true",
			opts: synth.ListChecksOptions{Enabled: &trueVal},
			want: url.Values{"enabled": []string{"true"}},
		},
		{
			name: "enabled false",
			opts: synth.ListChecksOptions{Enabled: &falseVal},
			want: url.Values{"enabled": []string{"false"}},
		},
		{
			name: "min/max frequency converted to ms",
			opts: synth.ListChecksOptions{
				MinFrequency: 30 * time.Second,
				MaxFrequency: 5 * time.Minute,
			},
			want: url.Values{
				"min_frequency": []string{"30000"},
				"max_frequency": []string{"300000"},
			},
		},
		{
			name: "all filters combined",
			opts: synth.ListChecksOptions{
				Search:       "staging",
				Enabled:      &trueVal,
				MinFrequency: time.Second,
				MaxFrequency: time.Minute,
			},
			want: url.Values{
				"search":        []string{"staging"},
				"enabled":       []string{"true"},
				"min_frequency": []string{"1000"},
				"max_frequency": []string{"60000"},
			},
		},
		{
			name: "with-alerts alone (no filters)",
			opts: synth.ListChecksOptions{WithAlerts: true},
			want: url.Values{"includeAlerts": []string{"true"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got url.Values
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}))

			_, err := client.ListChecksFiltered(context.Background(), "sm-uid", tc.opts)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClient_ListChecksFiltered_RejectsAlertsWithFilters(t *testing.T) {
	// Server should never be hit when client-side validation refuses.
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not receive a request when filters+with-alerts is rejected")
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := client.ListChecksFiltered(context.Background(), "sm-uid", synth.ListChecksOptions{
		Search:     "anything",
		WithAlerts: true,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, synth.ErrFiltersWithAlerts)
}

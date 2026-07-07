package prometheus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_CardinalityLabelNames(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
		capturedAuth  string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		capturedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prometheus.CardinalityLabelNamesResponse{
			LabelValuesCountTotal: 100,
			LabelNamesCount:       2,
			Cardinality: []prometheus.LabelNameCardinality{
				{LabelName: "__name__", LabelValuesCount: 50},
				{LabelName: "job", LabelValuesCount: 10},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.CardinalityLabelNames(context.Background(), "grafanacloud-prom", prometheus.CardinalityOptions{
		Selector:    `{__name__="up"}`,
		CountMethod: "active",
		Limit:       50,
	})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/grafanacloud-prom/resources/api/v1/cardinality/label_names", capturedPath)
	assert.Equal(t, `{__name__="up"}`, capturedQuery.Get("selector"))
	assert.Equal(t, "active", capturedQuery.Get("count_method"))
	assert.Equal(t, "50", capturedQuery.Get("limit"))
	assert.Empty(t, capturedQuery["label_names[]"])
	assert.Equal(t, "Bearer test-token", capturedAuth)

	require.NotNil(t, resp)
	assert.Equal(t, 100, resp.LabelValuesCountTotal)
	assert.Equal(t, 2, resp.LabelNamesCount)
	require.Len(t, resp.Cardinality, 2)
	assert.Equal(t, "__name__", resp.Cardinality[0].LabelName)
	assert.Equal(t, 50, resp.Cardinality[0].LabelValuesCount)
}

func TestClient_CardinalityLabelNames_OmitsEmptyParams(t *testing.T) {
	var capturedQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prometheus.CardinalityLabelNamesResponse{})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.CardinalityLabelNames(context.Background(), "prom", prometheus.CardinalityOptions{Limit: 0})
	require.NoError(t, err)

	// selector and count_method are omitted when empty; limit is always sent.
	_, hasSelector := capturedQuery["selector"]
	_, hasCountMethod := capturedQuery["count_method"]
	assert.False(t, hasSelector)
	assert.False(t, hasCountMethod)
	assert.Equal(t, "0", capturedQuery.Get("limit"))
}

func TestClient_CardinalityLabelValues(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prometheus.CardinalityLabelValuesResponse{
			SeriesCountTotal: 1000,
			Labels: []prometheus.LabelValuesCardinality{
				{
					LabelName:        "job",
					LabelValuesCount: 2,
					SeriesCount:      500,
					Cardinality: []prometheus.LabelValueCardinality{
						{LabelValue: "grafana", SeriesCount: 300},
						{LabelValue: "loki", SeriesCount: 200},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.CardinalityLabelValues(context.Background(), "prom", []string{"job", "instance"}, prometheus.CardinalityOptions{Limit: 20})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom/resources/api/v1/cardinality/label_values", capturedPath)
	assert.Equal(t, []string{"job", "instance"}, capturedQuery["label_names[]"])
	assert.Equal(t, "20", capturedQuery.Get("limit"))

	require.NotNil(t, resp)
	assert.Equal(t, 1000, resp.SeriesCountTotal)
	require.Len(t, resp.Labels, 1)
	assert.Equal(t, "job", resp.Labels[0].LabelName)
	require.Len(t, resp.Labels[0].Cardinality, 2)
	assert.Equal(t, "grafana", resp.Labels[0].Cardinality[0].LabelValue)
	assert.Equal(t, 300, resp.Labels[0].Cardinality[0].SeriesCount)
}

func TestClient_Cardinality_UnavailableFriendlyError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusNotImplemented} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"cardinality analysis is disabled"}`))
		}))

		client := newTestClient(t, srv.URL)
		_, err := client.CardinalityLabelNames(context.Background(), "prom", prometheus.CardinalityOptions{Limit: 20})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only available on Grafana Mimir")
		// The raw upstream message is preserved as the wrapped cause.
		assert.Contains(t, err.Error(), "cardinality analysis is disabled")

		srv.Close()
	}
}

func TestClient_Cardinality_OtherErrorIsRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)
	_, err := client.CardinalityLabelValues(context.Background(), "prom", []string{"job"}, prometheus.CardinalityOptions{Limit: 20})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.NotContains(t, err.Error(), "only available on Grafana Mimir")
}

func TestClient_BuildCardinalityPathsEscapeUID(t *testing.T) {
	c := &prometheus.Client{}

	names := c.BuildCardinalityLabelNamesPath("uid/../admin")
	assert.Contains(t, names, "uid%2F..%2Fadmin")
	assert.NotContains(t, names, "uid/../admin")

	values := c.BuildCardinalityLabelValuesPath("uid/../admin")
	assert.Contains(t, values, "uid%2F..%2Fadmin")
	assert.NotContains(t, values, "uid/../admin")
}

func TestFormatCardinalityLabelNamesTable(t *testing.T) {
	var buf bytes.Buffer
	resp := &prometheus.CardinalityLabelNamesResponse{
		LabelValuesCountTotal: 60,
		LabelNamesCount:       200, // more than the two shown rows: a genuine total
		Cardinality: []prometheus.LabelNameCardinality{
			{LabelName: "__name__", LabelValuesCount: 50},
			{LabelName: "job", LabelValuesCount: 10},
		},
	}

	require.NoError(t, prometheus.FormatCardinalityLabelNamesTable(&buf, resp))

	out := buf.String()
	// Both sections render: response-level totals summary, then the per-name list.
	assert.Contains(t, out, "Summary:")
	assert.Contains(t, out, "Label names:")
	assert.Contains(t, out, "UNIQUE LABEL NAMES")
	assert.Contains(t, out, "UNIQUE LABEL VALUES")
	assert.Contains(t, out, "200") // label_names_count total
	assert.Contains(t, out, "60")  // label_values_count_total
	assert.Contains(t, out, "__name__")
	assert.Contains(t, out, "50")
	assert.Contains(t, out, "job")

	assert.Less(t, strings.Index(out, "Summary:"), strings.Index(out, "Label names:"))
}

func TestFormatCardinalityLabelNamesTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, prometheus.FormatCardinalityLabelNamesTable(&buf, &prometheus.CardinalityLabelNamesResponse{}))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatCardinalityLabelValuesTable(t *testing.T) {
	var buf bytes.Buffer
	resp := &prometheus.CardinalityLabelValuesResponse{
		SeriesCountTotal: 500,
		Labels: []prometheus.LabelValuesCardinality{
			{
				LabelName:        "job",
				LabelValuesCount: 2,
				SeriesCount:      500,
				Cardinality: []prometheus.LabelValueCardinality{
					{LabelValue: "grafana", SeriesCount: 300},
					{LabelValue: "loki", SeriesCount: 200},
				},
			},
		},
	}

	require.NoError(t, prometheus.FormatCardinalityLabelValuesTable(&buf, resp))

	out := buf.String()
	// Both sections are rendered: a per-label summary and the per-value breakdown.
	assert.Contains(t, out, "Summary:")
	assert.Contains(t, out, "Label values:")
	assert.Contains(t, out, "UNIQUE LABEL VALUES") // summary column
	assert.Contains(t, out, "LABEL VALUE")         // breakdown column
	assert.Contains(t, out, "SERIES")
	// Summary row: distinct value count for the label.
	assert.Contains(t, out, "2")
	// Per-value breakdown rows.
	assert.Contains(t, out, "grafana")
	assert.Contains(t, out, "300")
	assert.Contains(t, out, "loki")

	// The "Summary:" section precedes the "Label values:" section.
	assert.Less(t, strings.Index(out, "Summary:"), strings.Index(out, "Label values:"))
}

func TestFormatCardinalityLabelValuesTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, prometheus.FormatCardinalityLabelValuesTable(&buf, &prometheus.CardinalityLabelValuesResponse{}))
	assert.Contains(t, buf.String(), "No data")
}

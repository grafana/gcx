package modelrates_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/modelrates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *modelrates.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := agento11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return modelrates.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/model-rates")

		writeJSON(w, map[string]any{
			"items": []modelrates.Rate{
				{
					Provider:            "openai",
					Model:               "gpt-5.5",
					EffectiveFrom:       "2026-09-23T09:14:22.481739Z",
					InputUSDPerMillion:  new(float64(2)),
					OutputUSDPerMillion: new(float64(8)),
					CreatedBy:           "someone@grafana.com",
				},
			},
		})
	}))

	rates, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, "gpt-5.5", rates[0].Model)
	assert.InDelta(t, 2, *rates[0].InputUSDPerMillion, 0)
}

// TestClient_Set pins the wire contract: the per-million figures go out as
// given, and effective_from is never sent. A write takes effect when the
// server accepts it, so a client that could name the instant would be able to
// backdate a price.
func TestClient_Set(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/model-rates")

		raw, err := io.ReadAll(r.Body)
		// assert rather than require: a failed require inside a handler
		// calls FailNow off the test goroutine, which does not stop the test.
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, json.Unmarshal(raw, &body))

		writeJSON(w, modelrates.Rate{
			Provider:            "openai",
			Model:               "gpt-5.5",
			EffectiveFrom:       "2026-09-23T09:14:22.481739Z",
			InputUSDPerMillion:  new(float64(2)),
			OutputUSDPerMillion: new(float64(8)),
		})
	}))

	stored, err := client.Set(context.Background(), &modelrates.RateWrite{
		Provider:            "openai",
		Model:               "gpt-5.5",
		InputUSDPerMillion:  new(float64(2)),
		OutputUSDPerMillion: new(float64(8)),
	})
	require.NoError(t, err)

	assert.InDelta(t, 2, body["input_usd_per_million"], 0)
	assert.InDelta(t, 8, body["output_usd_per_million"], 0)
	assert.NotContains(t, body, "effective_from", "a client that can name the instant can backdate a price")
	// Rates nobody set are absent, not zero: zero says the model does not
	// charge for that bucket, which is a different statement.
	assert.NotContains(t, body, "request_usd")
	assert.NotContains(t, body, "cache_read_usd_per_million")

	assert.Equal(t, "2026-09-23T09:14:22.481739Z", stored.EffectiveFrom)
}

// TestClient_Delete pins that the full key travels, including the timestamp: a
// model can carry several rates, one per change, so a delete has to name which.
func TestClient_Delete(t *testing.T) {
	var query url.Values
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		query = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))

	effectiveFrom, err := time.Parse(time.RFC3339Nano, "2026-09-23T09:14:22.481739Z")
	require.NoError(t, err)
	require.NoError(t, client.Delete(context.Background(), "openai", "gpt-5.5", effectiveFrom))

	assert.Equal(t, "openai", query.Get("provider"))
	assert.Equal(t, "gpt-5.5", query.Get("model"))
	assert.Equal(t, "2026-09-23T09:14:22.481739Z", query.Get("effective_from"))
}

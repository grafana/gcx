package loki_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestClient_PatternsSummaries(t *testing.T) {
	tests := []struct {
		name string
		in   []loki.Pattern
		want []loki.Pattern
	}{
		{name: "nil"},
		{name: "empty", in: []loki.Pattern{}, want: []loki.Pattern{}},
		{
			name: "sum samples and preserve level",
			in:   []loki.Pattern{{Pattern: "request <_>", Level: "info", Samples: [][]int64{{1, 10}, {2, 15}}}},
			want: []loki.Pattern{{Pattern: "request <_>", Level: "info", Samples: [][]int64{{1, 10}, {2, 15}}, TotalCount: 25}},
		},
		{
			name: "ignore malformed samples",
			in:   []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{nil, {}, {1}, {2, 7}, {3, 4, 99}}}},
			want: []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{nil, {}, {1}, {2, 7}, {3, 4, 99}}, TotalCount: 11}},
		},
		{
			name: "int64 total",
			in:   []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{{1, 1 << 31}, {2, 1}}}},
			want: []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{{1, 1 << 31}, {2, 1}}, TotalCount: 1<<31 + 1}},
		},
		{
			name: "sort descending with alphabetical ties",
			in: []loki.Pattern{
				{Pattern: "rare", Samples: [][]int64{{1, 2}}},
				{Pattern: "zebra", Samples: [][]int64{{1, 5}}},
				{Pattern: "alpha", Samples: [][]int64{{1, 2}, {2, 3}}},
			},
			want: []loki.Pattern{
				{Pattern: "alpha", Samples: [][]int64{{1, 2}, {2, 3}}, TotalCount: 5},
				{Pattern: "zebra", Samples: [][]int64{{1, 5}}, TotalCount: 5},
				{Pattern: "rare", Samples: [][]int64{{1, 2}}, TotalCount: 2},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(loki.PatternsResponse{Status: "success", Data: tt.in}))
			}))
			defer server.Close()

			cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
			client, err := loki.NewClient(cfg)
			require.NoError(t, err)

			start := time.Unix(1711839260, 0)
			resp, err := client.Patterns(context.Background(), "loki-uid", `{job="varlogs"}`, start, start.Add(time.Minute), "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.Data)
		})
	}
}

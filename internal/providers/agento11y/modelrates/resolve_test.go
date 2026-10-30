package modelrates_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/modelrates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	for _, at := range []string{"", "2026-10-01T01:02:03.123400Z"} {
		t.Run(at, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/model-rates:resolve", r.URL.Path)
				var body modelrates.ResolveRequest
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					return
				}
				assert.Equal(t, []modelrates.ResolveItem{{Provider: "synthetic", Model: "test", At: at}}, body.Items)
				writeJSON(w, map[string]any{"items": []map[string]any{{"provider": "synthetic", "model": "test", "at": at, "source": "tenant", "pricing": map[string]any{"prompt_usd_per_token": 0.000001, "future_dimension": 7}}}})
			}))
			result, err := client.Resolve(context.Background(), modelrates.ResolveItem{Provider: "synthetic", Model: "test", At: at})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			assert.Contains(t, string(result.Items[0].Pricing), "future_dimension")
		})
	}
}

func TestResolveRejectsInvalidFlags(t *testing.T) {
	for _, args := range [][]string{{"get-effective"}, {"get-effective", "--provider", "p"}, {"get-effective", "--provider", "p", "--model", "m", "--at", "bad"}, {"get-effective", "--provider", "p", "--model", "m", "--at", ""}} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			cmd := modelrates.Commands(&providers.ConfigLoader{})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(args)
			require.Error(t, cmd.Execute())
		})
	}
}

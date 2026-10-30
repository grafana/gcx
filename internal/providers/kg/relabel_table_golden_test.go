package kg_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRelabelTableGolden(t *testing.T) {
	forceNoColor(t)
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct {
		name  string
		group map[string]any
	}{
		{"populated", map[string]any{"name": "generated", "rules": []any{
			map[string]any{"selector": "{job=\"checkout\"}", "target_label": "service", "join_labels": []any{"namespace", "job"}, "ranked_choice": []any{"service", "app"}, "replacement": "checkout", "drop": true},
			map[string]any{"selector": "{job=\"cart\"}", "target_label": "service", "drop": false},
			map[string]any{"selector": "{job=\"database\"}", "join_labels": []any{"job", 3}, "ranked_choice": "not-a-list"},
			nil, "not-a-rule",
		}}},
		{"empty", map[string]any{"name": "generated", "rules": []any{}}},
		{"missing_rules", map[string]any{"name": "generated"}},
		{"absent", nil},
	} {
		for _, output := range []string{"table", "json"} {
			t.Run(tc.name+"/"+output, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if tc.group == nil {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					writeJSON(w, tc.group)
				}))
				defer server.Close()
				stdout, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewRelabelRulesCommand(writeLoaderFor(server)) }, []string{"get", "-o", output}, "")
				require.NoError(t, err)
				if output == "json" {
					value := decodeSingleJSON(t, []byte(stdout))
					if tc.group == nil {
						require.Nil(t, value)
					} else {
						require.IsType(t, map[string]any{}, value)
					}
				}
				name := "relabel_" + tc.name + "_" + output
				testutils.Golden(t, name+"_stdout", stdout)
				testutils.Golden(t, name+"_stderr", stderr)
			})
		}
	}
}

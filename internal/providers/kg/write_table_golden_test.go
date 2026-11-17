package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTableGolden(t *testing.T) {
	forceNoColor(t)
	testutils.SetAgentMode(t, false)
	for _, kind := range []string{"entities", "relationships"} {
		for _, scenario := range []struct {
			name             string
			count, successes int
			array            bool
		}{
			{"single", 1, 1, false},
			{"batch", 2, 2, true},
			{"partial", 2, 1, true},
			{"first_failure", 2, 0, true},
			{"one_item_array", 1, 1, true},
		} {
			for _, output := range []string{"table", "json"} {
				t.Run(kind+"/"+scenario.name+"/"+output, func(t *testing.T) {
					seen := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						seen++
						if seen > scenario.successes {
							w.WriteHeader(http.StatusInternalServerError)
							_, _ = w.Write([]byte(`{"error":"boom"}`))
							return
						}
						if kind == "entities" {
							var req kg.EntityWriteRequest
							assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
							w.WriteHeader(http.StatusCreated)
							writeJSON(w, kg.EntityWriteResponse{Domain: req.Domain, Type: req.Type, Name: req.Name, Scope: req.Scope})
						} else {
							var req kg.RelationshipWriteRequest
							assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
							writeJSON(w, kg.RelationshipWriteResponse{Domain: req.Domain, Type: req.Type, From: req.From, To: req.To})
						}
					}))
					defer server.Close()
					var requests []any
					for _, name := range []string{"checkout", "cart"}[:scenario.count] {
						if kind == "entities" {
							requests = append(requests, kg.EntityWriteRequest{Domain: "myapp", Type: "Service", Name: name, Scope: map[string]string{"site": "west", "env": "prod"}})
						} else {
							requests = append(requests, kg.RelationshipWriteRequest{Domain: "myapp", Type: "CALLS", From: kg.EntityRef{Domain: "myapp", Type: "Service", Name: name}, To: kg.EntityRef{Domain: "myapp", Type: "Service", Name: "database"}})
						}
					}
					var input any = requests
					if !scenario.array {
						input = requests[0]
					}
					stdin, err := json.Marshal(input)
					require.NoError(t, err)
					build := func() *cobra.Command {
						if kind == "entities" {
							return kg.NewEntitiesCreateCommand(writeLoaderFor(server))
						}
						return kg.NewRelationshipsCreateCommand(writeLoaderFor(server))
					}
					stdout, stderr, err := runKgCommand(t, build, []string{"-f", "-", "-o", output}, string(stdin))
					switch {
					case scenario.successes == 0:
						require.Error(t, err)
						var emitted *gcxerrors.EmittedError
						require.NotErrorAs(t, err, &emitted)
						require.Empty(t, stdout)
					case scenario.successes < scenario.count:
						var emitted *gcxerrors.EmittedError
						require.ErrorAs(t, err, &emitted)
						require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
					default:
						require.NoError(t, err)
					}
					if output == "json" && stdout != "" {
						value := decodeSingleJSON(t, []byte(stdout))
						if scenario.count == 1 {
							require.IsType(t, map[string]any{}, value)
						} else {
							require.IsType(t, []any{}, value)
						}
					}
					name := "write_" + kind + "_" + scenario.name + "_" + output
					testutils.Golden(t, name+"_stdout", stdout)
					testutils.Golden(t, name+"_stderr", stderr)
				})
			}
		}
	}
}

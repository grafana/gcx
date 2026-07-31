//nolint:testpackage // white-box testing: drives the unexported run* functions and opts types.
package clusters

// These tests pin the pre-GA agent output contract for the clusters mutation
// commands (configure, remove):
//
//   - default human stdout stays byte-identical to the pre-migration
//     MutationResult.Emit one-liner;
//   - agent mode emits EXACTLY ONE JSON value (a
//     gcx.instrumentation.mutation document) on stdout, then EOF;
//   - explicit -o json / -o yaml overrides are honored.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contractFakeClient returns a fake whose cluster state makes --cost-metrics
// a real change (CostMetrics=false → true).
func contractFakeClient() *fakeClient {
	return &fakeClient{
		GetK8SInstrumentationFn: func(_ context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error) {
			f := false
			return &instrumentation.GetK8SInstrumentationResponse{Cluster: instrumentation.Cluster{
				Name:        clusterName,
				Selection:   "SELECTION_INCLUDED",
				CostMetrics: &f,
			}}, nil
		},
		SetK8SInstrumentationFn: func(_ context.Context, _ string, _ instrumentation.Cluster, _ instrumentation.BackendURLs) error {
			return nil
		},
	}
}

// runClustersMutation drives runConfigure or runRemove with parsed flags and
// returns stdout.
func runClustersMutation(t *testing.T, verb string, flagArgs []string) string {
	t.Helper()

	client := contractFakeClient()
	var buf bytes.Buffer

	switch verb {
	case "configure":
		opts := &configureOpts{}
		cmd := &cobra.Command{}
		opts.setup(cmd.Flags())
		require.NoError(t, cmd.ParseFlags(flagArgs))
		require.NoError(t, opts.Validate())
		require.NoError(t, runConfigure(context.Background(), cmd, opts, client, "prod-eu", instrumentation.BackendURLs{}, &buf))
	case "remove":
		opts := &removeOpts{}
		cmd := &cobra.Command{}
		opts.setup(cmd.Flags())
		require.NoError(t, cmd.ParseFlags(append([]string{"--yes"}, flagArgs...)))
		require.NoError(t, opts.Validate())
		require.NoError(t, runRemove(context.Background(), &opts.IO, client, "prod-eu", instrumentation.BackendURLs{}, &buf))
	default:
		t.Fatalf("unknown verb %q", verb)
	}
	return buf.String()
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON value
// followed by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", raw)
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF, "stdout must contain exactly one JSON value:\n%s", raw)
	return doc
}

func forceAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(func() { agent.SetFlag(false) })
}

func TestClustersMutations_HumanDefault_ByteIdentical(t *testing.T) {
	tests := []struct {
		name string
		verb string
		args []string
		want string
	}{
		{
			name: "configure rmw change",
			verb: "configure",
			args: []string{"--cost-metrics"},
			want: "configure \"prod-eu\": done\n",
		},
		{
			name: "configure rmw no-op",
			verb: "configure",
			args: []string{"--cost-metrics=false"},
			want: "configure \"prod-eu\": no changes\n",
		},
		{
			name: "configure use-defaults",
			verb: "configure",
			args: []string{"--use-defaults", "--yes"},
			want: "configure \"prod-eu\": done\n",
		},
		{
			name: "remove",
			verb: "remove",
			args: nil,
			want: "remove \"prod-eu\": done\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_AGENT_MODE", "false")
			agent.ResetForTesting()
			t.Cleanup(agent.ResetForTesting)

			got := runClustersMutation(t, tt.verb, tt.args)
			assert.Equal(t, tt.want, got, "default human stdout must stay byte-identical")
		})
	}
}

func TestClustersMutations_AgentMode_SingleJSONDocument(t *testing.T) {
	tests := []struct {
		name        string
		verb        string
		args        []string
		wantAction  string
		wantChanged bool
	}{
		{
			name: "configure rmw change", verb: "configure", args: []string{"--cost-metrics"},
			wantAction: "configure", wantChanged: true,
		},
		{
			name: "configure rmw no-op", verb: "configure", args: []string{"--cost-metrics=false"},
			wantAction: "configure", wantChanged: false,
		},
		{
			name: "remove", verb: "remove", args: nil,
			wantAction: "remove", wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The agents default is resolved when the opts bind their flags,
			// so agent mode must be on before runClustersMutation builds them.
			forceAgentMode(t, true)

			doc := decodeSingleJSONValue(t, runClustersMutation(t, tt.verb, tt.args))
			assert.Equal(t, instoutput.MutationResultType, doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tt.wantAction, doc["action"])
			assert.Equal(t, tt.wantChanged, doc["changed"])
			target, ok := doc["target"].(map[string]any)
			require.True(t, ok, "target must be an object")
			assert.Equal(t, "prod-eu", target["cluster"])
		})
	}
}

func TestClustersMutations_ExplicitOutputOverrides(t *testing.T) {
	t.Run("-o json in human mode", func(t *testing.T) {
		t.Setenv("GCX_AGENT_MODE", "false")
		agent.ResetForTesting()
		t.Cleanup(agent.ResetForTesting)

		doc := decodeSingleJSONValue(t, runClustersMutation(t, "configure", []string{"--cost-metrics", "-o", "json"}))
		assert.Equal(t, instoutput.MutationResultType, doc["type"])
		assert.Equal(t, "configure", doc["action"])
	})

	t.Run("-o yaml wins over agent default", func(t *testing.T) {
		forceAgentMode(t, true)

		out := runClustersMutation(t, "remove", []string{"-o", "yaml"})
		assert.Contains(t, out, "action: remove\n")
		assert.Contains(t, out, "type: gcx.instrumentation.mutation\n")
		assert.Contains(t, out, "cluster: prod-eu\n")
	})

	t.Run("-o text explicit in agent mode", func(t *testing.T) {
		forceAgentMode(t, true)

		out := runClustersMutation(t, "remove", []string{"-o", "text"})
		assert.Equal(t, "remove \"prod-eu\": done\n", out)
	})
}

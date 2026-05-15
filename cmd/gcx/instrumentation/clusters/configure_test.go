//nolint:testpackage // white-box testing: accesses unexported runConfigure and types.
package clusters

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/spf13/cobra"
)

// TestRunConfigure_ChangeDetection verifies that runConfigure emits changed:false
// (and skips SetK8SInstrumentation) when the proposed state matches the current state.
func TestRunConfigure_ChangeDetection(t *testing.T) {
	tests := []struct {
		name            string
		args            []string // feature flags passed to the command
		existingCluster instrumentation.Cluster
		wantChanged     bool   // true → SetK8SInstrumentation must be called; false → must NOT be called
		wantOutput      string // substring expected in the output
	}{
		{
			// Idempotent re-run: cluster already has CostMetrics=true; user passes --cost-metrics.
			// Expected: changed=false, SetK8SInstrumentation NOT called.
			name: "idempotent re-run same flag",
			args: []string{"--cost-metrics"},
			existingCluster: instrumentation.Cluster{
				Name:        "prod-eu",
				Selection:   "SELECTION_INCLUDED",
				CostMetrics: func() *bool { v := true; return &v }(),
			},
			wantChanged: false,
			wantOutput:  "no changes",
		},
		{
			// State differs: cluster has CostMetrics=false; user passes --cost-metrics.
			// Expected: changed=true, SetK8SInstrumentation IS called.
			name: "flip one flag",
			args: []string{"--cost-metrics"},
			existingCluster: instrumentation.Cluster{
				Name:        "prod-eu",
				Selection:   "SELECTION_INCLUDED",
				CostMetrics: func() *bool { v := false; return &v }(),
			},
			wantChanged: true,
			wantOutput:  "done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Force non-agent mode so MutationResult.Emit writes human-readable
			// output ("no changes" / "done") that we can assert against consistently,
			// regardless of whether CLAUDECODE or similar env vars are set.
			t.Setenv("GCX_AGENT_MODE", "false")
			agent.ResetForTesting()
			t.Cleanup(agent.ResetForTesting)

			var setCalls int
			client := &fakeClient{
				GetK8SInstrumentationFn: func(_ context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error) {
					c := tt.existingCluster
					c.Name = clusterName
					return &instrumentation.GetK8SInstrumentationResponse{Cluster: c}, nil
				},
				SetK8SInstrumentationFn: func(_ context.Context, _ string, _ instrumentation.Cluster, _ instrumentation.BackendURLs) error {
					setCalls++
					return nil
				},
			}

			// Build a minimal Cobra command with the configure flags so we can
			// use flags.Changed() semantics inside runConfigure.
			opts := &configureOpts{}
			cmd := &cobra.Command{}
			opts.setup(cmd.Flags())
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("flag parse error: %v", err)
			}

			var buf bytes.Buffer
			err := runConfigure(
				context.Background(),
				cmd,
				opts,
				client,
				"prod-eu",
				instrumentation.BackendURLs{},
				&buf,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantChanged && setCalls == 0 {
				t.Error("expected SetK8SInstrumentation to be called, but it was not")
			}
			if !tt.wantChanged && setCalls > 0 {
				t.Errorf("expected SetK8SInstrumentation NOT to be called, but it was called %d time(s)", setCalls)
			}

			out := buf.String()
			if tt.wantOutput != "" && !strings.Contains(out, tt.wantOutput) {
				t.Errorf("output: want %q in %q", tt.wantOutput, out)
			}
		})
	}
}

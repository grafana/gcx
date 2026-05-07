//nolint:testpackage // white-box testing: accesses unexported run* functions and types.
package clusters

import (
	"bytes"
	"context"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrOutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunGet(t *testing.T) {
	tests := []struct {
		name            string
		clusterName     string
		getCluster      instrumentation.Cluster
		getErr          error
		monClusters     []instrumentation.ClusterObservedState
		monErr          error
		pipelines       []instrumentation.Pipeline
		pipeErr         error
		wantErr         bool
		wantDetailedErr bool
		wantErrSummary  string
		wantExitCode    *int
		wantStatus      instrumentation.InstrumentationStatus
		wantInOutput    string
	}{
		{
			name:        "cluster found in monitoring — status from RunK8sMonitoring",
			clusterName: "prod-eu",
			getCluster: instrumentation.Cluster{
				Name:          "prod-eu",
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolVal(true), //nolint:modernize
				ClusterEvents: boolVal(true), //nolint:modernize
			},
			monClusters: []instrumentation.ClusterObservedState{
				{
					Name:                  "prod-eu",
					InstrumentationStatus: instrumentation.StatusInstrumented,
					Workloads:             3,
				},
			},
			wantStatus:   instrumentation.StatusInstrumented,
			wantInOutput: "prod-eu",
		},
		{
			name:        "cluster absent from monitoring — pipeline fallback PENDING",
			clusterName: "new-cluster",
			getCluster: instrumentation.Cluster{
				Name:      "new-cluster",
				Selection: "SELECTION_INCLUDED",
			},
			monClusters: nil,
			pipelines:   []instrumentation.Pipeline{makeK8sPipeline("new-cluster")},
			wantStatus:  instrumentation.StatusPendingInstrumentation,
		},
		{
			name:        "cluster absent from monitoring — no pipeline — NOT_INSTRUMENTED",
			clusterName: "ghost-cluster",
			getCluster: instrumentation.Cluster{
				Name:      "ghost-cluster",
				Selection: "SELECTION_INCLUDED",
			},
			monClusters: nil,
			pipelines:   nil,
			wantStatus:  instrumentation.StatusNotInstrumented,
		},
		{
			name:        "GetK8SInstrumentation error propagates",
			clusterName: "prod-eu",
			getErr:      assert.AnError,
			wantErr:     true,
		},
		{
			name:        "RunK8sMonitoring error propagates",
			clusterName: "prod-eu",
			getCluster: instrumentation.Cluster{
				Name:      "prod-eu",
				Selection: "SELECTION_INCLUDED",
			},
			monErr:  assert.AnError,
			wantErr: true,
		},
		{
			name:        "unknown cluster — backend returns zero-valued proto — exit 1 DetailedError",
			clusterName: "unknown-cluster",
			// zero-valued Cluster: Selection == "", all flags nil — matches IsEmptyDefaultCluster
			getCluster:      instrumentation.Cluster{},
			wantErr:         true,
			wantDetailedErr: true,
			wantErrSummary:  "Resource not found",
			wantExitCode:    intVal(1), //nolint:modernize
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{
				GetK8SInstrumentationFn: func(_ context.Context, name string) (*instrumentation.GetK8SInstrumentationResponse, error) {
					if tt.getErr != nil {
						return nil, tt.getErr
					}
					c := tt.getCluster
					c.Name = name
					return &instrumentation.GetK8SInstrumentationResponse{Cluster: c}, nil
				},
				RunK8sMonitoringFn: func(_ context.Context, _ instrumentation.PromHeaders) (*instrumentation.RunK8sMonitoringResponse, error) {
					if tt.monErr != nil {
						return nil, tt.monErr
					}
					return &instrumentation.RunK8sMonitoringResponse{Clusters: tt.monClusters}, nil
				},
				ListPipelinesFn: func(_ context.Context) ([]instrumentation.Pipeline, error) {
					if tt.pipeErr != nil {
						return nil, tt.pipeErr
					}
					return tt.pipelines, nil
				},
			}

			opts := &getOpts{}
			opts.IO.RegisterCustomCodec("table", &instrOutput.ClusterTableCodec{Wide: false})
			opts.IO.RegisterCustomCodec("wide", &instrOutput.ClusterTableCodec{Wide: true})
			opts.IO.DefaultFormat("table")
			opts.IO.OutputFormat = "json"

			var buf bytes.Buffer
			err := runGet(
				context.Background(),
				opts,
				client,
				tt.clusterName,
				instrumentation.PromHeaders{},
				&buf,
			)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantDetailedErr {
					var de *fail.DetailedError
					require.ErrorAs(t, err, &de, "expected *fail.DetailedError, got %T: %v", err, err)
					assert.Equal(t, tt.wantErrSummary, de.Summary)
					if tt.wantExitCode != nil {
						require.NotNil(t, de.ExitCode, "expected non-nil ExitCode")
						assert.Equal(t, *tt.wantExitCode, *de.ExitCode)
					}
				}
				return
			}
			require.NoError(t, err)

			out := buf.String()
			if tt.wantStatus != "" {
				assert.Contains(t, out, string(tt.wantStatus),
					"expected status %q in output:\n%s", tt.wantStatus, out)
			}
			if tt.wantInOutput != "" {
				assert.Contains(t, out, tt.wantInOutput,
					"expected %q in output:\n%s", tt.wantInOutput, out)
			}
		})
	}
}

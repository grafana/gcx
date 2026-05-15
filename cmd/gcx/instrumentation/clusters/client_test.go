//nolint:testpackage // white-box testing: tests access unexported run* functions and clusterClient interface.
package clusters

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

// fakeClient implements clusterClient for tests.
type fakeClient struct {
	GetK8SInstrumentationFn func(ctx context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error)
	SetK8SInstrumentationFn func(ctx context.Context, clusterName string, k8s instrumentation.Cluster, urls instrumentation.BackendURLs) error
	RunK8sMonitoringFn      func(ctx context.Context, promHeaders instrumentation.PromHeaders) (*instrumentation.RunK8sMonitoringResponse, error)
	ListPipelinesFn         func(ctx context.Context) ([]instrumentation.Pipeline, error)
}

func (f *fakeClient) GetK8SInstrumentation(ctx context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error) {
	if f.GetK8SInstrumentationFn != nil {
		return f.GetK8SInstrumentationFn(ctx, clusterName)
	}
	return nil, errors.New("fakeClient: GetK8SInstrumentation not configured")
}

func (f *fakeClient) SetK8SInstrumentation(ctx context.Context, clusterName string, k8s instrumentation.Cluster, urls instrumentation.BackendURLs) error {
	if f.SetK8SInstrumentationFn != nil {
		return f.SetK8SInstrumentationFn(ctx, clusterName, k8s, urls)
	}
	return errors.New("fakeClient: SetK8SInstrumentation not configured")
}

func (f *fakeClient) RunK8sMonitoring(ctx context.Context, promHeaders instrumentation.PromHeaders) (*instrumentation.RunK8sMonitoringResponse, error) {
	if f.RunK8sMonitoringFn != nil {
		return f.RunK8sMonitoringFn(ctx, promHeaders)
	}
	return nil, errors.New("fakeClient: RunK8sMonitoring not configured")
}

func (f *fakeClient) ListPipelines(ctx context.Context) ([]instrumentation.Pipeline, error) {
	if f.ListPipelinesFn != nil {
		return f.ListPipelinesFn(ctx)
	}
	return nil, errors.New("fakeClient: ListPipelines not configured")
}

// fakeMonitoringClient implements enumerate.MonitoringClient for testing.
// Uses a function field so tests can return different results per call.
type fakeMonitoringClient struct {
	RunK8sMonitoringFn func(ctx context.Context) ([]instrumentation.ClusterObservedState, error)
}

func (f *fakeMonitoringClient) RunK8sMonitoring(ctx context.Context) ([]instrumentation.ClusterObservedState, error) {
	if f.RunK8sMonitoringFn != nil {
		return f.RunK8sMonitoringFn(ctx)
	}
	return nil, errors.New("fakeMonitoringClient: RunK8sMonitoringFn not configured")
}

// fakePipelineClient implements enumerate.PipelineClient for testing.
type fakePipelineClient struct {
	Pipelines []instrumentation.Pipeline
	Err       error
}

func (f *fakePipelineClient) ListPipelines(_ context.Context) ([]instrumentation.Pipeline, error) {
	return f.Pipelines, f.Err
}

// intVal returns a pointer to an int literal, for use in test table literals.
func intVal(i int) *int { return &i } //nolint:modernize // returns pointer to value, not zero pointer like new()

// makeK8sPipeline creates a fake K8s monitoring pipeline for a given cluster.
func makeK8sPipeline(clusterName string) instrumentation.Pipeline {
	return instrumentation.Pipeline{
		ID:   "pipe-" + clusterName,
		Name: "k8s-monitoring-" + clusterName,
		Metadata: map[string]any{
			"type":    "k8s_monitoring",
			"cluster": clusterName,
		},
	}
}

func TestPipelineFallbackStatus_LiberalKeys(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		pipelines   []instrumentation.Pipeline
		want        instrumentation.InstrumentationStatus
	}{
		{
			name:        "cluster key matches",
			clusterName: "prod-eu",
			pipelines: []instrumentation.Pipeline{
				{Metadata: map[string]any{"type": "k8s_monitoring", "cluster": "prod-eu"}},
			},
			want: instrumentation.StatusPendingInstrumentation,
		},
		{
			name:        "cluster_name key matches",
			clusterName: "prod-eu",
			pipelines: []instrumentation.Pipeline{
				{Metadata: map[string]any{"type": "k8s_monitoring", "cluster_name": "prod-eu"}},
			},
			want: instrumentation.StatusPendingInstrumentation,
		},
		{
			name:        "no matching cluster returns not instrumented",
			clusterName: "prod-eu",
			pipelines: []instrumentation.Pipeline{
				{Metadata: map[string]any{"type": "k8s_monitoring", "cluster_name": "other"}},
			},
			want: instrumentation.StatusNotInstrumented,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pipelineFallbackStatus(tc.clusterName, tc.pipelines)
			if got != tc.want {
				t.Errorf("pipelineFallbackStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

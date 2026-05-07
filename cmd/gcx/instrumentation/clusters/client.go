package clusters

import (
	"context"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

// clusterClient is the minimal interface required by cluster commands.
// Satisfied by *instrumentation.Client created via instrumentation.NewClient.
// Used for dependency injection in tests.
type clusterClient interface {
	GetK8SInstrumentation(ctx context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error)
	SetK8SInstrumentation(ctx context.Context, clusterName string, k8s instrumentation.Cluster, urls instrumentation.BackendURLs) error
	RunK8sMonitoring(ctx context.Context, promHeaders instrumentation.PromHeaders) (*instrumentation.RunK8sMonitoringResponse, error)
	ListPipelines(ctx context.Context) ([]instrumentation.Pipeline, error)
}

// monitoringAdapter adapts a clusterClient + PromHeaders to the
// enumerate.MonitoringClient interface, which expects RunK8sMonitoring(ctx)
// without PromHeaders. PromHeaders are bound at construction time.
type monitoringAdapter struct {
	client      clusterClient
	promHeaders instrumentation.PromHeaders
}

func (a *monitoringAdapter) RunK8sMonitoring(ctx context.Context) ([]instrumentation.ClusterObservedState, error) {
	resp, err := a.client.RunK8sMonitoring(ctx, a.promHeaders)
	if err != nil {
		return nil, err
	}
	return resp.Clusters, nil
}

// declaredStateClient is the minimal interface for the wait command's
// pre-flight check: verify the cluster is declared before polling.
type declaredStateClient interface {
	GetK8SInstrumentation(ctx context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error)
}

// pipelineAdapter adapts a clusterClient to the enumerate.PipelineClient interface.
type pipelineAdapter struct {
	client clusterClient
}

func (a *pipelineAdapter) ListPipelines(ctx context.Context) ([]instrumentation.Pipeline, error) {
	return a.client.ListPipelines(ctx)
}

// boolPtr returns a pointer to b. Used when converting flag bool values to
// *bool tri-state domain fields.
//
//nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
func boolPtr(b bool) *bool { return &b }

// k8sMonitoringPipelineType is the metadata "type" value that identifies K8s
// monitoring pipelines. Mirrors the value in the enumerate package.
const k8sMonitoringPipelineType = "k8s_monitoring"

// pipelineFallbackStatus checks whether clusterName has a K8s monitoring
// pipeline in pipelines. Returns StatusPendingInstrumentation if found,
// StatusNotInstrumented if not. Used by get when the cluster is
// absent from RunK8sMonitoring.
func pipelineFallbackStatus(clusterName string, pipelines []instrumentation.Pipeline) instrumentation.InstrumentationStatus {
	for _, p := range pipelines {
		if p.Metadata == nil {
			continue
		}
		typeVal, ok := p.Metadata["type"]
		if !ok {
			continue
		}
		typeStr, ok := typeVal.(string)
		if !ok || typeStr != k8sMonitoringPipelineType {
			continue
		}
		// Accept both "cluster_name" (grafana-cloud-onboarding) and "cluster" (legacy).
		for _, key := range []string{"cluster_name", "cluster"} {
			clusterVal, ok := p.Metadata[key]
			if !ok {
				continue
			}
			clusterStr, ok := clusterVal.(string)
			if ok && clusterStr == clusterName {
				return instrumentation.StatusPendingInstrumentation
			}
		}
	}
	return instrumentation.StatusNotInstrumented
}

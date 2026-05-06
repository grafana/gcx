// Package status implements the "gcx instrumentation status" command.
// It provides a cross-cutting observed-state view combining RunK8sMonitoring,
// ListPipelines (via the enumerate helper), and — when --namespace is set —
// RunK8sDiscovery for namespace-level workload status.
package status

import (
	"context"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/enumerate"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
)

// monitoringSource is the minimal interface required to call RunK8sMonitoring
// without PromHeaders in the signature. The real *instrumentation.Client
// requires PromHeaders; the monitoringAdapter struct binds them at construction.
// Structurally identical to enumerate.MonitoringClient.
type monitoringSource interface {
	RunK8sMonitoring(ctx context.Context) ([]instrumentation.ClusterObservedState, error)
}

// pipelineSource is the minimal interface required to call ListPipelines.
// Structurally identical to enumerate.PipelineClient.
// *instrumentation.Client satisfies this interface directly.
type pipelineSource interface {
	ListPipelines(ctx context.Context) ([]instrumentation.Pipeline, error)
}

// discoverySource is the minimal interface required to call RunK8sDiscovery
// without PromHeaders in the signature. The discoveryAdapter binds them at
// construction time.
type discoverySource interface {
	RunK8sDiscovery(ctx context.Context) ([]instrumentation.DiscoveryItem, error)
}

// monitoringAdapter adapts *instrumentation.Client to the monitoringSource
// interface expected by enumerate.Enumerate. PromHeaders are bound at
// construction so callers can pass this as enumerate.MonitoringClient.
type monitoringAdapter struct {
	client      *instrumentation.Client
	promHeaders instrumentation.PromHeaders
}

// RunK8sMonitoring satisfies monitoringSource (and enumerate.MonitoringClient).
func (a *monitoringAdapter) RunK8sMonitoring(ctx context.Context) ([]instrumentation.ClusterObservedState, error) {
	resp, err := a.client.RunK8sMonitoring(ctx, a.promHeaders)
	if err != nil {
		return nil, err
	}
	return resp.Clusters, nil
}

// discoveryAdapter adapts *instrumentation.Client to the discoverySource
// interface. PromHeaders are bound at construction.
type discoveryAdapter struct {
	client      *instrumentation.Client
	promHeaders instrumentation.PromHeaders
}

// RunK8sDiscovery satisfies discoverySource.
func (a *discoveryAdapter) RunK8sDiscovery(ctx context.Context) ([]instrumentation.DiscoveryItem, error) {
	resp, err := a.client.RunK8sDiscovery(ctx, a.promHeaders)
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// run executes the status command business logic and returns the data to encode.
//
// When namespaceFilter is non-empty it returns []instroutput.ServiceView
// (workload-level view via RunK8sDiscovery, filtered by clusterFilter and
// namespaceFilter).
//
// When namespaceFilter is empty it returns []instroutput.ClusterView
// (cluster-level view via enumerate.Enumerate, optionally filtered by
// clusterFilter).
//
// Both paths use make([]T, 0) for F-AGENT-01 compliance (empty JSON array,
// not null).
func run(
	ctx context.Context,
	clusterFilter, namespaceFilter string,
	mon monitoringSource,
	pipe pipelineSource,
	disc discoverySource,
) (any, error) {
	if namespaceFilter != "" {
		return runServiceView(ctx, clusterFilter, namespaceFilter, disc)
	}
	return runClusterView(ctx, clusterFilter, mon, pipe)
}

// runClusterView builds the cluster-level view using enumerate.Enumerate.
// Pre-Alloy clusters (source = PipelineSource only) appear with
// StatusPendingInstrumentation and zero observed metrics.
func runClusterView(
	ctx context.Context,
	clusterFilter string,
	mon monitoringSource,
	pipe pipelineSource,
) ([]instroutput.ClusterView, error) {
	observed, err := enumerate.Enumerate(ctx, mon, pipe)
	if err != nil {
		return nil, err
	}

	views := make([]instroutput.ClusterView, 0, len(observed))
	for _, oc := range observed {
		if clusterFilter != "" && oc.Name != clusterFilter {
			continue
		}
		v := instroutput.ClusterView{
			Name:                  oc.Name,
			InstrumentationStatus: oc.Status,
		}
		// Populate observed metrics only when the cluster is visible to
		// RunK8sMonitoring (State is nil for pre-Alloy PipelineSource clusters).
		if oc.State != nil {
			v.Namespaces = len(oc.State.Namespaces)
			v.Workloads = oc.State.Workloads
			v.Pods = oc.State.Pods
			v.Nodes = oc.State.Nodes
		}
		views = append(views, v)
	}
	return views, nil
}

// runServiceView builds the workload-level view using RunK8sDiscovery.
// Results are filtered client-side by clusterFilter (if non-empty) and by
// namespaceFilter.
func runServiceView(
	ctx context.Context,
	clusterFilter, namespaceFilter string,
	disc discoverySource,
) ([]instroutput.ServiceView, error) {
	items, err := disc.RunK8sDiscovery(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]instroutput.ServiceView, 0, len(items))
	for _, item := range items {
		if clusterFilter != "" && item.ClusterName != clusterFilter {
			continue
		}
		if item.Namespace != namespaceFilter {
			continue
		}
		views = append(views, instroutput.ServiceView{
			ClusterName:           item.ClusterName,
			Namespace:             item.Namespace,
			Name:                  item.Name,
			WorkloadType:          item.WorkloadType,
			DisplayNamespace:      item.DisplayNamespace,
			DisplayName:           item.DisplayName,
			OS:                    item.OS,
			Lang:                  item.Lang,
			InstrumentationStatus: item.InstrumentationStatus,
		})
	}
	return views, nil
}

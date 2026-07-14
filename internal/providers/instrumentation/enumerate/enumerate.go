// Package enumerate implements the cluster-enumeration helper.
//
// RunK8sMonitoring builds its cluster set from survey_info{} Prometheus queries.
// Clusters that have been Set via SetK8SInstrumentation but whose Alloy collector
// has not yet started reporting survey_info{} are invisible to that RPC. To surface
// these "pre-Alloy" clusters, Enumerate merges RunK8sMonitoring() with
// PipelineService.ListPipelines() filtered to K8s monitoring pipelines
// (metadata["type"] == "k8s_monitoring"). Clusters present only in ListPipelines
// are returned with Status=StatusPendingInstrumentation.
//
// This helper is used by clusters list, clusters get,
// status, and clusters wait.
package enumerate

import (
	"context"
	"sort"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"golang.org/x/sync/errgroup"
)

// k8sMonitoringPipelineType is the metadata "type" value that identifies K8s
// monitoring pipelines. Matches the value emitted by the fleet-management backend
// and verified in T1 client_test.go wire fixtures.
const k8sMonitoringPipelineType = "k8s_monitoring"

// MonitoringClient is the minimal interface required by Enumerate to call
// RunK8sMonitoring. The real *instrumentation.Client satisfies this via an
// adapter that unwraps the response to a cluster slice.
type MonitoringClient interface {
	RunK8sMonitoring(ctx context.Context) ([]instrumentation.ClusterObservedState, error)
}

// PipelineClient is the minimal interface required by Enumerate to call
// ListPipelines.
type PipelineClient interface {
	ListPipelines(ctx context.Context) ([]instrumentation.Pipeline, error)
}

// Source indicates which backend source(s) surfaced an ObservedCluster.
type Source int

const (
	// MonitoringSource means the cluster was returned only by RunK8sMonitoring.
	MonitoringSource Source = iota
	// PipelineSource means the cluster was returned only by ListPipelines (pre-Alloy).
	PipelineSource
	// BothSources means the cluster appeared in both RunK8sMonitoring and ListPipelines.
	BothSources
)

// ObservedCluster is the merged view of a single cluster from both backends.
type ObservedCluster struct {
	// Name is the cluster name.
	Name string
	// Status is the observed instrumentation status. Clusters that are present
	// only in pipeline state are assigned StatusPendingInstrumentation.
	Status instrumentation.InstrumentationStatus
	// State is populated from RunK8sMonitoring when the cluster is visible to
	// that RPC. Nil for pre-Alloy clusters (Source == PipelineSource).
	State *instrumentation.ClusterObservedState
	// Source indicates which backend surfaced this cluster.
	Source Source
}

// Enumerate merges the cluster sets from RunK8sMonitoring and ListPipelines,
// returning a unified []ObservedCluster sorted alphabetically by cluster name.
//
// Both RPCs are called concurrently. Clusters present only in ListPipelines
// (whose pipeline metadata["type"] == "k8s_monitoring" and metadata["cluster"]
// or metadata["cluster_name"] is non-empty) are added with
// Status=StatusPendingInstrumentation and Source=PipelineSource. Clusters
// present in both sources have Source=BothSources and their status is taken
// from RunK8sMonitoring.
func Enumerate(ctx context.Context, mon MonitoringClient, pipe PipelineClient) ([]ObservedCluster, error) {
	var (
		monClusters   []instrumentation.ClusterObservedState
		pipePipelines []instrumentation.Pipeline
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(2)

	g.Go(func() error {
		var err error
		monClusters, err = mon.RunK8sMonitoring(gctx)
		return err
	})

	g.Go(func() error {
		var err error
		pipePipelines, err = pipe.ListPipelines(gctx)
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Build the result map keyed by cluster name from RunK8sMonitoring.
	clusterMap := make(map[string]*ObservedCluster, len(monClusters))
	for i := range monClusters {
		c := &monClusters[i]
		clusterMap[c.Name] = &ObservedCluster{
			Name:   c.Name,
			Status: c.InstrumentationStatus,
			State:  c,
			Source: MonitoringSource,
		}
	}

	// Merge K8s monitoring pipelines from ListPipelines. A pipeline is a K8s
	// monitoring pipeline if it has metadata["type"] == "k8s_monitoring" and
	// metadata["cluster_name"] or metadata["cluster"] is a non-empty string.
	// Pipelines not matching this filter are ignored (e.g. Beyla survey
	// pipelines, app pipelines, etc.).
	for _, p := range pipePipelines {
		clusterName, ok := k8sMonitoringClusterName(p)
		if !ok {
			continue
		}

		if existing, found := clusterMap[clusterName]; found {
			// Cluster already visible in RunK8sMonitoring — mark as both.
			existing.Source = BothSources
		} else {
			// Pre-Alloy cluster: Set but not yet reporting survey_info.
			clusterMap[clusterName] = &ObservedCluster{
				Name:   clusterName,
				Status: instrumentation.StatusPendingInstrumentation,
				State:  nil,
				Source: PipelineSource,
			}
		}
	}

	// Collect and sort alphabetically by cluster name.
	result := make([]ObservedCluster, 0, len(clusterMap))
	for _, oc := range clusterMap {
		result = append(result, *oc)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// k8sMonitoringClusterName returns the cluster name from a pipeline's metadata
// if it is a K8s monitoring pipeline. Accepts both "cluster_name"
// (grafana-cloud-onboarding v0.4.x) and "cluster" (legacy) as the cluster-name
// metadata key, with "cluster_name" taking precedence.
func k8sMonitoringClusterName(p instrumentation.Pipeline) (string, bool) {
	if p.Metadata == nil {
		return "", false
	}

	pipeType, ok := p.Metadata["type"]
	if !ok {
		return "", false
	}
	typeStr, ok := pipeType.(string)
	if !ok || typeStr != k8sMonitoringPipelineType {
		return "", false
	}

	// Accept "cluster_name" (new, grafana-cloud-onboarding) first,
	// then fall back to "cluster" (legacy).
	for _, key := range []string{"cluster_name", "cluster"} {
		val, ok := p.Metadata[key]
		if !ok {
			continue
		}
		name, ok := val.(string)
		if ok && name != "" {
			return name, true
		}
	}

	return "", false
}

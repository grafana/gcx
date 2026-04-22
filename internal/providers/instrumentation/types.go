// Package instrumentation provides the top-level gcx instrumentation provider
// with Cluster and App resource kinds under instrumentation.grafana.app/v1alpha1.
package instrumentation

import (
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// Group is the API group for instrumentation resources.
	Group = "instrumentation.grafana.app"
	// Version is the API version for instrumentation resources.
	Version = "v1alpha1"
)

// ClusterGVK is the GroupVersionKind for the Cluster resource kind.
var ClusterGVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: "Cluster"} //nolint:gochecknoglobals

// AppGVK is the GroupVersionKind for the App resource kind.
var AppGVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: "App"} //nolint:gochecknoglobals

// Compile-time assertions that Cluster and App satisfy the ResourceIdentity contract.
var _ adapter.ResourceIdentity = (*Cluster)(nil)
var _ adapter.ResourceIdentity = (*App)(nil)

// AppDisplayName returns the display name (metadata.name) for an App resource
// from its natural key fields. The result is used for display only and MUST NOT
// be parsed to recover cluster or namespace identity.
func AppDisplayName(cluster, namespace string) string {
	return cluster + "-" + namespace
}

// Cluster is the spec type for the Cluster resource kind. It holds
// Kubernetes-level monitoring configuration for a single cluster.
// metadata.name is the cluster identifier (natural key).
//
//nolint:recvcheck // Mixed receivers required for Go generics TypedCRUD compatibility.
type Cluster struct {
	// name tracks the cluster identifier from metadata.name; not serialised into spec JSON.
	name string

	Selection     string `json:"selection,omitempty"`
	CostMetrics   bool   `json:"costMetrics"`
	ClusterEvents bool   `json:"clusterEvents"`
	EnergyMetrics bool   `json:"energyMetrics"`
	NodeLogs      bool   `json:"nodeLogs"`

	// Monitoring-only fields populated by list operations from RunK8sMonitoring.
	// Not serialised into spec JSON (json:"-") — display-only.
	Status    string `json:"-"`
	Workloads int    `json:"-"`
	Pods      int    `json:"-"`
	Nodes     int    `json:"-"`
}

// GetResourceName returns the cluster identifier (from metadata.name).
func (c Cluster) GetResourceName() string { return c.name }

// SetResourceName restores the cluster identifier from metadata.name after
// an unstructured round-trip. Never call this to parse app-level identity.
func (c *Cluster) SetResourceName(name string) { c.name = name }

// App is the spec type for the App resource kind. It holds Beyla application
// instrumentation configuration for a single (cluster, namespace) pair.
// Natural key is the composite (spec.cluster, spec.namespace).
// metadata.name is display-only: AppDisplayName(spec.cluster, spec.namespace).
//
//nolint:recvcheck // Mixed receivers required for Go generics TypedCRUD compatibility.
type App struct {
	Cluster         string      `json:"cluster"`
	Namespace       string      `json:"namespace"`
	Selection       string      `json:"selection,omitempty"`
	Tracing         bool        `json:"tracing,omitempty"`
	Logging         bool        `json:"logging,omitempty"`
	ProcessMetrics  bool        `json:"processMetrics,omitempty"`
	ExtendedMetrics bool        `json:"extendedMetrics,omitempty"`
	Profiling       bool        `json:"profiling,omitempty"`
	Apps            []AppConfig `json:"apps,omitempty"`
}

// GetResourceName returns the display name derived from (spec.cluster, spec.namespace).
// This value is display-only; identity is always derived from spec fields.
func (a App) GetResourceName() string { return AppDisplayName(a.Cluster, a.Namespace) }

// SetResourceName is a no-op for App. The App's identity is authoritative from
// spec.cluster and spec.namespace, which round-trip naturally as spec fields.
// NEVER parse metadata.name to recover cluster or namespace.
func (a *App) SetResourceName(_ string) {}

// AppConfig represents a per-workload instrumentation override within a namespace.
type AppConfig struct {
	Name      string `json:"name"`
	Selection string `json:"selection"`
	Type      string `json:"type,omitempty"`
}

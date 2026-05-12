package instrumentation

// InstrumentationStatus is the proto enum value for the observed state-machine
// status of a cluster or workload. Values match the fleet-management proto enum
// directly so they can be forwarded to JSON/YAML output without translation.
// Use the typed constants below rather than raw strings.
type InstrumentationStatus string

const (
	// StatusPendingInstrumentation is emitted when a pipeline is configured but
	// the Alloy collector has not yet started reporting (kube_node_info / target_info).
	StatusPendingInstrumentation InstrumentationStatus = "PENDING_INSTRUMENTATION"
	// StatusInstrumented is emitted when the collector is actively reporting telemetry.
	StatusInstrumented InstrumentationStatus = "INSTRUMENTED"
	// StatusPendingUninstrumentation is emitted when instrumentation has been disabled
	// but the collector has not yet observed the pipeline deletion.
	StatusPendingUninstrumentation InstrumentationStatus = "PENDING_UNINSTRUMENTATION"
	// StatusNotInstrumented is the default state: no pipeline configured, no collector reporting.
	StatusNotInstrumented InstrumentationStatus = "NOT_INSTRUMENTED"
	// StatusExcluded is set when the user explicitly excluded the cluster via Selection=EXCLUDED.
	StatusExcluded InstrumentationStatus = "EXCLUDED"
	// StatusError is defined in the proto but reserved — never set by the current backend.
	// wait commands MUST exit non-zero if this status is observed.
	StatusError InstrumentationStatus = "INSTRUMENTATION_ERROR"
)

// App represents the namespace-level instrumentation configuration for a cluster.
// App identity in all commands is positional (cluster, namespace) — no metadata.name
// requirement. NO Selection field. Tri-state *bool fields
// model "unset / on / off" for read-modify-write flag preservation.
type App struct {
	// Name is the Kubernetes namespace name.
	Name string `json:"name" yaml:"name"`
	// Autoinstrument is the primary on/off knob for namespace-level Beyla
	// instrumentation. enable always sets this to true.
	Autoinstrument *bool `json:"autoInstrument,omitempty" yaml:"autoInstrument,omitempty"`
	// Tracing controls distributed tracing collection.
	Tracing *bool `json:"tracing,omitempty" yaml:"tracing,omitempty"`
	// Logging controls log collection.
	Logging *bool `json:"logging,omitempty" yaml:"logging,omitempty"`
	// ProcessMetrics controls process-level metrics collection.
	ProcessMetrics *bool `json:"processMetrics,omitempty" yaml:"processMetrics,omitempty"`
	// ExtendedMetrics controls extended Beyla metrics collection.
	ExtendedMetrics *bool `json:"extendedMetrics,omitempty" yaml:"extendedMetrics,omitempty"`
	// Profiling controls continuous profiling collection.
	Profiling *bool `json:"profiling,omitempty" yaml:"profiling,omitempty"`
	// Apps holds per-workload selection overrides within this namespace.
	// These are the knobs driven by services include/exclude/clear.
	Apps []AppOverride `json:"apps,omitempty" yaml:"apps,omitempty"`
}

// AppOverride represents a per-workload selection override within a namespace.
// Unlike App, AppOverride retains Selection because the per-workload override IS
// used by the backend (pkg/instrumentation/v1/k8s_beyla.go) — only the namespace-level
// Selection is ignored (spec §Backend ground truth).
type AppOverride struct {
	// Name is the workload name.
	Name string `json:"name" yaml:"name"`
	// Selection is the proto enum value: SELECTION_INCLUDED or SELECTION_EXCLUDED.
	Selection string `json:"selection" yaml:"selection"`
}

// Cluster represents the K8s monitoring configuration for a cluster.
// Selection is retained for serialization correctness and round-trip RMW writes;
// it must not be user-controllable via flags. Selection appears in JSON and
// YAML output but NOT in text/wide table output.
type Cluster struct {
	// Name is the cluster name.
	Name string `json:"name" yaml:"name"`
	// Selection is the backend control field (SELECTION_INCLUDED / SELECTION_EXCLUDED).
	// Retained for round-trip correctness; not exposed as a flag; always set by the
	// backend and preserved on writes. Never omitempty — always serialized.
	Selection string `json:"selection" yaml:"selection"`
	// CostMetrics controls Kubernetes cost metrics collection.
	CostMetrics *bool `json:"costMetrics,omitempty" yaml:"costMetrics,omitempty"`
	// EnergyMetrics controls energy/power metrics collection.
	EnergyMetrics *bool `json:"energyMetrics,omitempty" yaml:"energyMetrics,omitempty"`
	// ClusterEvents controls Kubernetes cluster-event collection.
	ClusterEvents *bool `json:"clusterEvents,omitempty" yaml:"clusterEvents,omitempty"`
	// NodeLogs controls node-level log collection.
	NodeLogs *bool `json:"nodeLogs,omitempty" yaml:"nodeLogs,omitempty"`
}

// NamespaceObservedState holds the observed instrumentation state for a single
// namespace, populated from RunK8sDiscovery / RunK8sMonitoring. Used for status
// and wait commands.
type NamespaceObservedState struct {
	// Name is the Kubernetes namespace name.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// InstrumentationStatus is the proto state-machine value.
	InstrumentationStatus InstrumentationStatus `json:"instrumentationStatus,omitempty" yaml:"instrumentationStatus,omitempty"`
	// InstrumentationErrorMessage is the error message when status is ERROR.
	InstrumentationErrorMessage string `json:"instrumentationErrorMessage,omitempty" yaml:"instrumentationErrorMessage,omitempty"`
	// Workloads is the number of observed workloads in this namespace.
	Workloads int `json:"workloads,omitempty" yaml:"workloads,omitempty"`
	// Pods is the number of observed pods in this namespace.
	Pods int `json:"pods,omitempty" yaml:"pods,omitempty"`
}

// ClusterObservedState holds the observed instrumentation state for a single
// cluster, populated from RunK8sMonitoring. Used for status and wait commands.
type ClusterObservedState struct {
	// Name is the cluster name.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// InstrumentationStatus is the proto state-machine value.
	InstrumentationStatus InstrumentationStatus `json:"instrumentationStatus,omitempty" yaml:"instrumentationStatus,omitempty"`
	// InstrumentationErrorMessage is the error message when status is ERROR.
	InstrumentationErrorMessage string `json:"instrumentationErrorMessage,omitempty" yaml:"instrumentationErrorMessage,omitempty"`
	// Namespaces holds per-namespace observed state.
	Namespaces []NamespaceObservedState `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	// Nodes is the number of observed nodes in this cluster.
	Nodes int `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	// Workloads is the number of observed workloads in this cluster.
	Workloads int `json:"workloads,omitempty" yaml:"workloads,omitempty"`
	// Pods is the number of observed pods in this cluster.
	Pods int `json:"pods,omitempty" yaml:"pods,omitempty"`
}

// DiscoveryItem represents a single discovered workload from RunK8sDiscovery.
// Used by services list/get.
type DiscoveryItem struct {
	// ClusterName is the cluster this workload belongs to.
	ClusterName string `json:"clusterName,omitempty" yaml:"clusterName,omitempty"`
	// Namespace is the Kubernetes namespace.
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// Name is the workload name.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// WorkloadType is the Kubernetes workload kind (e.g. "deployment", "daemonset").
	WorkloadType string `json:"workloadType,omitempty" yaml:"workloadType,omitempty"`
	// DisplayNamespace is the human-friendly namespace display name.
	DisplayNamespace string `json:"displayNamespace,omitempty" yaml:"displayNamespace,omitempty"`
	// DisplayName is the human-friendly workload display name.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	// OS is the detected operating system.
	OS string `json:"os,omitempty" yaml:"os,omitempty"`
	// Lang is the detected programming language.
	Lang string `json:"lang,omitempty" yaml:"lang,omitempty"`
	// InstrumentationStatus is the proto state-machine value for this workload.
	InstrumentationStatus InstrumentationStatus `json:"instrumentationStatus,omitempty" yaml:"instrumentationStatus,omitempty"`
}

// Pipeline is a local representation of a fleet-management pipeline, used by
// the enumerate helper (T5) to detect clusters that have been Set but are not
// yet reporting survey_info. This type mirrors fleet.Pipeline
// but lives in this package to avoid a cross-provider import.
type Pipeline struct {
	// ID is the pipeline's unique identifier.
	ID string `json:"id,omitempty"`
	// Name is the human-readable pipeline name.
	Name string `json:"name,omitempty"`
	// Enabled reports whether the pipeline is active.
	Enabled *bool `json:"enabled,omitempty"`
	// Metadata holds arbitrary key/value pairs used for pipeline categorization.
	// The enumerate helper filters by K8s monitoring pipeline metadata keys.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// IsEmptyDefaultCluster returns true when the backend returned a fully
// zero-valued K8SInstrumentation response for an unknown cluster name.
// The backend returns HTTP 200 with a zero-valued proto for unknown
// identifiers, so the client must detect the not-found case itself.
//
// Detection: selection is empty AND every flag is nil or false.
// A non-empty selection or any flag set to true is a positive registration signal.
func IsEmptyDefaultCluster(c Cluster) bool {
	if c.Selection != "" {
		return false
	}
	for _, b := range []*bool{c.CostMetrics, c.EnergyMetrics, c.ClusterEvents, c.NodeLogs} {
		if b != nil && *b {
			return false
		}
	}
	return true
}

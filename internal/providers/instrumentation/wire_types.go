package instrumentation

// NamespaceConfig is the wire-format namespace instrumentation configuration
// used in Connect API payloads. JSON tags match the Connect API contract
// (lowercase, no camelCase) and MUST NOT be changed without a backend contract change.
type NamespaceConfig struct {
	Name            string      `json:"name" yaml:"name"`
	Selection       string      `json:"selection" yaml:"selection"`
	Tracing         bool        `json:"tracing" yaml:"tracing"`
	ProcessMetrics  bool        `json:"processmetrics" yaml:"processmetrics"`
	ExtendedMetrics bool        `json:"extendedmetrics" yaml:"extendedmetrics"`
	Logging         bool        `json:"logging" yaml:"logging"`
	Profiling       bool        `json:"profiling" yaml:"profiling"`
	Apps            []AppConfig `json:"apps,omitempty" yaml:"apps,omitempty"`
}

// AppSpec is the application-level instrumentation configuration
// sent to and received from the Connect API.
type AppSpec struct {
	Namespaces []NamespaceConfig `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
}

// K8sSpec is the Kubernetes-level monitoring configuration
// sent to and received from the Connect API.
// JSON tags are lowercase to match the Connect API contract.
type K8sSpec struct {
	Selection     string `json:"selection,omitempty" yaml:"selection,omitempty"`
	CostMetrics   bool   `json:"costmetrics" yaml:"costmetrics"`
	EnergyMetrics bool   `json:"energymetrics" yaml:"energymetrics"`
	ClusterEvents bool   `json:"clusterevents" yaml:"clusterevents"`
	NodeLogs      bool   `json:"nodelogs" yaml:"nodelogs"`
}

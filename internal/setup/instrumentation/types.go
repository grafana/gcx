package instrumentation

import (
	"errors"
	"fmt"
)

const (
	// APIVersion is the required apiVersion for InstrumentationConfig manifests.
	APIVersion = "setup.grafana.app/v1alpha1"
	// Kind is the required kind for InstrumentationConfig manifests.
	Kind = "InstrumentationConfig"
)

// Metadata holds the identity fields for an InstrumentationConfig manifest.
type Metadata struct {
	Name string `json:"name" yaml:"name"`
}

// AppConfig represents a per-workload override within a namespace.
// Signal toggles (tracing, logging, etc.) are NOT supported at the workload
// level — signals are configured at namespace level only (FR-027).
type AppConfig struct {
	Name      string `json:"name" yaml:"name"`
	Selection string `json:"selection" yaml:"selection"`
	Type      string `json:"type,omitempty" yaml:"type,omitempty"`
}

// NamespaceConfig configures instrumentation for a specific namespace.
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

// AppSpec holds the application-level instrumentation configuration.
type AppSpec struct {
	Namespaces []NamespaceConfig `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
}

// K8sSpec holds the Kubernetes-level monitoring configuration.
type K8sSpec struct {
	CostMetrics   bool `json:"costmetrics" yaml:"costmetrics"`
	EnergyMetrics bool `json:"energymetrics" yaml:"energymetrics"`
	ClusterEvents bool `json:"clusterevents" yaml:"clusterevents"`
	NodeLogs      bool `json:"nodelogs" yaml:"nodelogs"`
}

// InstrumentationSpec is the spec section of an InstrumentationConfig.
type InstrumentationSpec struct {
	App *AppSpec `json:"app,omitempty" yaml:"app,omitempty"`
	K8s *K8sSpec `json:"k8s,omitempty" yaml:"k8s,omitempty"`
}

// InstrumentationConfig is the declarative manifest for Grafana Instrumentation Hub
// configuration. apiVersion must be setup.grafana.app/v1alpha1, kind must be
// InstrumentationConfig, and metadata.name is the cluster name.
//
// This is a plain Go struct — NOT a K8s resource (NC-002).
// It MUST NOT contain datasource URLs, instance IDs, or API tokens (NC-003).
type InstrumentationConfig struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string              `json:"kind" yaml:"kind"`
	Metadata   Metadata            `json:"metadata" yaml:"metadata"`
	Spec       InstrumentationSpec `json:"spec" yaml:"spec"`
}

// Validate checks that the manifest has the correct apiVersion, kind, and a
// non-empty metadata.name (cluster name). Returns an error on the first violation.
func (c *InstrumentationConfig) Validate() error {
	if c.APIVersion != APIVersion {
		return fmt.Errorf("invalid apiVersion %q: must be %q", c.APIVersion, APIVersion)
	}
	if c.Kind != Kind {
		return fmt.Errorf("invalid kind %q: must be %q", c.Kind, Kind)
	}
	if c.Metadata.Name == "" {
		return errors.New("metadata.name (cluster name) is required")
	}
	return nil
}

package instrumentation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/fleet"
)

// Connect endpoint paths for the instrumentation and discovery services.
const (
	pathGetAppInstrumentation = "/instrumentation.v1.InstrumentationService/GetAppInstrumentation"
	pathSetAppInstrumentation = "/instrumentation.v1.InstrumentationService/SetAppInstrumentation"
	pathGetK8SInstrumentation = "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation"
	pathSetK8SInstrumentation = "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation"
	pathSetupK8sDiscovery     = "/discovery.v1.DiscoveryService/SetupK8sDiscovery"
	pathRunK8sDiscovery       = "/discovery.v1.DiscoveryService/RunK8sDiscovery"
	pathRunK8sMonitoring      = "/discovery.v1.DiscoveryService/RunK8sMonitoring"
)

// Client is the instrumentation-specific HTTP client built on top of the
// shared fleet base client. It adds methods for all instrumentation.v1 and
// discovery.v1 Connect endpoints. No connectrpc library is used (NC-006).
type Client struct {
	fleet *fleet.Client
}

// NewClient creates a new instrumentation Client using the provided fleet base client.
func NewClient(f *fleet.Client) *Client {
	return &Client{fleet: f}
}

// --- Request / response types ---

type getAppInstrumentationRequest struct {
	ClusterName string `json:"clusterName"`
}

// GetAppInstrumentationResponse is the response from GetAppInstrumentation.
type GetAppInstrumentationResponse struct {
	Namespaces []NamespaceConfig `json:"namespaces,omitempty"`
}

type setAppInstrumentationRequest struct {
	ClusterName string            `json:"clusterName"`
	Namespaces  []NamespaceConfig `json:"namespaces,omitempty"`
}

type getK8SInstrumentationRequest struct {
	ClusterName string `json:"clusterName"`
}

// GetK8SInstrumentationResponse is the response from GetK8SInstrumentation.
type GetK8SInstrumentationResponse struct {
	CostMetrics   bool `json:"costmetrics"`
	EnergyMetrics bool `json:"energymetrics"`
	ClusterEvents bool `json:"clusterevents"`
	NodeLogs      bool `json:"nodelogs"`
}

type setK8SInstrumentationRequest struct {
	ClusterName   string `json:"clusterName"`
	CostMetrics   bool   `json:"costmetrics"`
	EnergyMetrics bool   `json:"energymetrics"`
	ClusterEvents bool   `json:"clusterevents"`
	NodeLogs      bool   `json:"nodelogs"`
}

type setupK8sDiscoveryRequest struct {
	ClusterName string `json:"clusterName"`
}

type runK8sDiscoveryRequest struct {
	ClusterName string `json:"clusterName"`
}

// DiscoveredApp represents a discovered workload within a namespace.
type DiscoveredApp struct {
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	State string `json:"state,omitempty"`
}

// DiscoveredNamespace represents a discovered namespace and its workloads.
type DiscoveredNamespace struct {
	Name string          `json:"name,omitempty"`
	Apps []DiscoveredApp `json:"apps,omitempty"`
}

// RunK8sDiscoveryResponse holds discovered workloads returned by RunK8sDiscovery.
type RunK8sDiscoveryResponse struct {
	Namespaces []DiscoveredNamespace `json:"namespaces,omitempty"`
}

// ClusterState holds the instrumentation state for a single cluster.
type ClusterState struct {
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
}

// RunK8sMonitoringResponse holds per-cluster monitoring state.
type RunK8sMonitoringResponse struct {
	Clusters []ClusterState `json:"clusters,omitempty"`
}

// --- Client methods ---

// GetAppInstrumentation retrieves the app instrumentation configuration for the given cluster.
func (c *Client) GetAppInstrumentation(ctx context.Context, clusterName string) (*GetAppInstrumentationResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathGetAppInstrumentation, getAppInstrumentationRequest{ClusterName: clusterName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetAppInstrumentation: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}

	var result GetAppInstrumentationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("GetAppInstrumentation: decode response: %w", err)
	}
	return &result, nil
}

// SetAppInstrumentation sets app instrumentation configuration for the given cluster.
func (c *Client) SetAppInstrumentation(ctx context.Context, clusterName string, namespaces []NamespaceConfig) error {
	resp, err := c.fleet.DoRequest(ctx, pathSetAppInstrumentation, setAppInstrumentationRequest{
		ClusterName: clusterName,
		Namespaces:  namespaces,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SetAppInstrumentation: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}
	return nil
}

// GetK8SInstrumentation retrieves the K8s monitoring configuration for the given cluster.
func (c *Client) GetK8SInstrumentation(ctx context.Context, clusterName string) (*GetK8SInstrumentationResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathGetK8SInstrumentation, getK8SInstrumentationRequest{ClusterName: clusterName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetK8SInstrumentation: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}

	var result GetK8SInstrumentationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("GetK8SInstrumentation: decode response: %w", err)
	}
	return &result, nil
}

// SetK8SInstrumentation sets K8s monitoring configuration for the given cluster.
func (c *Client) SetK8SInstrumentation(ctx context.Context, clusterName string, k8s K8sSpec) error {
	resp, err := c.fleet.DoRequest(ctx, pathSetK8SInstrumentation, setK8SInstrumentationRequest{
		ClusterName:   clusterName,
		CostMetrics:   k8s.CostMetrics,
		EnergyMetrics: k8s.EnergyMetrics,
		ClusterEvents: k8s.ClusterEvents,
		NodeLogs:      k8s.NodeLogs,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SetK8SInstrumentation: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}
	return nil
}

// SetupK8sDiscovery initializes K8s discovery for the given cluster.
func (c *Client) SetupK8sDiscovery(ctx context.Context, clusterName string) error {
	resp, err := c.fleet.DoRequest(ctx, pathSetupK8sDiscovery, setupK8sDiscoveryRequest{ClusterName: clusterName})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SetupK8sDiscovery: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}
	return nil
}

// RunK8sDiscovery executes discovery for the given cluster and returns discovered workloads.
func (c *Client) RunK8sDiscovery(ctx context.Context, clusterName string) (*RunK8sDiscoveryResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathRunK8sDiscovery, runK8sDiscoveryRequest{ClusterName: clusterName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RunK8sDiscovery: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}

	var result RunK8sDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("RunK8sDiscovery: decode response: %w", err)
	}
	return &result, nil
}

// RunK8sMonitoring retrieves the instrumentation monitoring state for all clusters.
// The request body is empty per the API contract.
func (c *Client) RunK8sMonitoring(ctx context.Context) (*RunK8sMonitoringResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathRunK8sMonitoring, struct{}{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RunK8sMonitoring: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}

	var result RunK8sMonitoringResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("RunK8sMonitoring: decode response: %w", err)
	}
	return &result, nil
}

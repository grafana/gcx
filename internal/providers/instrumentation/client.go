package instrumentation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/resources/adapter"
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

// BackendURLs holds the datasource write endpoints required by Set and
// SetupDiscovery requests. These are auto-resolved from the Grafana Cloud
// stack info and MUST NOT appear in the InstrumentationConfig manifest (NC-003).
type BackendURLs struct {
	MimirURL          string `json:"mimir_url"`
	MimirUsername     string `json:"mimir_username"`
	LokiURL           string `json:"loki_url"`
	LokiUsername      string `json:"loki_username"`
	TempoURL          string `json:"tempo_url"`
	TempoUsername     string `json:"tempo_username"`
	PyroscopeURL      string `json:"pyroscope_url"`
	PyroscopeUsername string `json:"pyroscope_username"`
}

// BackendURLsFromStack resolves datasource write endpoints from the Grafana
// Cloud stack info returned by GCOM. The URLs include the required push path
// suffixes. The Tempo URL is converted to gRPC host:port format.
func BackendURLsFromStack(stack cloud.StackInfo) BackendURLs {
	return BackendURLs{
		MimirURL:          appendPath(stack.HMInstancePromURL, "/api/prom/push"),
		MimirUsername:     strconv.Itoa(stack.HMInstancePromID),
		LokiURL:           appendPath(stack.HLInstanceURL, "/loki/api/v1/push"),
		LokiUsername:      strconv.Itoa(stack.HLInstanceID),
		TempoURL:          toGRPCHostPort(stack.HTInstanceURL),
		TempoUsername:     strconv.Itoa(stack.HTInstanceID),
		PyroscopeURL:      appendPath(stack.HPInstanceURL, "/ingest"),
		PyroscopeUsername: strconv.Itoa(stack.HPInstanceID),
	}
}

// PromHeaders holds the X-Prom-* headers required by discovery/monitoring endpoints.
type PromHeaders struct {
	ClusterID  string
	InstanceID string
}

// PromHeadersFromStack extracts the X-Prom-* header values from stack info.
func PromHeadersFromStack(stack cloud.StackInfo) PromHeaders {
	return PromHeaders{
		ClusterID:  strconv.Itoa(stack.HMInstancePromClusterID),
		InstanceID: strconv.Itoa(stack.HMInstancePromID),
	}
}

func (h PromHeaders) toMap() map[string]string {
	return map[string]string{
		"X-Prom-Cluster-ID":  h.ClusterID,
		"X-Prom-Instance-ID": h.InstanceID,
	}
}

// --- Request / response types ---
// The API wraps instrumentation data in a "cluster" envelope object.

// appCluster is the wire-format cluster object for app instrumentation.
type appCluster struct {
	Name       string            `json:"name"`
	Namespaces []NamespaceConfig `json:"namespaces,omitempty"`
}

type getAppRequest struct {
	ClusterName string `json:"cluster_name"`
}

type getAppResponse struct {
	Cluster appCluster `json:"cluster"`
}

type setAppRequest struct {
	BackendURLs

	Cluster appCluster `json:"cluster"`
}

// k8sCluster is the wire-format cluster object for K8s instrumentation.
type k8sCluster struct {
	Name          string `json:"name"`
	Selection     string `json:"selection,omitempty"`
	CostMetrics   bool   `json:"costmetrics"`
	EnergyMetrics bool   `json:"energymetrics"`
	ClusterEvents bool   `json:"clusterevents"`
	NodeLogs      bool   `json:"nodelogs"`
}

type getK8SRequest struct {
	ClusterName string `json:"cluster_name"`
}

type getK8SResponse struct {
	Cluster k8sCluster `json:"cluster"`
}

type setK8SRequest struct {
	BackendURLs

	Cluster k8sCluster `json:"cluster"`
}

// setupDiscoveryRequest includes backend URLs but no cluster name.
type setupDiscoveryRequest struct {
	BackendURLs
}

// GetAppInstrumentationResponse is the unwrapped response from GetAppInstrumentation.
type GetAppInstrumentationResponse struct {
	Namespaces []NamespaceConfig `json:"namespaces,omitempty"`
}

// GetK8SInstrumentationResponse is the unwrapped response from GetK8SInstrumentation.
type GetK8SInstrumentationResponse struct {
	Selection     string `json:"selection,omitempty"`
	CostMetrics   bool   `json:"costmetrics"`
	EnergyMetrics bool   `json:"energymetrics"`
	ClusterEvents bool   `json:"clusterevents"`
	NodeLogs      bool   `json:"nodelogs"`
}

// DiscoveredItem represents a single discovered workload from RunK8sDiscovery.
type DiscoveredItem struct {
	ClusterName           string `json:"clusterName,omitempty"`
	Namespace             string `json:"namespace,omitempty"`
	Name                  string `json:"name,omitempty"`
	WorkloadType          string `json:"workloadType,omitempty"`
	DisplayNamespace      string `json:"displayNamespace,omitempty"`
	DisplayName           string `json:"displayName,omitempty"`
	OS                    string `json:"os,omitempty"`
	Lang                  string `json:"lang,omitempty"`
	InstrumentationStatus string `json:"instrumentationStatus,omitempty"`
}

// RunK8sDiscoveryResponse holds discovered workloads returned by RunK8sDiscovery.
type RunK8sDiscoveryResponse struct {
	Items []DiscoveredItem `json:"items,omitempty"`
}

// MonitoringNamespace holds per-namespace monitoring data.
type MonitoringNamespace struct {
	Name                        string `json:"name,omitempty"`
	InstrumentationStatus       string `json:"instrumentationStatus,omitempty"`
	InstrumentationErrorMessage string `json:"instrumentationErrorMessage,omitempty"`
	Workloads                   int    `json:"workloads,omitempty"`
	Pods                        int    `json:"pods,omitempty"`
}

// ClusterState holds the instrumentation state for a single cluster.
type ClusterState struct {
	Name                        string                `json:"name,omitempty"`
	InstrumentationStatus       string                `json:"instrumentationStatus,omitempty"`
	InstrumentationErrorMessage string                `json:"instrumentationErrorMessage,omitempty"`
	Namespaces                  []MonitoringNamespace `json:"namespaces,omitempty"`
	Nodes                       int                   `json:"nodes,omitempty"`
	Workloads                   int                   `json:"workloads,omitempty"`
	Pods                        int                   `json:"pods,omitempty"`
}

// RunK8sMonitoringResponse holds per-cluster monitoring state.
type RunK8sMonitoringResponse struct {
	Clusters []ClusterState `json:"clusters,omitempty"`
}

// --- Client methods ---

// GetAppInstrumentation retrieves the app instrumentation configuration for the given cluster.
func (c *Client) GetAppInstrumentation(ctx context.Context, clusterName string) (*GetAppInstrumentationResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathGetAppInstrumentation, getAppRequest{ClusterName: clusterName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetAppInstrumentation: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}

	var envelope getAppResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("GetAppInstrumentation: decode response: %w", err)
	}
	return &GetAppInstrumentationResponse{
		Namespaces: envelope.Cluster.Namespaces,
	}, nil
}

// SetAppInstrumentation sets app instrumentation configuration for the given cluster.
func (c *Client) SetAppInstrumentation(ctx context.Context, clusterName string, namespaces []NamespaceConfig, urls BackendURLs) error {
	resp, err := c.fleet.DoRequest(ctx, pathSetAppInstrumentation, setAppRequest{
		Cluster: appCluster{
			Name:       clusterName,
			Namespaces: namespaces,
		},
		BackendURLs: urls,
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
	resp, err := c.fleet.DoRequest(ctx, pathGetK8SInstrumentation, getK8SRequest{ClusterName: clusterName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("GetK8SInstrumentation: HTTP 404: %w", adapter.ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetK8SInstrumentation: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}

	var envelope getK8SResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("GetK8SInstrumentation: decode response: %w", err)
	}
	return &GetK8SInstrumentationResponse{
		Selection:     envelope.Cluster.Selection,
		CostMetrics:   envelope.Cluster.CostMetrics,
		EnergyMetrics: envelope.Cluster.EnergyMetrics,
		ClusterEvents: envelope.Cluster.ClusterEvents,
		NodeLogs:      envelope.Cluster.NodeLogs,
	}, nil
}

// SetK8SInstrumentation sets K8s monitoring configuration for the given cluster.
func (c *Client) SetK8SInstrumentation(ctx context.Context, clusterName string, k8s K8sSpec, urls BackendURLs) error {
	selection := k8s.Selection
	if selection == "" {
		selection = "SELECTION_INCLUDED"
	}
	resp, err := c.fleet.DoRequest(ctx, pathSetK8SInstrumentation, setK8SRequest{
		Cluster: k8sCluster{
			Name:          clusterName,
			Selection:     selection,
			CostMetrics:   k8s.CostMetrics,
			EnergyMetrics: k8s.EnergyMetrics,
			ClusterEvents: k8s.ClusterEvents,
			NodeLogs:      k8s.NodeLogs,
		},
		BackendURLs: urls,
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

// SetupK8sDiscovery initializes K8s discovery datasource endpoints.
func (c *Client) SetupK8sDiscovery(ctx context.Context, urls BackendURLs, promHeaders PromHeaders) error {
	resp, err := c.fleet.DoRequestWithHeaders(ctx, pathSetupK8sDiscovery, setupDiscoveryRequest{BackendURLs: urls}, promHeaders.toMap())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SetupK8sDiscovery: HTTP %d: %s", resp.StatusCode, fleet.ReadErrorBody(resp))
	}
	return nil
}

// RunK8sDiscovery executes discovery and returns discovered workloads.
func (c *Client) RunK8sDiscovery(ctx context.Context, promHeaders PromHeaders) (*RunK8sDiscoveryResponse, error) {
	resp, err := c.fleet.DoRequestWithHeaders(ctx, pathRunK8sDiscovery, struct{}{}, promHeaders.toMap())
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
func (c *Client) RunK8sMonitoring(ctx context.Context, promHeaders PromHeaders) (*RunK8sMonitoringResponse, error) {
	resp, err := c.fleet.DoRequestWithHeaders(ctx, pathRunK8sMonitoring, struct{}{}, promHeaders.toMap())
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

// --- helpers ---

func appendPath(base, suffix string) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + suffix
}

func toGRPCHostPort(httpURL string) string {
	if httpURL == "" {
		return ""
	}
	u, err := url.Parse(httpURL)
	if err != nil {
		return httpURL
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return u.Hostname() + ":" + port
}

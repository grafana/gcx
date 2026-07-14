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
)

// Connect endpoint paths for the instrumentation, discovery, and pipeline services.
const (
	pathGetAppInstrumentation = "/instrumentation.v1.InstrumentationService/GetAppInstrumentation"
	pathSetAppInstrumentation = "/instrumentation.v1.InstrumentationService/SetAppInstrumentation"
	pathGetK8SInstrumentation = "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation"
	pathSetK8SInstrumentation = "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation"
	pathSetupK8sDiscovery     = "/discovery.v1.DiscoveryService/SetupK8sDiscovery"
	pathRunK8sDiscovery       = "/discovery.v1.DiscoveryService/RunK8sDiscovery"
	pathRunK8sMonitoring      = "/discovery.v1.DiscoveryService/RunK8sMonitoring"
	pathListPipelines         = "/pipeline.v1.PipelineService/ListPipelines"
)

// Client is the instrumentation-specific HTTP client built on top of the
// shared fleet base client. It adds methods for all instrumentation.v1,
// discovery.v1, and pipeline.v1 Connect endpoints. No connectrpc library
// is used.
type Client struct {
	fleet *fleet.Client
}

// NewClient creates a new instrumentation Client using the provided fleet base client.
func NewClient(f *fleet.Client) *Client {
	return &Client{fleet: f}
}

// BackendURLs holds the datasource write endpoints required by Set and
// SetupDiscovery requests. These are auto-resolved from the Grafana Cloud
// stack info and MUST NOT appear in any user-facing manifest.
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

// FleetManagement holds the fleet management connection parameters used by the
// grafana-cloud-onboarding helm chart. Decoupled from BackendURLs because the
// onboarding chart does not take per-signal datasource URLs — Fleet Management
// owns destination routing in the pipelines it pushes to alloy-daemon.
type FleetManagement struct {
	// URL is the Fleet Management gRPC endpoint (AgentManagementInstanceURL).
	URL string
	// Username is the Fleet Management instance ID used as the basic-auth username.
	Username string
}

// FleetManagementFromStack resolves Fleet Management connection parameters
// from the Grafana Cloud stack info returned by GCOM.
func FleetManagementFromStack(stack cloud.StackInfo) FleetManagement {
	return FleetManagement{
		URL:      stack.AgentManagementInstanceURL,
		Username: strconv.Itoa(stack.AgentManagementInstanceID),
	}
}

// --- Wire-format types (internal to client.go) ---
//
// Wire types use concrete bool fields for JSON marshaling / unmarshaling.
// Conversion to domain types (which use *bool for tri-state semantics) is
// performed in each method. On write paths, nil *bool fields collapse to false
// (derefBool) since the RMW preserve logic lives in the rmw helper (T4).

// wireAppOverride is the on-wire per-workload override.
type wireAppOverride struct {
	Name      string `json:"name"`
	Selection string `json:"selection"`
}

// wireAppNamespace is the on-wire namespace entry within an app cluster.
type wireAppNamespace struct {
	Name            string            `json:"name"`
	Autoinstrument  bool              `json:"autoinstrument"`
	Tracing         bool              `json:"tracing"`
	Logging         bool              `json:"logging"`
	ProcessMetrics  bool              `json:"processmetrics"`
	ExtendedMetrics bool              `json:"extendedmetrics"`
	Profiling       bool              `json:"profiling"`
	Apps            []wireAppOverride `json:"apps,omitempty"`
}

// wireAppCluster is the on-wire cluster object for app instrumentation.
type wireAppCluster struct {
	Name       string             `json:"name"`
	Namespaces []wireAppNamespace `json:"namespaces,omitempty"`
}

// getAppRequest is the request body for GetAppInstrumentation.
type getAppRequest struct {
	ClusterName string `json:"cluster_name"`
}

// getAppResponse is the response envelope for GetAppInstrumentation.
type getAppResponse struct {
	Cluster wireAppCluster `json:"cluster"`
}

// setAppRequest is the request body for SetAppInstrumentation.
type setAppRequest struct {
	BackendURLs

	Cluster wireAppCluster `json:"cluster"`
}

// wireK8sCluster is the on-wire cluster object for K8s instrumentation.
type wireK8sCluster struct {
	Name          string `json:"name"`
	Selection     string `json:"selection"`
	CostMetrics   bool   `json:"costmetrics"`
	EnergyMetrics bool   `json:"energymetrics"`
	ClusterEvents bool   `json:"clusterevents"`
	NodeLogs      bool   `json:"nodelogs"`
}

// getK8SRequest is the request body for GetK8SInstrumentation.
type getK8SRequest struct {
	ClusterName string `json:"cluster_name"`
}

// getK8SResponse is the response envelope for GetK8SInstrumentation.
type getK8SResponse struct {
	Cluster wireK8sCluster `json:"cluster"`
}

// setK8SRequest is the request body for SetK8SInstrumentation.
type setK8SRequest struct {
	BackendURLs

	Cluster wireK8sCluster `json:"cluster"`
}

// setupDiscoveryRequest includes backend URLs but no cluster name.
type setupDiscoveryRequest struct {
	BackendURLs
}

// wireDiscoveryItem is the on-wire item from RunK8sDiscovery.
type wireDiscoveryItem struct {
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

// wireRunK8sDiscoveryResponse is the on-wire response from RunK8sDiscovery.
type wireRunK8sDiscoveryResponse struct {
	Items []wireDiscoveryItem `json:"items,omitempty"`
}

// wireMonitoringNamespace is the on-wire per-namespace entry from RunK8sMonitoring.
type wireMonitoringNamespace struct {
	Name                        string `json:"name,omitempty"`
	InstrumentationStatus       string `json:"instrumentationStatus,omitempty"`
	InstrumentationErrorMessage string `json:"instrumentationErrorMessage,omitempty"`
	Workloads                   int    `json:"workloads,omitempty"`
	Pods                        int    `json:"pods,omitempty"`
}

// wireClusterState is the on-wire per-cluster state from RunK8sMonitoring.
type wireClusterState struct {
	Name                        string                    `json:"name,omitempty"`
	InstrumentationStatus       string                    `json:"instrumentationStatus,omitempty"`
	InstrumentationErrorMessage string                    `json:"instrumentationErrorMessage,omitempty"`
	Namespaces                  []wireMonitoringNamespace `json:"namespaces,omitempty"`
	Nodes                       int                       `json:"nodes,omitempty"`
	Workloads                   int                       `json:"workloads,omitempty"`
	Pods                        int                       `json:"pods,omitempty"`
}

// wireRunK8sMonitoringResponse is the on-wire response from RunK8sMonitoring.
type wireRunK8sMonitoringResponse struct {
	Clusters []wireClusterState `json:"clusters,omitempty"`
}

// wirePipeline is the on-wire pipeline object from ListPipelines.
type wirePipeline struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// wireListPipelinesResponse is the on-wire response from ListPipelines.
type wireListPipelinesResponse struct {
	Pipelines []wirePipeline `json:"pipelines,omitempty"`
}

// --- Response types ---

// GetAppInstrumentationResponse is the unwrapped response from GetAppInstrumentation.
type GetAppInstrumentationResponse struct {
	// Namespaces is the list of configured namespace entries.
	Namespaces []App
}

// GetK8SInstrumentationResponse is the unwrapped response from GetK8SInstrumentation.
type GetK8SInstrumentationResponse struct {
	// Cluster holds the declared K8s monitoring configuration.
	Cluster Cluster
}

// RunK8sDiscoveryResponse holds discovered workloads returned by RunK8sDiscovery.
type RunK8sDiscoveryResponse struct {
	// Items is the list of discovered workloads.
	Items []DiscoveryItem
}

// RunK8sMonitoringResponse holds per-cluster monitoring state from RunK8sMonitoring.
type RunK8sMonitoringResponse struct {
	// Clusters is the list of observed cluster states.
	Clusters []ClusterObservedState
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
		return nil, fleet.StatusError("GetAppInstrumentation", pathGetAppInstrumentation, resp)
	}

	var envelope getAppResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("GetAppInstrumentation: decode response: %w", err)
	}

	namespaces := make([]App, 0, len(envelope.Cluster.Namespaces))
	for _, wns := range envelope.Cluster.Namespaces {
		apps := make([]AppOverride, 0, len(wns.Apps))
		for _, wa := range wns.Apps {
			apps = append(apps, AppOverride(wa))
		}
		namespaces = append(namespaces, App{
			Name:            wns.Name,
			Autoinstrument:  boolPtr(wns.Autoinstrument),  //nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
			Tracing:         boolPtr(wns.Tracing),         //nolint:modernize
			Logging:         boolPtr(wns.Logging),         //nolint:modernize
			ProcessMetrics:  boolPtr(wns.ProcessMetrics),  //nolint:modernize
			ExtendedMetrics: boolPtr(wns.ExtendedMetrics), //nolint:modernize
			Profiling:       boolPtr(wns.Profiling),       //nolint:modernize
			Apps:            apps,
		})
	}
	return &GetAppInstrumentationResponse{Namespaces: namespaces}, nil
}

// SetAppInstrumentation sets app instrumentation configuration for the given cluster.
// The namespaces slice is a whole-list replacement of the cluster's app config.
func (c *Client) SetAppInstrumentation(ctx context.Context, clusterName string, namespaces []App, urls BackendURLs) error {
	wireNS := make([]wireAppNamespace, 0, len(namespaces))
	for _, ns := range namespaces {
		apps := make([]wireAppOverride, 0, len(ns.Apps))
		for _, a := range ns.Apps {
			apps = append(apps, wireAppOverride(a))
		}
		wireNS = append(wireNS, wireAppNamespace{
			Name:            ns.Name,
			Autoinstrument:  derefBool(ns.Autoinstrument),
			Tracing:         derefBool(ns.Tracing),
			Logging:         derefBool(ns.Logging),
			ProcessMetrics:  derefBool(ns.ProcessMetrics),
			ExtendedMetrics: derefBool(ns.ExtendedMetrics),
			Profiling:       derefBool(ns.Profiling),
			Apps:            apps,
		})
	}

	resp, err := c.fleet.DoRequest(ctx, pathSetAppInstrumentation, setAppRequest{
		BackendURLs: urls,
		Cluster:     wireAppCluster{Name: clusterName, Namespaces: wireNS},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fleet.StatusError("SetAppInstrumentation", pathSetAppInstrumentation, resp)
	}
	return nil
}

// GetK8SInstrumentation retrieves the K8s monitoring configuration for the given cluster.
//
// Note: wire-fixture tests confirm the response mapper
// correctly preserves SELECTION_EXCLUDED. If the live stack returns "" after
// SetK8SInstrumentation(SELECTION_EXCLUDED), the bug is server-side.
// See docs/adrs/instrumentation/002-cli-redesign.md § "E — Selection enum hygiene".
func (c *Client) GetK8SInstrumentation(ctx context.Context, clusterName string) (*GetK8SInstrumentationResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathGetK8SInstrumentation, getK8SRequest{ClusterName: clusterName})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fleet.StatusError("GetK8SInstrumentation", pathGetK8SInstrumentation, resp)
	}

	var envelope getK8SResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("GetK8SInstrumentation: decode response: %w", err)
	}

	return &GetK8SInstrumentationResponse{
		Cluster: Cluster{
			Name:          envelope.Cluster.Name,
			Selection:     envelope.Cluster.Selection,
			CostMetrics:   boolPtr(envelope.Cluster.CostMetrics),   //nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
			EnergyMetrics: boolPtr(envelope.Cluster.EnergyMetrics), //nolint:modernize
			ClusterEvents: boolPtr(envelope.Cluster.ClusterEvents), //nolint:modernize
			NodeLogs:      boolPtr(envelope.Cluster.NodeLogs),      //nolint:modernize
		},
	}, nil
}

// SetK8SInstrumentation sets K8s monitoring configuration for the given cluster.
// When Selection is SELECTION_EXCLUDED, the backend deletes the K8s monitoring pipeline.
func (c *Client) SetK8SInstrumentation(ctx context.Context, clusterName string, k8s Cluster, urls BackendURLs) error {
	selection := k8s.Selection
	if selection == "" {
		selection = "SELECTION_INCLUDED"
	}

	resp, err := c.fleet.DoRequest(ctx, pathSetK8SInstrumentation, setK8SRequest{
		BackendURLs: urls,
		Cluster: wireK8sCluster{
			Name:          clusterName,
			Selection:     selection,
			CostMetrics:   derefBool(k8s.CostMetrics),
			EnergyMetrics: derefBool(k8s.EnergyMetrics),
			ClusterEvents: derefBool(k8s.ClusterEvents),
			NodeLogs:      derefBool(k8s.NodeLogs),
		},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fleet.StatusError("SetK8SInstrumentation", pathSetK8SInstrumentation, resp)
	}
	return nil
}

// SetupK8sDiscovery initializes K8s discovery datasource endpoints.
// This call is idempotent: if a Beyla survey pipeline already exists,
// the backend returns success without modification.
func (c *Client) SetupK8sDiscovery(ctx context.Context, urls BackendURLs) error {
	resp, err := c.fleet.DoRequest(ctx, pathSetupK8sDiscovery, setupDiscoveryRequest{BackendURLs: urls})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fleet.StatusError("SetupK8sDiscovery", pathSetupK8sDiscovery, resp)
	}
	return nil
}

// RunK8sDiscovery executes discovery and returns all discovered workloads
// across all clusters. Filtering by cluster, namespace, or status is performed
// client-side by the callers.
func (c *Client) RunK8sDiscovery(ctx context.Context) (*RunK8sDiscoveryResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathRunK8sDiscovery, struct{}{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fleet.StatusError("RunK8sDiscovery", pathRunK8sDiscovery, resp)
	}

	var wire wireRunK8sDiscoveryResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("RunK8sDiscovery: decode response: %w", err)
	}

	items := make([]DiscoveryItem, 0, len(wire.Items))
	for _, wi := range wire.Items {
		items = append(items, DiscoveryItem{
			ClusterName:           wi.ClusterName,
			Namespace:             wi.Namespace,
			Name:                  wi.Name,
			WorkloadType:          wi.WorkloadType,
			DisplayNamespace:      wi.DisplayNamespace,
			DisplayName:           wi.DisplayName,
			OS:                    wi.OS,
			Lang:                  wi.Lang,
			InstrumentationStatus: InstrumentationStatus(wi.InstrumentationStatus),
		})
	}
	return &RunK8sDiscoveryResponse{Items: items}, nil
}

// RunK8sMonitoring retrieves the instrumentation monitoring state for all clusters.
// Clusters that have been Set but are not yet reporting survey_info will be absent
// from this response — the enumerate helper (T5) resolves them via ListPipelines.
func (c *Client) RunK8sMonitoring(ctx context.Context) (*RunK8sMonitoringResponse, error) {
	resp, err := c.fleet.DoRequest(ctx, pathRunK8sMonitoring, struct{}{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fleet.StatusError("RunK8sMonitoring", pathRunK8sMonitoring, resp)
	}

	var wire wireRunK8sMonitoringResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("RunK8sMonitoring: decode response: %w", err)
	}

	clusters := make([]ClusterObservedState, 0, len(wire.Clusters))
	for _, wc := range wire.Clusters {
		namespaces := make([]NamespaceObservedState, 0, len(wc.Namespaces))
		for _, wn := range wc.Namespaces {
			namespaces = append(namespaces, NamespaceObservedState{
				Name:                        wn.Name,
				InstrumentationStatus:       InstrumentationStatus(wn.InstrumentationStatus),
				InstrumentationErrorMessage: wn.InstrumentationErrorMessage,
				Workloads:                   wn.Workloads,
				Pods:                        wn.Pods,
			})
		}
		clusters = append(clusters, ClusterObservedState{
			Name:                        wc.Name,
			InstrumentationStatus:       InstrumentationStatus(wc.InstrumentationStatus),
			InstrumentationErrorMessage: wc.InstrumentationErrorMessage,
			Namespaces:                  namespaces,
			Nodes:                       wc.Nodes,
			Workloads:                   wc.Workloads,
			Pods:                        wc.Pods,
		})
	}
	return &RunK8sMonitoringResponse{Clusters: clusters}, nil
}

// ListPipelines returns all pipelines from the fleet management pipeline service.
// Callers (the enumerate helper, T5) perform client-side filtering by K8s monitoring
// pipeline metadata to identify clusters that have been Set but are not yet reporting
// survey_info.
func (c *Client) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	resp, err := c.fleet.DoRequest(ctx, pathListPipelines, struct{}{})
	if err != nil {
		return nil, fmt.Errorf("ListPipelines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fleet.StatusError("ListPipelines", pathListPipelines, resp)
	}

	var wire wireListPipelinesResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("ListPipelines: decode response: %w", err)
	}

	pipelines := make([]Pipeline, 0, len(wire.Pipelines))
	for _, wp := range wire.Pipelines {
		pipelines = append(pipelines, Pipeline(wp))
	}
	return pipelines, nil
}

// IsNamespaceDiscovered returns true iff the named namespace appears under
// the named cluster in RunK8sDiscovery's response. A fresh discovery call is
// made each time; no caching. Discovery RPC error is propagated as a wrapped
// error (callers handle it).
//
// The RunK8sDiscovery response is a flat list of workloads (DiscoveryItems).
// A namespace is considered "discovered" iff at least one workload with
// ClusterName == cluster and Namespace == namespace exists in the response.
func (c *Client) IsNamespaceDiscovered(ctx context.Context, cluster, namespace string) (bool, error) {
	resp, err := c.RunK8sDiscovery(ctx)
	if err != nil {
		return false, fmt.Errorf("discovery: %w", err)
	}
	for _, item := range resp.Items {
		if item.ClusterName == cluster && item.Namespace == namespace {
			return true, nil
		}
	}
	return false, nil
}

// --- helpers ---

// boolPtr returns a pointer to the given bool value. Used when converting
// wire bool fields to domain *bool fields.
//
//nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
func boolPtr(b bool) *bool { return &b }

// derefBool dereferences a *bool, returning false for nil. Used when converting
// domain *bool fields to wire bool fields on write paths.
// NOTE: nil means "preserve existing value" in the RMW context; collapsing to false
// here is intentional — the RMW helper (T4) is responsible for re-reading and
// merging the current value before invoking Set methods.
func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// appendPath joins base and suffix, removing any trailing slash from base.
func appendPath(base, suffix string) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + suffix
}

// toGRPCHostPort converts an HTTP(S) URL to a gRPC-compatible host:port string.
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

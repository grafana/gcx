// Package output provides output codec adapters for the instrumentation provider.
// Codecs plug instrumentation types into gcx's existing output system without
// a K8s envelope. JSON and YAML output the bare domain type
// directly; table and wide output use custom codecs with specific column
// inventories.
package output

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/style"
)

// ─── View types ──────────────────────────────────────────────────────────────
//
// View types combine declared configuration (from the instrumentation client)
// with observed state (from RunK8sMonitoring). They are used for ALL output
// formats: JSON/YAML marshals the struct tags directly; table/wide codecs read
// the fields for column values.
//
// JSON/YAML/wide expose the underlying proto enum value for status.
// Human-facing table (default) normalizes status to OK/FAILING/NODATA.

// ClusterView is the unified view type for a single cluster, combining the
// declared Cluster config with observed ClusterObservedState. All output
// formats (JSON, YAML, table, wide) consume this type.
//
// Selection is present in JSON/YAML (no omitempty) but table
// and wide codecs do NOT render a SELECTION column.
type ClusterView struct {
	// Declared config fields — mirrors instrumentation.Cluster.
	Name          string `json:"name" yaml:"name"`
	Selection     string `json:"selection" yaml:"selection"` // always serialized; never omitempty
	CostMetrics   *bool  `json:"costMetrics,omitempty" yaml:"costMetrics,omitempty"`
	EnergyMetrics *bool  `json:"energyMetrics,omitempty" yaml:"energyMetrics,omitempty"`
	ClusterEvents *bool  `json:"clusterEvents,omitempty" yaml:"clusterEvents,omitempty"`
	NodeLogs      *bool  `json:"nodeLogs,omitempty" yaml:"nodeLogs,omitempty"`

	// Observed state fields — merged from ClusterObservedState.
	InstrumentationStatus instrumentation.InstrumentationStatus `json:"instrumentationStatus,omitempty" yaml:"instrumentationStatus,omitempty"`
	Namespaces            int                                   `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	Workloads             int                                   `json:"workloads,omitempty" yaml:"workloads,omitempty"`
	Pods                  int                                   `json:"pods,omitempty" yaml:"pods,omitempty"`
	Nodes                 int                                   `json:"nodes,omitempty" yaml:"nodes,omitempty"`

	// Age is a pre-formatted human-readable age string set by the command layer.
	Age string `json:"age,omitempty" yaml:"age,omitempty"`
}

// AppView is the unified view type for a single namespace-level app
// instrumentation entry, combining declared App config with observed
// NamespaceObservedState.
//
// ClusterName is table-only context (excluded from JSON/YAML via json:"-"); it
// carries the cluster name for the CLUSTER column in table output, where the
// command always knows which cluster was queried.
type AppView struct {
	// ClusterName is the cluster this app belongs to. Excluded from JSON/YAML
	// because it is contextual (always known from the --cluster flag) and not
	// part of the App's own data model.
	ClusterName string `json:"-" yaml:"-"`

	// Declared config fields — mirrors instrumentation.App.
	Name            string `json:"name" yaml:"name"`
	Autoinstrument  *bool  `json:"autoInstrument,omitempty" yaml:"autoInstrument,omitempty"`
	Tracing         *bool  `json:"tracing,omitempty" yaml:"tracing,omitempty"`
	Logging         *bool  `json:"logging,omitempty" yaml:"logging,omitempty"`
	ProcessMetrics  *bool  `json:"processMetrics,omitempty" yaml:"processMetrics,omitempty"`
	ExtendedMetrics *bool  `json:"extendedMetrics,omitempty" yaml:"extendedMetrics,omitempty"`
	Profiling       *bool  `json:"profiling,omitempty" yaml:"profiling,omitempty"`

	// Observed state fields — merged from NamespaceObservedState.
	InstrumentationStatus instrumentation.InstrumentationStatus `json:"instrumentationStatus,omitempty" yaml:"instrumentationStatus,omitempty"`
	Workloads             int                                   `json:"workloads,omitempty" yaml:"workloads,omitempty"`
	Pods                  int                                   `json:"pods,omitempty" yaml:"pods,omitempty"`

	// Overrides is the count of per-workload AppOverride entries (len(App.Apps)).
	Overrides int `json:"overrides,omitempty" yaml:"overrides,omitempty"`

	// Discovered reports whether the namespace appears in RunK8sDiscovery's response.
	// No omitempty — false is meaningful (namespace declared but not observed on-cluster).
	Discovered bool `json:"discovered" yaml:"discovered"`

	// Age is a pre-formatted human-readable age string set by the command layer.
	Age string `json:"age,omitempty" yaml:"age,omitempty"`
}

// ServiceView is the unified view type for a single discovered workload
// (service), based on instrumentation.DiscoveryItem with an additional
// InstrumentationErrorMessage field for wide output.
type ServiceView struct {
	ClusterName                 string                                `json:"clusterName,omitempty" yaml:"clusterName,omitempty"`
	Namespace                   string                                `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name                        string                                `json:"name,omitempty" yaml:"name,omitempty"`
	WorkloadType                string                                `json:"workloadType,omitempty" yaml:"workloadType,omitempty"`
	DisplayNamespace            string                                `json:"displayNamespace,omitempty" yaml:"displayNamespace,omitempty"`
	DisplayName                 string                                `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	OS                          string                                `json:"os,omitempty" yaml:"os,omitempty"`
	Lang                        string                                `json:"lang,omitempty" yaml:"lang,omitempty"`
	InstrumentationStatus       instrumentation.InstrumentationStatus `json:"instrumentationStatus,omitempty" yaml:"instrumentationStatus,omitempty"`
	InstrumentationErrorMessage string                                `json:"instrumentationErrorMessage,omitempty" yaml:"instrumentationErrorMessage,omitempty"`

	// Age is a pre-formatted human-readable age string set by the command layer.
	Age string `json:"age,omitempty" yaml:"age,omitempty"`
}

// ─── Setup envelope ──────────────────────────────────────────────────────────

// SetupRequiredScopes is the canonical list of Cloud Access Policy scopes
// required for the grafana-cloud-onboarding helm chart. Defined once and
// reused by both the human-mode output text and the agent-mode JSON envelope.
var SetupRequiredScopes = []string{"set:alloy-data-write", "metrics:read"} //nolint:gochecknoglobals // package-level constant list; Go doesn't support const slices, var is the only option

// Discriminator values for the agent-mode setup helm envelope.
const (
	// SetupHelmEnvelopeType is the collision-resistant `type` discriminator.
	SetupHelmEnvelopeType = "gcx.instrumentation.setup"

	setupHelmSchemaVersion = "1"
)

// SetupHelmEnvelope is the agent-mode JSON envelope emitted by
// `gcx instrumentation setup <cluster> --print-helm-only --agent`.
//
// Type and SchemaVersion are the agent-legible discriminators; they are
// declared first so they serialize first on the emitted line.
// HelmCommand is the copy-pasteable helm upgrade command string.
// AccessPoliciesURL is the fully-qualified Grafana Cloud access-policies URL
// with the org slug substituted; falls back to literal <your-org> when the
// slug is unknown (empty string in OrgSlug).
// RequiredScopes lists the Cloud Access Policy scopes required by the helm chart.
type SetupHelmEnvelope struct {
	Type              string   `json:"type"`
	SchemaVersion     string   `json:"schema_version"`
	HelmCommand       string   `json:"helmCommand"`
	AccessPoliciesURL string   `json:"accessPoliciesURL"`
	RequiredScopes    []string `json:"requiredScopes"`
}

// NewSetupHelmEnvelope returns a SetupHelmEnvelope with the discriminators
// set. Construct envelopes with this so `type`/`schema_version` are never
// forgotten.
func NewSetupHelmEnvelope(helmCommand, accessPoliciesURL string, requiredScopes []string) SetupHelmEnvelope {
	return SetupHelmEnvelope{
		Type:              SetupHelmEnvelopeType,
		SchemaVersion:     setupHelmSchemaVersion,
		HelmCommand:       helmCommand,
		AccessPoliciesURL: accessPoliciesURL,
		RequiredScopes:    requiredScopes,
	}
}

// AccessPoliciesURL returns the Grafana Cloud access-policies URL for the
// given org slug. When slug is empty the literal placeholder <your-org> is
// used so callers always get a valid URL string regardless of context type
// (fallback spec Step 4).
func AccessPoliciesURL(orgSlug string) string {
	if orgSlug == "" {
		orgSlug = "<your-org>"
	}
	return "https://grafana.com/orgs/" + orgSlug + "/access-policies"
}

// ─── List envelope types ─────────────────────────────────────────────────────
//
// Instrumentation list commands wrap their results in these canonical envelopes
// so that JSON output matches the documented contract:
//
//	{"items": [...]}    (non-empty)
//	{"items": []}       (empty — never null)
//
// docs/design/output.md §101: all list endpoints emit the items envelope.
// The Items slice is initialized via make([]T, 0) in the command layer to
// guarantee [] (not null) for empty results.

// ClusterListEnvelope is the JSON envelope for the clusters list command.
type ClusterListEnvelope struct {
	Items []ClusterView `json:"items"`
}

// ServiceListEnvelope is the JSON envelope for the services list command.
type ServiceListEnvelope struct {
	Items []ServiceView `json:"items"`
}

// AppListEnvelope is the JSON envelope for the clusters apps list command.
type AppListEnvelope struct {
	Items []AppView `json:"items"`
}

// ─── STATUS normalization ─────────────────────────────────────────────────────

// NormalizeStatus maps a raw proto-enum InstrumentationStatus to the
// human-facing display value used in default table output.
//
// Handles both proto enum families and shorthand constants:
//
//	K8S_MONITORING_STATUS_INSTRUMENTED            → OK
//	K8S_MONITORING_STATUS_ERROR                   → FAILING
//	INSTRUMENTATION_STATUS_INSTRUMENTED           → OK
//	INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR  → FAILING
//	INSTRUMENTED (shorthand)                      → OK
//	INSTRUMENTATION_ERROR (shorthand)             → FAILING
//	everything else (PENDING_*, NOT_*, EXCLUDED, unknown) → NODATA
//
// Note: EXCLUDED maps to NODATA (not OK) — excluded clusters are not actively
// instrumented. This differs from the wait classifier (ClassifyK8sMonitoringStatus)
// which treats EXCLUDED as WaitSuccess for the purpose of exiting the wait loop.
func NormalizeStatus(status instrumentation.InstrumentationStatus) string {
	switch status {
	// Full wire names — K8s monitoring enum (from RunK8sMonitoring).
	case "K8S_MONITORING_STATUS_INSTRUMENTED":
		return "OK"
	case "K8S_MONITORING_STATUS_ERROR":
		return "FAILING"
	// Full wire names — Discovery item enum (from RunK8sDiscovery).
	case "INSTRUMENTATION_STATUS_INSTRUMENTED":
		return "OK"
	case "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR":
		return "FAILING"
	// Shorthand constants (backward compat — types.go values).
	case instrumentation.StatusInstrumented:
		return "OK"
	case instrumentation.StatusError:
		return "FAILING"
	}
	// All PENDING_*, NOT_INSTRUMENTED, EXCLUDED, UNSPECIFIED, and unknown values.
	return "NODATA"
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// boolDisplay returns "true", "false", or "" for a tri-state *bool.
func boolDisplay(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

// itoa converts an int to its decimal string representation.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// displayName returns the human-friendly display name, falling back to the
// canonical name when DisplayName is empty.
func displayName(displayName, name string) string {
	if displayName != "" {
		return displayName
	}
	return name
}

// displayNamespace returns the human-friendly display namespace, falling back
// to the canonical namespace when DisplayNamespace is empty.
func displayNamespace(displayNS, ns string) string {
	if displayNS != "" {
		return displayNS
	}
	return ns
}

// ─── ClusterTableCodec ────────────────────────────────────────────────────────

// ClusterTableCodec renders []ClusterView as a table or wide table.
//
// Default columns: NAME NAMESPACES WORKLOADS PODS STATUS
// Wide adds:       COST EVENTS ENERGY LOGS NODES
//
// Selection is NOT rendered as a column in either table or wide output.
// Default table STATUS is normalized to OK/FAILING/NODATA.
// Wide table STATUS is the raw proto enum value.
type ClusterTableCodec struct {
	Wide bool
}

var _ format.Codec = (*ClusterTableCodec)(nil)

func (c *ClusterTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ClusterTableCodec) Encode(w io.Writer, v any) error {
	var clusters []ClusterView
	switch val := v.(type) {
	case []ClusterView:
		clusters = val
	case ClusterListEnvelope:
		clusters = val.Items
	default:
		return fmt.Errorf("ClusterTableCodec: expected []ClusterView or ClusterListEnvelope, got %T", v)
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("NAME", "NAMESPACES", "WORKLOADS", "PODS", "COST", "EVENTS", "ENERGY", "LOGS", "NODES", "STATUS")
	} else {
		t = style.NewTable("NAME", "NAMESPACES", "WORKLOADS", "PODS", "STATUS")
	}

	for _, cl := range clusters {
		if c.Wide {
			t.Row(
				cl.Name,
				itoa(cl.Namespaces),
				itoa(cl.Workloads),
				itoa(cl.Pods),
				boolDisplay(cl.CostMetrics),
				boolDisplay(cl.ClusterEvents),
				boolDisplay(cl.EnergyMetrics),
				boolDisplay(cl.NodeLogs),
				itoa(cl.Nodes),
				string(cl.InstrumentationStatus),
			)
		} else {
			t.Row(
				cl.Name,
				itoa(cl.Namespaces),
				itoa(cl.Workloads),
				itoa(cl.Pods),
				NormalizeStatus(cl.InstrumentationStatus),
			)
		}
	}

	return t.Render(w)
}

func (c *ClusterTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ─── AppTableCodec ────────────────────────────────────────────────────────────

// AppTableCodec renders []AppView as a table or wide table.
//
// Default columns: NAME CLUSTER WORKLOADS PODS AUTOINSTRUMENT STATUS
// Wide adds:       TRACING LOGGING PROCESS_METRICS EXTENDED_METRICS PROFILING OVERRIDES
//
// Default table STATUS is normalized to OK/FAILING/NODATA.
// Wide table STATUS is the raw proto enum value.
type AppTableCodec struct {
	Wide bool
}

var _ format.Codec = (*AppTableCodec)(nil)

func (c *AppTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *AppTableCodec) Encode(w io.Writer, v any) error {
	var apps []AppView
	switch val := v.(type) {
	case []AppView:
		apps = val
	case AppListEnvelope:
		apps = val.Items
	default:
		return fmt.Errorf("AppTableCodec: expected []AppView or AppListEnvelope, got %T", v)
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("NAME", "CLUSTER", "WORKLOADS", "PODS", "AUTOINSTRUMENT", "TRACING", "LOGGING", "PROCESS_METRICS", "EXTENDED_METRICS", "PROFILING", "OVERRIDES", "STATUS")
	} else {
		t = style.NewTable("NAME", "CLUSTER", "WORKLOADS", "PODS", "AUTOINSTRUMENT", "STATUS")
	}

	for _, a := range apps {
		if c.Wide {
			t.Row(
				a.Name,
				a.ClusterName,
				itoa(a.Workloads),
				itoa(a.Pods),
				boolDisplay(a.Autoinstrument),
				boolDisplay(a.Tracing),
				boolDisplay(a.Logging),
				boolDisplay(a.ProcessMetrics),
				boolDisplay(a.ExtendedMetrics),
				boolDisplay(a.Profiling),
				itoa(a.Overrides),
				string(a.InstrumentationStatus),
			)
		} else {
			t.Row(
				a.Name,
				a.ClusterName,
				itoa(a.Workloads),
				itoa(a.Pods),
				boolDisplay(a.Autoinstrument),
				NormalizeStatus(a.InstrumentationStatus),
			)
		}
	}

	return t.Render(w)
}

func (c *AppTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ─── ServiceTableCodec ────────────────────────────────────────────────────────

// ServiceTableCodec renders []ServiceView as a table or wide table.
//
// Default columns: NAME CLUSTER NAMESPACE TYPE LANG STATUS
// Wide adds:       OS INSTRUMENTATION_ERROR
//
// TYPE is the K8s workload type (e.g. deployment, daemonset).
//
// Default table STATUS is normalized to OK/FAILING/NODATA.
// Wide table STATUS is the raw proto enum value.
//
// Table NAME renders DisplayName when set, falling back to Name.
// Table NAMESPACE renders DisplayNamespace when set, falling back to Namespace.
type ServiceTableCodec struct {
	Wide bool
}

var _ format.Codec = (*ServiceTableCodec)(nil)

func (c *ServiceTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ServiceTableCodec) Encode(w io.Writer, v any) error {
	var services []ServiceView
	switch val := v.(type) {
	case []ServiceView:
		services = val
	case ServiceListEnvelope:
		services = val.Items
	default:
		return fmt.Errorf("ServiceTableCodec: expected []ServiceView or ServiceListEnvelope, got %T", v)
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("NAME", "CLUSTER", "NAMESPACE", "TYPE", "LANG", "OS", "INSTRUMENTATION_ERROR", "STATUS")
	} else {
		t = style.NewTable("NAME", "CLUSTER", "NAMESPACE", "TYPE", "LANG", "STATUS")
	}

	for _, svc := range services {
		name := displayName(svc.DisplayName, svc.Name)
		ns := displayNamespace(svc.DisplayNamespace, svc.Namespace)

		if c.Wide {
			t.Row(
				name,
				svc.ClusterName,
				ns,
				svc.WorkloadType,
				svc.Lang,
				svc.OS,
				svc.InstrumentationErrorMessage,
				string(svc.InstrumentationStatus),
			)
		} else {
			t.Row(
				name,
				svc.ClusterName,
				ns,
				svc.WorkloadType,
				svc.Lang,
				NormalizeStatus(svc.InstrumentationStatus),
			)
		}
	}

	return t.Render(w)
}

func (c *ServiceTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

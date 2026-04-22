package instrumentation_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAppDisplayName(t *testing.T) {
	tests := []struct {
		cluster   string
		namespace string
		want      string
	}{
		{"prod-east", "payments", "prod-east-payments"},
		{"dev", "default", "dev-default"},
		{"", "ns", "-ns"},
		{"cluster", "", "cluster-"},
	}
	for _, tt := range tests {
		got := instrumentation.AppDisplayName(tt.cluster, tt.namespace)
		if got != tt.want {
			t.Errorf("AppDisplayName(%q, %q) = %q, want %q", tt.cluster, tt.namespace, got, tt.want)
		}
	}
}

func TestClusterRoundTrip(t *testing.T) {
	const clusterName = "prod-east"

	c := instrumentation.Cluster{
		CostMetrics:   true,
		ClusterEvents: false,
		EnergyMetrics: true,
		NodeLogs:      true,
	}
	c.SetResourceName(clusterName)

	// Build the full K8s-style envelope.
	envelope := adapter.TypedObject[instrumentation.Cluster]{
		TypeMeta: metav1.TypeMeta{
			APIVersion: instrumentation.ClusterGVK.GroupVersion().String(),
			Kind:       instrumentation.ClusterGVK.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: c.GetResourceName(),
		},
		Spec: c,
	}

	// Encode to JSON.
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Decode into unstructured.Unstructured.
	var u unstructured.Unstructured
	if err := json.Unmarshal(data, &u); err != nil {
		t.Fatalf("unmarshal into unstructured failed: %v", err)
	}

	// Verify apiVersion, kind, metadata.name.
	if got := u.GetAPIVersion(); got != "instrumentation.grafana.app/v1alpha1" {
		t.Errorf("apiVersion = %q, want %q", got, "instrumentation.grafana.app/v1alpha1")
	}
	if got := u.GetKind(); got != "Cluster" {
		t.Errorf("kind = %q, want %q", got, "Cluster")
	}
	if got := u.GetName(); got != clusterName {
		t.Errorf("metadata.name = %q, want %q", got, clusterName)
	}

	// Verify spec fields round-trip without loss.
	boolField := func(field string, want bool) {
		t.Helper()
		val, found, err := unstructured.NestedBool(u.Object, "spec", field)
		if err != nil {
			t.Errorf("spec.%s: unexpected error: %v", field, err)
			return
		}
		if !found {
			t.Errorf("spec.%s: field not found", field)
			return
		}
		if val != want {
			t.Errorf("spec.%s = %v, want %v", field, val, want)
		}
	}
	boolField("costMetrics", true)
	boolField("clusterEvents", false)
	boolField("energyMetrics", true)
	boolField("nodeLogs", true)

	// Verify no unexpected fields appear in spec (name must not be present).
	spec, _, _ := unstructured.NestedMap(u.Object, "spec")
	if _, ok := spec["name"]; ok {
		t.Error("spec.name should not be serialised (cluster name belongs only in metadata.name)")
	}
}

func TestAppRoundTrip(t *testing.T) {
	a := instrumentation.App{
		Cluster:   "prod-east",
		Namespace: "payments",
		Selection: "all",
		Tracing:   true,
		Logging:   false,
		Apps: []instrumentation.AppConfig{
			{Name: "checkout", Selection: "labeled", Type: "java"},
		},
	}

	// Build the full K8s-style envelope.
	envelope := adapter.TypedObject[instrumentation.App]{
		TypeMeta: metav1.TypeMeta{
			APIVersion: instrumentation.AppGVK.GroupVersion().String(),
			Kind:       instrumentation.AppGVK.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: a.GetResourceName(),
		},
		Spec: a,
	}

	// Encode to JSON.
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Decode into unstructured.Unstructured.
	var u unstructured.Unstructured
	if err := json.Unmarshal(data, &u); err != nil {
		t.Fatalf("unmarshal into unstructured failed: %v", err)
	}

	// Verify envelope fields.
	if got := u.GetAPIVersion(); got != "instrumentation.grafana.app/v1alpha1" {
		t.Errorf("apiVersion = %q", got)
	}
	if got := u.GetKind(); got != "App" {
		t.Errorf("kind = %q", got)
	}
	if got := u.GetName(); got != "prod-east-payments" {
		t.Errorf("metadata.name = %q, want %q", got, "prod-east-payments")
	}

	// Verify spec fields.
	strField := func(field, want string) {
		t.Helper()
		val, found, err := unstructured.NestedString(u.Object, "spec", field)
		if err != nil || !found {
			t.Errorf("spec.%s: not found (err=%v)", field, err)
			return
		}
		if val != want {
			t.Errorf("spec.%s = %q, want %q", field, val, want)
		}
	}
	boolField := func(field string, want bool) {
		t.Helper()
		val, found, err := unstructured.NestedBool(u.Object, "spec", field)
		if err != nil {
			t.Errorf("spec.%s: error: %v", field, err)
			return
		}
		if !found && want {
			t.Errorf("spec.%s: not found, want %v", field, want)
			return
		}
		if found && val != want {
			t.Errorf("spec.%s = %v, want %v", field, val, want)
		}
	}

	strField("cluster", "prod-east")
	strField("namespace", "payments")
	strField("selection", "all")
	boolField("tracing", true)
	boolField("logging", false)

	// Verify apps list round-trips.
	apps, found, err := unstructured.NestedSlice(u.Object, "spec", "apps")
	if err != nil || !found || len(apps) != 1 {
		t.Fatalf("spec.apps: found=%v err=%v len=%d", found, err, len(apps))
	}
	appMap, ok := apps[0].(map[string]any)
	if !ok {
		t.Fatalf("spec.apps[0] is not a map")
	}
	if appMap["name"] != "checkout" {
		t.Errorf("spec.apps[0].name = %v, want checkout", appMap["name"])
	}
	if appMap["selection"] != "labeled" {
		t.Errorf("spec.apps[0].selection = %v, want labeled", appMap["selection"])
	}
	if appMap["type"] != "java" {
		t.Errorf("spec.apps[0].type = %v, want java", appMap["type"])
	}
}

func TestAppSetResourceNameNoOp(t *testing.T) {
	a := instrumentation.App{Cluster: "prod-east", Namespace: "payments"}
	a.SetResourceName("something-else")
	// SetResourceName must be a no-op for App; identity comes from spec fields.
	if a.GetResourceName() != "prod-east-payments" {
		t.Errorf("GetResourceName() = %q after SetResourceName, want %q", a.GetResourceName(), "prod-east-payments")
	}
	if a.Cluster != "prod-east" || a.Namespace != "payments" {
		t.Error("SetResourceName must not modify spec.cluster or spec.namespace")
	}
}

func TestClusterSetResourceName(t *testing.T) {
	var c instrumentation.Cluster
	c.SetResourceName("my-cluster")
	if c.GetResourceName() != "my-cluster" {
		t.Errorf("GetResourceName() = %q, want %q", c.GetResourceName(), "my-cluster")
	}
	// Name must not be in the JSON spec.
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := m["name"]; ok {
		t.Error("Cluster name must not appear in spec JSON")
	}
}

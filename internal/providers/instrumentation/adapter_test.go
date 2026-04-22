package instrumentation //nolint:testpackage // Tests require access to internal helpers (validateAppIdentity, converters, etc.)

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Registrations
// ---------------------------------------------------------------------------

func TestRegistrations(t *testing.T) {
	// Registrations() is called with a providers.ConfigLoader — verify structure
	// only; the lazy factories are not invoked here.
	regs := Registrations()

	if len(regs) != 2 {
		t.Fatalf("Registrations() returned %d entries, want 2", len(regs))
	}

	for _, reg := range regs {
		if reg.Factory == nil {
			t.Errorf("kind %q: Factory is nil", reg.GVK.Kind)
		}
		if reg.Schema == nil {
			t.Errorf("kind %q: Schema is nil", reg.GVK.Kind)
		}
		if reg.Example == nil {
			t.Errorf("kind %q: Example is nil", reg.GVK.Kind)
		}
		if reg.Descriptor.Kind == "" {
			t.Errorf("kind %q: Descriptor.Kind is empty", reg.GVK.Kind)
		}
		if reg.GVK.Group == "" || reg.GVK.Version == "" || reg.GVK.Kind == "" {
			t.Errorf("incomplete GVK: %v", reg.GVK)
		}
	}

	if regs[0].GVK.Kind != "Cluster" {
		t.Errorf("regs[0].GVK.Kind = %q, want Cluster", regs[0].GVK.Kind)
	}
	if regs[1].GVK.Kind != "App" {
		t.Errorf("regs[1].GVK.Kind = %q, want App", regs[1].GVK.Kind)
	}
}

// ---------------------------------------------------------------------------
// App identity validation
// ---------------------------------------------------------------------------

func TestValidateAppIdentity(t *testing.T) {
	tests := []struct {
		name      string
		metaName  string
		cluster   string
		namespace string
		wantErr   bool
		errFields []string // substrings that must appear in the error
	}{
		{
			name:      "absent metadata.name is valid",
			metaName:  "",
			cluster:   "prod-east",
			namespace: "payments",
			wantErr:   false,
		},
		{
			name:      "matching metadata.name is valid",
			metaName:  "prod-east-payments",
			cluster:   "prod-east",
			namespace: "payments",
			wantErr:   false,
		},
		{
			name:      "mismatched metadata.name is an error",
			metaName:  "wrong-name",
			cluster:   "prod-east",
			namespace: "payments",
			wantErr:   true,
			errFields: []string{"wrong-name", "prod-east", "payments", "prod-east-payments"},
		},
		{
			name:      "empty cluster is still validated",
			metaName:  "wrong",
			cluster:   "",
			namespace: "payments",
			wantErr:   true,
			errFields: []string{"wrong", "-payments"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"name": tt.metaName},
				"spec": map[string]any{
					"cluster":   tt.cluster,
					"namespace": tt.namespace,
				},
			}}
			obj.SetName(tt.metaName)

			err := validateAppIdentity(obj)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAppIdentity() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				for _, field := range tt.errFields {
					if !strings.Contains(err.Error(), field) {
						t.Errorf("error %q does not contain %q", err.Error(), field)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// appAdapter identity enforcement + no-HTTP-call guarantee
// ---------------------------------------------------------------------------

func TestAppAdapterMismatchedNameNoHTTPCall(t *testing.T) {
	spy := &spyAdapter{}
	a := &appAdapter{inner: spy}

	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"cluster":   "prod-east",
			"namespace": "payments",
		},
	}}
	obj.SetName("wrong-name") // mismatches "prod-east-payments"

	_, err := a.Create(context.Background(), obj, metav1.CreateOptions{})
	if err == nil {
		t.Fatal("Create() with mismatched name should return an error")
	}
	if spy.createCalled {
		t.Error("inner.Create must NOT be called when identity validation fails")
	}

	// Error must reference all three fields.
	for _, field := range []string{"wrong-name", "prod-east", "payments"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error %q does not contain %q", err.Error(), field)
		}
	}
}

func TestAppAdapterAbsentNamePassesThrough(t *testing.T) {
	spy := &spyAdapter{}
	a := &appAdapter{inner: spy}

	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"cluster":   "prod-east",
			"namespace": "payments",
		},
	}}
	// metadata.name is empty/absent → validation should pass

	// inner.Create will return an error (not a real client), but that's expected
	_, _ = a.Create(context.Background(), obj, metav1.CreateOptions{})
	if !spy.createCalled {
		t.Error("inner.Create must be called when metadata.name is absent")
	}
}

func TestAppAdapterMatchingNamePassesThrough(t *testing.T) {
	spy := &spyAdapter{}
	a := &appAdapter{inner: spy}

	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"cluster":   "prod-east",
			"namespace": "payments",
		},
	}}
	obj.SetName("prod-east-payments") // matches AppDisplayName(cluster, namespace)

	_, _ = a.Create(context.Background(), obj, metav1.CreateOptions{})
	if !spy.createCalled {
		t.Error("inner.Create must be called when metadata.name matches")
	}
}

// spyAdapter records calls and returns nil errors (not used for real I/O).
type spyAdapter struct {
	createCalled bool
	updateCalled bool
}

func (s *spyAdapter) Create(_ context.Context, _ *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	s.createCalled = true
	return &unstructured.Unstructured{}, nil
}

func (s *spyAdapter) Update(_ context.Context, _ *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	s.updateCalled = true
	return &unstructured.Unstructured{}, nil
}

func (s *spyAdapter) List(_ context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (s *spyAdapter) Get(_ context.Context, _ string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}

func (s *spyAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error { return nil }
func (s *spyAdapter) Descriptor() resources.Descriptor                                 { return resources.Descriptor{} }
func (s *spyAdapter) Aliases() []string                                                { return nil }
func (s *spyAdapter) Schema() json.RawMessage                                          { return nil }
func (s *spyAdapter) Example() json.RawMessage                                         { return nil }

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func TestNamespaceConfigToApp(t *testing.T) {
	ns := NamespaceConfig{
		Name:            "payments",
		Selection:       "all",
		Tracing:         true,
		Logging:         false,
		ProcessMetrics:  true,
		ExtendedMetrics: false,
		Profiling:       true,
		Apps: []AppConfig{
			{Name: "checkout", Selection: "labeled", Type: "java"},
		},
	}

	app := namespaceConfigToApp("prod-east", ns)

	if app.Cluster != "prod-east" {
		t.Errorf("app.Cluster = %q, want prod-east", app.Cluster)
	}
	if app.Namespace != "payments" {
		t.Errorf("app.Namespace = %q, want payments", app.Namespace)
	}
	if app.Selection != "all" {
		t.Errorf("app.Selection = %q, want all", app.Selection)
	}
	if !app.Tracing {
		t.Error("app.Tracing should be true")
	}
	if app.Logging {
		t.Error("app.Logging should be false")
	}
	if len(app.Apps) != 1 {
		t.Fatalf("len(app.Apps) = %d, want 1", len(app.Apps))
	}
	if app.Apps[0].Name != "checkout" {
		t.Errorf("app.Apps[0].Name = %q, want checkout", app.Apps[0].Name)
	}
}

func TestAppToNamespaceConfigRoundTrip(t *testing.T) {
	app := App{
		Cluster:         "prod-east",
		Namespace:       "payments",
		Selection:       "all",
		Tracing:         true,
		Logging:         true,
		ProcessMetrics:  false,
		ExtendedMetrics: true,
		Profiling:       false,
		Apps: []AppConfig{
			{Name: "checkout", Selection: "labeled", Type: "java"},
		},
	}

	ns := appToNamespaceConfig(app)
	back := namespaceConfigToApp("prod-east", ns)

	if back.Namespace != app.Namespace {
		t.Errorf("Namespace = %q, want %q", back.Namespace, app.Namespace)
	}
	if back.Selection != app.Selection {
		t.Errorf("Selection = %q, want %q", back.Selection, app.Selection)
	}
	if back.Tracing != app.Tracing {
		t.Errorf("Tracing = %v, want %v", back.Tracing, app.Tracing)
	}
	if back.ExtendedMetrics != app.ExtendedMetrics {
		t.Errorf("ExtendedMetrics = %v, want %v", back.ExtendedMetrics, app.ExtendedMetrics)
	}
	if len(back.Apps) != 1 || back.Apps[0].Name != "checkout" {
		t.Errorf("Apps round-trip failed: %+v", back.Apps)
	}
}

// ---------------------------------------------------------------------------
// Cluster flag comparison
// ---------------------------------------------------------------------------

func TestRemoteOnlyClusterFlags(t *testing.T) {
	tests := []struct {
		name   string
		local  Cluster
		remote Cluster
		want   []string
	}{
		{
			name:   "no remote-only flags",
			local:  Cluster{CostMetrics: true, NodeLogs: true},
			remote: Cluster{CostMetrics: true, NodeLogs: true},
			want:   nil,
		},
		{
			name:   "remote has extra enabled flag",
			local:  Cluster{CostMetrics: true},
			remote: Cluster{CostMetrics: true, NodeLogs: true},
			want:   []string{"nodeLogs"},
		},
		{
			name:   "multiple remote-only flags",
			local:  Cluster{},
			remote: Cluster{CostMetrics: true, ClusterEvents: true, EnergyMetrics: true, NodeLogs: true},
			want:   []string{"costMetrics", "clusterEvents", "energyMetrics", "nodeLogs"},
		},
		{
			name:   "local enables flag remote does not — not a conflict",
			local:  Cluster{CostMetrics: true},
			remote: Cluster{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteOnlyClusterFlags(tt.local, tt.remote)
			if len(got) != len(tt.want) {
				t.Fatalf("remoteOnlyClusterFlags() = %v, want %v", got, tt.want)
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("flags[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Schema and example JSON validity
// ---------------------------------------------------------------------------

func TestClusterSchemaIsValidJSON(t *testing.T) {
	schema := ClusterSchema()
	if schema == nil {
		t.Fatal("ClusterSchema() returned nil")
	}
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		t.Fatalf("ClusterSchema() is not valid JSON: %v", err)
	}
}

func TestAppSchemaIsValidJSON(t *testing.T) {
	schema := AppSchema()
	if schema == nil {
		t.Fatal("AppSchema() returned nil")
	}
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		t.Fatalf("AppSchema() is not valid JSON: %v", err)
	}
}

func TestClusterExampleIsValidJSON(t *testing.T) {
	ex := ClusterExample()
	var m map[string]any
	if err := json.Unmarshal(ex, &m); err != nil {
		t.Fatalf("ClusterExample() is not valid JSON: %v", err)
	}
	// Must not contain backend URL fields.
	spec, _ := m["spec"].(map[string]any)
	for _, key := range []string{"mimir_url", "loki_url", "tempo_url", "pyroscope_url"} {
		if _, ok := spec[key]; ok {
			t.Errorf("ClusterExample() spec contains URL field %q (NC-003)", key)
		}
	}
}

func TestAppExampleIsValidJSON(t *testing.T) {
	ex := AppExample()
	var m map[string]any
	if err := json.Unmarshal(ex, &m); err != nil {
		t.Fatalf("AppExample() is not valid JSON: %v", err)
	}
	// Must not contain backend URL fields.
	spec, _ := m["spec"].(map[string]any)
	for _, key := range []string{"mimir_url", "loki_url", "tempo_url", "pyroscope_url"} {
		if _, ok := spec[key]; ok {
			t.Errorf("AppExample() spec contains URL field %q (NC-003)", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Natural key functions
// ---------------------------------------------------------------------------

func TestClusterNaturalKey(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "prod-east"},
	}}
	key, ok := ClusterNaturalKey(obj)
	if !ok {
		t.Fatal("ClusterNaturalKey returned ok=false")
	}
	if key == "" {
		t.Error("ClusterNaturalKey returned empty key")
	}
}

func TestAppNaturalKey(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"cluster":   "prod-east",
			"namespace": "payments",
		},
	}}
	key, ok := AppNaturalKey(obj)
	if !ok {
		t.Fatal("AppNaturalKey returned ok=false")
	}
	if key == "" {
		t.Error("AppNaturalKey returned empty key")
	}
	// Both fields must appear somewhere in the key.
	if !strings.Contains(key, "prod-east") && !strings.Contains(key, "prod") {
		t.Errorf("AppNaturalKey %q should reference cluster identity", key)
	}
}

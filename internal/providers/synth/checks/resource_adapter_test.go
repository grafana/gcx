package checks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeLoader implements smcfg.Loader using a fixed base URL and token.
type fakeLoader struct {
	baseURL   string
	token     string
	namespace string
}

func (l *fakeLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	return l.baseURL, l.token, l.namespace, nil
}

// newTestServer creates an httptest.Server that serves the provided handler.
func newAdapterTestServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// stubCheckList is the minimal SM API data for list tests.
//
//nolint:gochecknoglobals // Test fixture shared across test functions.
var stubCheckList = []checks.Check{
	{
		ID:        1001,
		TenantID:  214,
		Job:       "web-check",
		Target:    "https://grafana.com",
		Frequency: 60000,
		Timeout:   10000,
		Enabled:   true,
		Settings:  checks.CheckSettings{"http": map[string]any{"method": "GET"}},
		Probes:    []int64{1, 2},
	},
}

// stubProbeList is the minimal SM API probe list for name resolution.
//
//nolint:gochecknoglobals // Test fixture shared across test functions.
var stubProbeListData = []map[string]any{
	{"id": float64(1), "name": "Oregon"},
	{"id": float64(2), "name": "Spain"},
}

// buildTestMux creates a ServeMux with endpoints for checks and probes.
func buildTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/check/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubCheckList)
	})

	mux.HandleFunc("/api/v1/probe/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubProbeListData)
	})

	mux.HandleFunc("/api/v1/check/1001", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubCheckList[0])
	})

	mux.HandleFunc("/api/v1/tenant", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(checks.Tenant{ID: 214})
	})

	return mux
}

func TestResourceAdapter_List(t *testing.T) {
	mux := buildTestMux(t)
	srv := newAdapterTestServer(t, mux)

	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	list, err := a.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	item := list.Items[0]
	assert.Equal(t, checks.APIVersion, item.GetAPIVersion())
	assert.Equal(t, checks.Kind, item.GetKind())
	// metadata.name includes the numeric ID suffix for uniqueness; metadata.uid also carries it.
	assert.Equal(t, "web-check-1001", item.GetName())
	assert.Equal(t, "1001", string(item.GetUID()))
	assert.Equal(t, "default", item.GetNamespace())

	spec, ok := item.Object["spec"].(map[string]any)
	require.True(t, ok, "spec should be a map")
	assert.Equal(t, "web-check", spec["job"])

	// Probe IDs should be resolved to names in the spec.
	probeList, ok := spec["probes"].([]any)
	require.True(t, ok, "probes should be []any")
	require.Len(t, probeList, 2)
	assert.Equal(t, "Oregon", probeList[0])
	assert.Equal(t, "Spain", probeList[1])
}

func TestResourceAdapter_Get(t *testing.T) {
	mux := buildTestMux(t)
	srv := newAdapterTestServer(t, mux)

	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	obj, err := a.Get(context.Background(), "1001", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, obj)

	assert.Equal(t, checks.APIVersion, obj.GetAPIVersion())
	assert.Equal(t, checks.Kind, obj.GetKind())
	// metadata.name includes the numeric ID suffix; metadata.uid also carries it.
	assert.Equal(t, "web-check-1001", obj.GetName())
	assert.Equal(t, "1001", string(obj.GetUID()))
}

func TestResourceAdapter_Get_NonNumericName(t *testing.T) {
	loader := &fakeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	_, err = a.Get(context.Background(), "not-a-number", metav1.GetOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "numeric check ID")
}

func TestResourceAdapter_Delete_NonNumericName(t *testing.T) {
	loader := &fakeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	err = a.Delete(context.Background(), "not-a-number", metav1.DeleteOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "numeric check ID")
}

func TestResourceAdapter_Create(t *testing.T) {
	newCheck := checks.Check{
		ID:        9999,
		TenantID:  214,
		Job:       "new-check",
		Target:    "https://new.com",
		Frequency: 30000,
		Timeout:   5000,
		Enabled:   true,
		Settings:  checks.CheckSettings{"ping": map[string]any{}},
		Probes:    []int64{1},
	}

	mux := buildTestMux(t)
	mux.HandleFunc("/api/v1/check/add", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(newCheck)
	})
	mux.HandleFunc("/api/v1/check/9999", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(newCheck)
	})

	srv := newAdapterTestServer(t, mux)

	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": checks.APIVersion,
			"kind":       checks.Kind,
			"metadata": map[string]any{
				"name":      "0",
				"namespace": "default",
			},
			"spec": map[string]any{
				"job":       "new-check",
				"target":    "https://new.com",
				"frequency": float64(30000),
				"timeout":   float64(5000),
				"enabled":   true,
				"settings":  map[string]any{"ping": map[string]any{}},
				"probes":    []any{"Oregon"},
			},
		},
	}

	created, err := a.Create(context.Background(), obj, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, checks.Kind, created.GetKind())
}

func TestResourceAdapter_Create_UnknownProbeName(t *testing.T) {
	mux := buildTestMux(t)
	srv := newAdapterTestServer(t, mux)

	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": checks.APIVersion,
			"kind":       checks.Kind,
			"metadata": map[string]any{
				"name":      "0",
				"namespace": "default",
			},
			"spec": map[string]any{
				"job":       "bad-probe-check",
				"target":    "https://example.com",
				"frequency": float64(30000),
				"timeout":   float64(5000),
				"enabled":   true,
				"settings":  map[string]any{"ping": map[string]any{}},
				"probes":    []any{"NonExistentProbe"},
			},
		},
	}

	_, err = a.Create(context.Background(), obj, metav1.CreateOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `probe "NonExistentProbe" not found`)
}

func TestResourceAdapter_Update_UnknownProbeName(t *testing.T) {
	mux := buildTestMux(t)
	mux.HandleFunc("/api/v1/check/update", func(w http.ResponseWriter, r *http.Request) {
		// Should not be reached; probe resolution must fail first.
		w.WriteHeader(http.StatusOK)
	})
	srv := newAdapterTestServer(t, mux)

	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": checks.APIVersion,
			"kind":       checks.Kind,
			"metadata": map[string]any{
				"name":      "1001",
				"namespace": "default",
			},
			"spec": map[string]any{
				"job":       "web-check",
				"target":    "https://grafana.com",
				"frequency": float64(60000),
				"timeout":   float64(10000),
				"enabled":   true,
				"settings":  map[string]any{"http": map[string]any{"method": "GET"}},
				"probes":    []any{"TypoProbe"},
			},
		},
	}

	_, err = a.Update(context.Background(), obj, metav1.UpdateOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `probe "TypoProbe" not found`)
}

func TestResourceAdapter_Descriptor(t *testing.T) {
	loader := &fakeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	desc := a.Descriptor()
	assert.Equal(t, "syntheticmonitoring.ext.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v1alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "Check", desc.Kind)
}

func TestResourceAdapter_NoAliases(t *testing.T) {
	loader := &fakeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	assert.Empty(t, a.Aliases(), "adapter aliases should be empty (provider-prefixed aliases removed)")
}

func TestNewAdapterFactory_LazyInit(t *testing.T) {
	// Verify that NewAdapterFactory does not call LoadSMConfig during construction.
	callCount := 0
	loader := &countingLoader{callCount: &callCount}
	_ = checks.NewAdapterFactory(loader)

	assert.Equal(t, 0, callCount, "LoadSMConfig must not be called during factory construction")
}

// Verify that smcfg.Loader interface is satisfied by fakeLoader.
var _ smcfg.Loader = &fakeLoader{}

// countingLoader counts LoadSMConfig invocations.
type countingLoader struct {
	callCount *int
}

func (l *countingLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	*l.callCount++
	return "http://unused", "t", "default", nil
}

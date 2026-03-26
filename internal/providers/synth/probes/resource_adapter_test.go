package probes_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafanactl/internal/providers/synth/probes"
	"github.com/grafana/grafanactl/internal/providers/synth/smcfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeProbeLoader implements smcfg.Loader using fixed values.
type fakeProbeLoader struct {
	baseURL   string
	token     string
	namespace string
}

func (l *fakeProbeLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	return l.baseURL, l.token, l.namespace, nil
}

var _ smcfg.Loader = &fakeProbeLoader{}

//nolint:gochecknoglobals // Test fixture shared across test functions.
var stubProbes = []probes.Probe{
	{
		ID:        1,
		TenantID:  214,
		Name:      "Oregon",
		Region:    "US",
		Public:    true,
		Online:    true,
		Latitude:  45.5,
		Longitude: -122.6,
	},
	{
		ID:       2,
		TenantID: 214,
		Name:     "Spain",
		Region:   "EU",
		Public:   true,
	},
}

func newProbeTestServer(t *testing.T, probeList []probes.Probe) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/probe/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(probeList)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeResourceAdapter_List(t *testing.T) {
	srv := newProbeTestServer(t, stubProbes)

	loader := &fakeProbeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	list, err := a.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 2)

	first := list.Items[0]
	assert.Equal(t, probes.APIVersion, first.GetAPIVersion())
	assert.Equal(t, probes.Kind, first.GetKind())
	assert.Equal(t, "1", first.GetName())
	assert.Equal(t, "default", first.GetNamespace())

	spec, ok := first.Object["spec"].(map[string]any)
	require.True(t, ok, "spec should be a map")
	assert.Equal(t, "Oregon", spec["name"])
	assert.Equal(t, "US", spec["region"])

	// Server-managed fields must not appear in spec.
	assert.NotContains(t, spec, "id")
	assert.NotContains(t, spec, "tenantId")
}

func TestProbeResourceAdapter_Get(t *testing.T) {
	srv := newProbeTestServer(t, stubProbes)

	loader := &fakeProbeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	obj, err := a.Get(context.Background(), "2", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, obj)

	assert.Equal(t, probes.APIVersion, obj.GetAPIVersion())
	assert.Equal(t, probes.Kind, obj.GetKind())
	assert.Equal(t, "2", obj.GetName())

	spec, ok := obj.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Spain", spec["name"])
}

func TestProbeResourceAdapter_Get_NotFound(t *testing.T) {
	srv := newProbeTestServer(t, stubProbes)

	loader := &fakeProbeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	_, err = a.Get(context.Background(), "9999", metav1.GetOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestProbeResourceAdapter_Get_NonNumericName(t *testing.T) {
	loader := &fakeProbeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	_, err = a.Get(context.Background(), "not-a-number", metav1.GetOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "numeric ID")
}

func TestProbeResourceAdapter_Create_ReadOnly(t *testing.T) {
	loader := &fakeProbeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	_, err = a.Create(context.Background(), &unstructured.Unstructured{}, metav1.CreateOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
}

func TestProbeResourceAdapter_Update_ReadOnly(t *testing.T) {
	loader := &fakeProbeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	_, err = a.Update(context.Background(), &unstructured.Unstructured{}, metav1.UpdateOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
}

func TestProbeResourceAdapter_Delete_ReadOnly(t *testing.T) {
	loader := &fakeProbeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	err = a.Delete(context.Background(), "1", metav1.DeleteOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
}

func TestProbeResourceAdapter_Descriptor(t *testing.T) {
	loader := &fakeProbeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	desc := a.Descriptor()
	assert.Equal(t, "syntheticmonitoring.ext.grafana.app", desc.GroupVersion.Group)
	assert.Equal(t, "v1alpha1", desc.GroupVersion.Version)
	assert.Equal(t, "Probe", desc.Kind)
}

func TestProbeResourceAdapter_NoAliases(t *testing.T) {
	loader := &fakeProbeLoader{baseURL: "http://unused", token: "t", namespace: "default"}
	factory := probes.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	assert.Empty(t, a.Aliases(), "adapter aliases should be empty (provider-prefixed aliases removed)")
}

func TestNewProbeAdapterFactory_LazyInit(t *testing.T) {
	callCount := 0
	loader := &countingProbeLoader{callCount: &callCount}
	_ = probes.NewAdapterFactory(loader)

	assert.Equal(t, 0, callCount, "LoadSMConfig must not be called during factory construction")
}

type countingProbeLoader struct {
	callCount *int
}

func (l *countingProbeLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	*l.callCount++
	return "http://unused", "t", "default", nil
}

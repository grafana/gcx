package checks_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
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

// LoadSMProxyConfig returns an empty datasource UID so the typed client skips the
// proxy and exercises the direct SM API (these adapter tests are transport-agnostic
// and serve the /api/v1 paths; the proxy path is covered in client_test.go).
func (l *fakeLoader) LoadSMProxyConfig(_ context.Context) (config.NamespacedRESTConfig, string, string, error) {
	return config.NamespacedRESTConfig{}, "", l.namespace, nil
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
		Channels:  map[string]any{"k6": map[string]any{"id": "v2"}},
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
	assert.Equal(t, map[string]any{"k6": map[string]any{"id": "v2"}}, spec["channels"])
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
	assert.True(t, apierrors.IsNotFound(err), "expected NotFound error for non-numeric name, got: %v", err)
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
		Channels:  map[string]any{"k6": map[string]any{"id": "v2"}},
	}

	mux := buildTestMux(t)
	mux.HandleFunc("/api/v1/check/add", func(w http.ResponseWriter, r *http.Request) {
		var request checks.Check
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		assert.Equal(t, map[string]any{"k6": map[string]any{"id": "v2"}}, request.Channels)
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
				"channels":  map[string]any{"k6": map[string]any{"id": "v2"}},
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

// TestResourceAdapter_Update_PlaintextScript_IsReEncoded reproduces the
// 'gcx resources push checks' workflow against a file produced by
// 'checks get --decode-script': the on-disk spec carries a plaintext
// scripted/browser script rather than the API's base64 form. The resource
// adapter's Update path (used by push) must re-encode it before sending the
// request, the same way readCheckSpec does for 'checks update -f'.
func TestResourceAdapter_Update_PlaintextScript_IsReEncoded(t *testing.T) {
	mux := buildTestMux(t)

	var sentScript string
	mux.HandleFunc("/api/v1/check/update", func(w http.ResponseWriter, r *http.Request) {
		var request checks.Check
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		nested, ok := request.Settings["scripted"].(map[string]any)
		if !assert.True(t, ok, "settings.scripted should be a map") {
			return
		}
		sentScript, ok = nested["script"].(string)
		assert.True(t, ok, "settings.scripted.script should be a string")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stubCheckList[0])
	})
	srv := newAdapterTestServer(t, mux)

	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	factory := checks.NewAdapterFactory(loader)

	a, err := factory(context.Background())
	require.NoError(t, err)

	plaintext := "export default function() {}"
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": checks.APIVersion,
			"kind":       checks.Kind,
			"metadata": map[string]any{
				"name":      "web-check-1001",
				"namespace": "default",
			},
			"spec": map[string]any{
				"job":       "web-check",
				"target":    "https://grafana.com",
				"frequency": float64(60000),
				"timeout":   float64(10000),
				"enabled":   true,
				"settings":  map[string]any{"scripted": map[string]any{"script": plaintext}},
				"probes":    []any{"Oregon"},
			},
		},
	}

	_, err = a.Update(context.Background(), obj, metav1.UpdateOptions{})
	require.NoError(t, err)

	_, decodeErr := base64.StdEncoding.DecodeString(sentScript)
	assert.NoError(t, decodeErr, "script sent to the SM API must be base64-encoded, got plaintext %q", sentScript)
}

// TestResourceAdapter_Update_BasicMetricsOnly checks that an explicit
// basicMetricsOnly in the YAML spec reaches the SM API request, including
// false. When the field is absent from the YAML it must stay absent from the
// request, so the SM API applies its own default (true).
func TestResourceAdapter_Update_BasicMetricsOnly(t *testing.T) {
	tests := []struct {
		name      string
		specYAML  string
		wantSent  bool
		wantValue bool
	}{
		{name: "unset is omitted", specYAML: "", wantSent: false},
		{name: "false is sent", specYAML: "basicMetricsOnly: false\n", wantSent: true, wantValue: false},
		{name: "true is sent", specYAML: "basicMetricsOnly: true\n", wantSent: true, wantValue: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := buildTestMux(t)

			var sent map[string]any
			mux.HandleFunc("/api/v1/check/update", func(w http.ResponseWriter, r *http.Request) {
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&sent)) {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(stubCheckList[0])
			})
			srv := newAdapterTestServer(t, mux)

			loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
			a, err := checks.NewAdapterFactory(loader)(context.Background())
			require.NoError(t, err)

			specYAML := "job: web-check\n" +
				"target: https://grafana.com\n" +
				"frequency: 60000\n" +
				"timeout: 10000\n" +
				"enabled: true\n" +
				"settings:\n  http:\n    method: GET\n" +
				"probes: [Oregon]\n" +
				tc.specYAML
			var spec map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(specYAML), &spec))

			obj := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": checks.APIVersion,
					"kind":       checks.Kind,
					"metadata": map[string]any{
						"name":      "web-check-1001",
						"namespace": "default",
					},
					"spec": spec,
				},
			}

			_, err = a.Update(context.Background(), obj, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.NotNil(t, sent, "update request was not sent")

			value, ok := sent["basicMetricsOnly"]
			if !tc.wantSent {
				assert.False(t, ok, "basicMetricsOnly must be absent from the request, got %v", value)
				return
			}
			require.True(t, ok, "basicMetricsOnly must be present in the request")
			assert.Equal(t, tc.wantValue, value)
		})
	}
}

// TestResourceAdapter_Get_BasicMetricsOnly checks that the value the SM API
// returns appears in the YAML output, including false, so a get/update round
// trip keeps a check in full-metrics mode.
func TestResourceAdapter_Get_BasicMetricsOnly(t *testing.T) {
	tests := []struct {
		name     string
		apiValue string
		wantYAML string
	}{
		{name: "false is shown", apiValue: "false", wantYAML: "basicMetricsOnly: false"},
		{name: "true is shown", apiValue: "true", wantYAML: "basicMetricsOnly: true"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := buildTestMux(t)
			mux.HandleFunc("/api/v1/check/2002", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":2002,"tenantId":214,"job":"full-metrics","target":"https://grafana.com",` +
					`"frequency":60000,"timeout":10000,"enabled":true,"settings":{"http":{"method":"GET"}},` +
					`"probes":[1],"basicMetricsOnly":` + tc.apiValue + `}`))
			})
			srv := newAdapterTestServer(t, mux)

			loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
			a, err := checks.NewAdapterFactory(loader)(context.Background())
			require.NoError(t, err)

			obj, err := a.Get(context.Background(), "full-metrics-2002", metav1.GetOptions{})
			require.NoError(t, err)

			out, err := yaml.Marshal(obj.Object)
			require.NoError(t, err)
			assert.Contains(t, string(out), tc.wantYAML)
		})
	}
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
	// Verify that NewAdapterFactory does not load any config during construction.
	callCount := 0
	loader := &countingLoader{callCount: &callCount}
	_ = checks.NewAdapterFactory(loader)

	assert.Equal(t, 0, callCount, "config must not be loaded during factory construction")
}

// Verify that smcfg.Loader interface is satisfied by fakeLoader.
var _ smcfg.Loader = &fakeLoader{}

// countingLoader counts config-loading invocations.
type countingLoader struct {
	callCount *int
}

func (l *countingLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	*l.callCount++
	return "http://unused", "t", "default", nil
}

func (l *countingLoader) LoadSMProxyConfig(_ context.Context) (config.NamespacedRESTConfig, string, string, error) {
	*l.callCount++
	return config.NamespacedRESTConfig{}, "", "default", nil
}

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
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// validateEnvelope builds the unstructured check envelope the push pipeline
// hands to the adapter.
func validateEnvelope(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": checks.APIVersion,
			"kind":       checks.Kind,
			"metadata":   map[string]any{"name": name, "namespace": "default"},
			"spec": map[string]any{
				"job":       "web-check",
				"target":    "https://grafana.com",
				"frequency": float64(60000),
				"timeout":   float64(3000),
				"enabled":   true,
				"settings":  map[string]any{"http": map[string]any{"method": "GET"}},
				"probes":    []any{"Oregon", "Spain"},
			},
		},
	}
}

// validateMux serves check/validate with handler and fails the test if any
// write endpoint is hit: a dry-run must never mutate.
func validateMux(t *testing.T, validate http.HandlerFunc) *http.ServeMux {
	t.Helper()
	mux := buildTestMux(t)
	mux.HandleFunc("/api/v1/check/validate", validate)
	for _, p := range []string{"/api/v1/check/add", "/api/v1/check/update"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("dry-run must not call %s", r.URL.Path)
			http.Error(w, "unexpected write", http.StatusInternalServerError)
		})
	}
	return mux
}

func newValidateAdapter(t *testing.T, mux *http.ServeMux) adapter.ResourceAdapter {
	t.Helper()
	srv := newAdapterTestServer(t, mux)
	loader := &fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"}
	a, err := checks.NewAdapterFactory(loader)(context.Background())
	require.NoError(t, err)
	return a
}

func TestResourceAdapter_DryRun_Create_Validates(t *testing.T) {
	var gotBody map[string]any
	calls := 0
	a := newValidateAdapter(t, validateMux(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "findings": []any{}})
	}))

	got, err := a.Create(context.Background(), validateEnvelope("web-check"), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, 1, calls)
	assert.Equal(t, "web-check", gotBody["job"])
	assert.Equal(t, []any{"Oregon", "Spain"}, gotBody["probes"], "probe names are sent as-is")
	assert.NotContains(t, gotBody, "id", "a create has no check ID")
}

func TestResourceAdapter_DryRun_Update_SendsCheckID(t *testing.T) {
	var gotBody map[string]any
	a := newValidateAdapter(t, validateMux(t, func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "findings": []any{}})
	}))

	_, err := a.Update(context.Background(), validateEnvelope("web-check-1001"), metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
	require.NoError(t, err)

	assert.InDelta(t, 1001, gotBody["id"], 0, "an update validates against its own check ID")
}

func TestResourceAdapter_DryRun_InvalidCheck(t *testing.T) {
	a := newValidateAdapter(t, validateMux(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid": false,
			"findings": []map[string]string{
				{"severity": "error", "field": "probes", "msg": "invalid probe identifier"},
				{"severity": "warning", "field": "frequency", "msg": "below the app minimum"},
			},
		})
	}))

	_, err := a.Create(context.Background(), validateEnvelope("web-check"), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probes: invalid probe identifier")
	assert.NotContains(t, err.Error(), "below the app minimum", "warnings must not fail validation")
	assert.NotErrorIs(t, err, adapter.ErrDryRunUnverified)
}

func TestResourceAdapter_DryRun_WarningsDoNotFail(t *testing.T) {
	a := newValidateAdapter(t, validateMux(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":    true,
			"findings": []map[string]string{{"severity": "warning", "field": "frequency", "msg": "below the app minimum"}},
		})
	}))

	_, err := a.Create(context.Background(), validateEnvelope("web-check"), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.NoError(t, err)
}

func TestResourceAdapter_DryRun_ServerWithoutValidate_IsUnverified(t *testing.T) {
	a := newValidateAdapter(t, validateMux(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))

	_, err := a.Create(context.Background(), validateEnvelope("web-check"), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.ErrorIs(t, err, adapter.ErrDryRunUnverified,
		"an old server must be reported as skipped, not as a failure or a false success")
}

func TestResourceAdapter_DryRun_ServerError_IsFailure(t *testing.T) {
	a := newValidateAdapter(t, validateMux(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := a.Create(context.Background(), validateEnvelope("web-check"), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, adapter.ErrDryRunUnverified)
}

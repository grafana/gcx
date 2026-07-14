package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/fleet"
	gcxerrors "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// includeTestServer tracks GetApp and SetApp calls for include/exclude/clear tests.
type includeTestServer struct {
	getAppResp     string // JSON response body for GetAppInstrumentation
	setAppCalled   atomic.Int64
	setAppStatus   int
	getAppStatus   int
	discoveryItems []map[string]any // items returned by RunK8sDiscovery; nil returns empty list
}

func (s *includeTestServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	if s.getAppStatus == 0 {
		s.getAppStatus = http.StatusOK
	}
	if s.setAppStatus == 0 {
		s.setAppStatus = http.StatusOK
	}
	if s.getAppResp == "" {
		s.getAppResp = `{}`
	}

	mux := http.NewServeMux()
	mux.HandleFunc(proxyFleetBase+"/instrumentation.v1.InstrumentationService/GetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.getAppStatus)
		_, _ = w.Write([]byte(s.getAppResp))
	})
	mux.HandleFunc(proxyFleetBase+"/instrumentation.v1.InstrumentationService/SetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		s.setAppCalled.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.setAppStatus)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc(proxyFleetBase+"/discovery.v1.DiscoveryService/RunK8sDiscovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		items := s.discoveryItems
		if items == nil {
			items = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func makeIncludeClient(t *testing.T, serverURL string) *instrumentation.Client {
	t.Helper()
	f, err := fleet.NewClient(config.NamespacedRESTConfig{
		Config: rest.Config{Host: serverURL, BearerToken: "test-bearer"},
	})
	require.NoError(t, err)
	return instrumentation.NewClient(f)
}

// buildGetAppResp builds a JSON GetAppInstrumentation response with one namespace.
//
//nolint:unparam // clusterName is designed to be flexible; current tests all use "c1" by convention.
func buildGetAppResp(t *testing.T, clusterName, nsName string, autoinstrument *bool, apps []map[string]any) string {
	t.Helper()
	ns := map[string]any{"name": nsName}
	if autoinstrument != nil {
		ns["autoinstrument"] = *autoinstrument
	}
	if len(apps) > 0 {
		ns["apps"] = apps
	}
	body := map[string]any{
		"cluster": map[string]any{
			"name":       clusterName,
			"namespaces": []any{ns},
		},
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return string(data)
}

// TestRunInclude_Idempotent_AutoinstrumentDefault tests // A namespace with namespace-default autoinstrument (nil) and no overrides.
// First call adds INCLUDED override; second call is a no-op (zero Set calls).
func TestRunInclude_Idempotent_AutoinstrumentDefault(t *testing.T) {
	// Initial: autoinstrument=nil (namespace default), no per-workload overrides.
	initialResp := buildGetAppResp(t, "c1", "grotshop", nil, nil)

	// After first include: INCLUDED override is present.
	afterFirstResp := buildGetAppResp(t, "c1", "grotshop", nil, []map[string]any{
		{"name": "frontend", "selection": "SELECTION_INCLUDED"},
	})

	callCount := 0
	ts := &includeTestServer{}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc(proxyFleetBase+"/discovery.v1.DiscoveryService/RunK8sDiscovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"clusterName": "c1", "namespace": "grotshop", "name": "frontend"},
		}})
	})
	mux.HandleFunc(proxyFleetBase+"/instrumentation.v1.InstrumentationService/GetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if callCount == 0 || callCount == 1 || callCount == 2 {
			// First 3 calls (pre-check + 2 RMW reads) use initial state.
			_, _ = w.Write([]byte(initialResp))
		} else {
			// Subsequent calls (second invocation's pre-check) use post-write state.
			_, _ = w.Write([]byte(afterFirstResp))
		}
		callCount++
	})
	mux.HandleFunc(proxyFleetBase+"/instrumentation.v1.InstrumentationService/SetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		ts.setAppCalled.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := makeIncludeClient(t, srv.URL)

	// First invocation: should call SetApp once.
	var out1 bytes.Buffer
	err1 := services.RunInclude(context.Background(), client, "c1", "grotshop", "frontend", instrumentation.BackendURLs{}, &out1)
	require.NoError(t, err1, "first include must succeed (exit 0)")
	firstCallCount := ts.setAppCalled.Load()
	assert.Equal(t, int64(1), firstCallCount, "first include must call SetApp once")

	// Second invocation: reads post-write state (INCLUDED already present) → no-op.
	var out2 bytes.Buffer
	err2 := services.RunInclude(context.Background(), client, "c1", "grotshop", "frontend", instrumentation.BackendURLs{}, &out2)
	require.NoError(t, err2, "second include must succeed (exit 0)")
	secondCallCount := ts.setAppCalled.Load()
	assert.Equal(t, firstCallCount, secondCallCount,
		"second include must be a no-op: no additional Set calls")
}

// TestRunInclude_AutoinstrumentTrue_NoOverrideAdded verifies that when autoinstrument=true,
// include does NOT add a redundant INCLUDED override.
func TestRunInclude_AutoinstrumentTrue_NoOverrideAdded(t *testing.T) {
	trueVal := true
	resp := buildGetAppResp(t, "c1", "ns", &trueVal, nil)

	ts := &includeTestServer{
		getAppResp: resp,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunInclude(context.Background(), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, &out)
	require.NoError(t, err)
	// autoinstrument=true: namespace default is on, no override needed → no-op.
	assert.Equal(t, int64(0), ts.setAppCalled.Load(), "no Set call when autoinstrument=true and service is not excluded")
}

// TestRunInclude_RemovesExcludedOverride verifies that include removes an existing
// EXCLUDED override (and adds INCLUDED if needed).
func TestRunInclude_RemovesExcludedOverride(t *testing.T) {
	falseVal := false
	resp := buildGetAppResp(t, "c1", "ns", &falseVal, []map[string]any{
		{"name": "svc", "selection": "SELECTION_EXCLUDED"},
	})

	ts := &includeTestServer{
		getAppResp: resp,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunInclude(context.Background(), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, &out)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ts.setAppCalled.Load(), "Set must be called once to remove EXCLUDED override")
}

// TestRunInclude_NamespaceNotFound verifies that include returns an error when the
// namespace is not in the cluster's app configuration.
// The workload is present in discovery (pre-flight passes), but GetAppInstrumentation
// returns an empty namespace list, so the original namespace-not-found error fires.
func TestRunInclude_NamespaceNotFound(t *testing.T) {
	// Empty namespace list; workload exists in discovery so pre-flight passes.
	ts := &includeTestServer{
		getAppResp: `{"cluster":{"name":"c1","namespaces":[]}}`,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "missing-ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	err := services.RunInclude(context.Background(), client, "c1", "missing-ns", "svc", instrumentation.BackendURLs{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "services include")
	assert.Contains(t, err.Error(), "missing-ns")
}

// TestRunInclude_WorkloadNotFound verifies that include returns a workload-not-found
// error when RunK8sDiscovery does not contain the requested service, and does NOT
// call GetAppInstrumentation (pre-flight short-circuits the operation).
//
//nolint:dupl // WorkloadNotFound tests for exclude/include/clear are intentionally symmetric.
func TestRunInclude_WorkloadNotFound(t *testing.T) {
	callCounts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCounts[strings.TrimPrefix(r.URL.Path, proxyFleetBase)]++
		switch strings.TrimPrefix(r.URL.Path, proxyFleetBase) {
		case "/discovery.v1.DiscoveryService/RunK8sDiscovery":
			// Return empty items — workload not found.
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunInclude(context.Background(), client, "prod-eu", "checkout", "nonexistent-svc",
		instrumentation.BackendURLs{}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Resource not found")
	// Verify GetAppInstrumentation was NOT called (pre-flight short-circuited).
	assert.Zero(t, callCounts["/instrumentation.v1.InstrumentationService/GetAppInstrumentation"],
		"GetAppInstrumentation must not be called after pre-flight failure")
}

// TestRunInclude_WorkloadNotFound_ExitCode1 verifies validateWorkloadExists
// returns a gcxerrors.DetailedError with ExitCode 1 (ExitGeneralError) when the workload
// is absent from RunK8sDiscovery. This confirms the explicit ExitCode introduced in
// services/helpers.go (was implicit default 1; now explicit).
func TestRunInclude_WorkloadNotFound_ExitCode1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, proxyFleetBase) {
		case "/discovery.v1.DiscoveryService/RunK8sDiscovery":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunInclude(context.Background(), client, "prod-eu", "checkout", "nonexistent-svc",
		instrumentation.BackendURLs{}, &out)
	require.Error(t, err)

	var de *gcxerrors.DetailedError
	require.ErrorAs(t, err, &de, "expected *gcxerrors.DetailedError, got %T: %v", err, err)
	assert.Equal(t, "Resource not found", de.Summary)
	require.NotNil(t, de.ExitCode, "ExitCode must be set explicitly")
	assert.Equal(t, 1, *de.ExitCode, "ExitCode must be 1 (ExitGeneralError)")
}

// ─── Unit tests for mutation helpers ─────────────────────────────────────────

// TestApplyIncludeMutation_AddsIncluded verifies that include adds INCLUDED when autoinstrument=false.
func TestApplyIncludeMutation_AddsIncluded(t *testing.T) {
	falseVal := false
	ns := instrumentation.App{Name: "ns", Autoinstrument: &falseVal}
	result := services.ApplyIncludeMutation(ns, "svc")

	require.Len(t, result.Apps, 1)
	assert.Equal(t, "svc", result.Apps[0].Name)
	assert.Equal(t, "SELECTION_INCLUDED", result.Apps[0].Selection)
}

// TestApplyIncludeMutation_NoOverrideWhenAutoinstrumentTrue verifies that include
// does NOT add INCLUDED when autoinstrument=true (no redundant override).
func TestApplyIncludeMutation_NoOverrideWhenAutoinstrumentTrue(t *testing.T) {
	trueVal := true
	ns := instrumentation.App{Name: "ns", Autoinstrument: &trueVal}
	result := services.ApplyIncludeMutation(ns, "svc")
	assert.Empty(t, result.Apps)
}

// TestApplyIncludeMutation_RemovesExcluded verifies that include removes EXCLUDED override.
func TestApplyIncludeMutation_RemovesExcluded(t *testing.T) {
	falseVal := false
	ns := instrumentation.App{
		Name:           "ns",
		Autoinstrument: &falseVal,
		Apps: []instrumentation.AppOverride{
			{Name: "svc", Selection: "SELECTION_EXCLUDED"},
		},
	}
	result := services.ApplyIncludeMutation(ns, "svc")
	// EXCLUDED removed; INCLUDED added (autoinstrument=false).
	require.Len(t, result.Apps, 1)
	assert.Equal(t, "SELECTION_INCLUDED", result.Apps[0].Selection)
}

// TestApplyIncludeMutation_NilAutoinstrument verifies that include adds INCLUDED
// when autoinstrument is nil (unset, treated as off).
func TestApplyIncludeMutation_NilAutoinstrument(t *testing.T) {
	ns := instrumentation.App{Name: "ns", Autoinstrument: nil}
	result := services.ApplyIncludeMutation(ns, "svc")
	require.Len(t, result.Apps, 1)
	assert.Equal(t, "SELECTION_INCLUDED", result.Apps[0].Selection)
}

// TestApplyIncludeMutation_Idempotent verifies that applying include twice returns the same state.
func TestApplyIncludeMutation_Idempotent(t *testing.T) {
	falseVal := false
	ns := instrumentation.App{Name: "ns", Autoinstrument: &falseVal}
	once := services.ApplyIncludeMutation(ns, "svc")
	twice := services.ApplyIncludeMutation(once, "svc")
	// Both should have exactly one INCLUDED override, no duplicates.
	require.Len(t, twice.Apps, 1)
	assert.Equal(t, "SELECTION_INCLUDED", twice.Apps[0].Selection)
}

package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunExclude_AutoinstrumentTrue_AddsExcluded verifies DWIM exclude logic:
// when autoinstrument=true, an EXCLUDED override must be added.
func TestRunExclude_AutoinstrumentTrue_AddsExcluded(t *testing.T) {
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
	err := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ts.setAppCalled.Load(), "Set must be called once to add EXCLUDED override when autoinstrument=true")
}

// TestRunExclude_AutoinstrumentFalse_NoOp verifies DWIM exclude logic:
// when autoinstrument=false the namespace default is already off — no override needed.
func TestRunExclude_AutoinstrumentFalse_NoOp(t *testing.T) {
	falseVal := false
	resp := buildGetAppResp(t, "c1", "ns", &falseVal, nil)

	ts := &includeTestServer{
		getAppResp: resp,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	require.NoError(t, err)
	assert.Equal(t, int64(0), ts.setAppCalled.Load(), "no Set call when autoinstrument=false (namespace default is already off)")
}

// TestRunExclude_NilAutoinstrument_NoOp verifies DWIM exclude logic:
// nil autoinstrument is treated the same as false — namespace default is off, no-op.
func TestRunExclude_NilAutoinstrument_NoOp(t *testing.T) {
	resp := buildGetAppResp(t, "c1", "ns", nil, nil)

	ts := &includeTestServer{
		getAppResp: resp,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	require.NoError(t, err)
	assert.Equal(t, int64(0), ts.setAppCalled.Load(), "no Set call when autoinstrument=nil")
}

// TestRunExclude_RemovesIncludedOverride verifies that exclude removes an existing
// INCLUDED override (and adds EXCLUDED since autoinstrument=true).
func TestRunExclude_RemovesIncludedOverride(t *testing.T) {
	trueVal := true
	resp := buildGetAppResp(t, "c1", "ns", &trueVal, []map[string]any{
		{"name": "svc", "selection": "SELECTION_INCLUDED"},
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
	err := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	require.NoError(t, err)
	assert.Equal(t, int64(1), ts.setAppCalled.Load(), "Set must be called once to replace INCLUDED with EXCLUDED")
}

// TestRunExclude_NamespaceNotFound verifies that exclude returns an error when the
// namespace is not in the cluster's app configuration.
// The workload is present in discovery (pre-flight passes), but GetAppInstrumentation
// returns an empty namespace list, so the original namespace-not-found error fires.
func TestRunExclude_NamespaceNotFound(t *testing.T) {
	ts := &includeTestServer{
		getAppResp: `{"cluster":{"name":"c1","namespaces":[]}}`,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "missing-ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	err := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "missing-ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "services exclude")
	assert.Contains(t, err.Error(), "missing-ns")
}

// TestRunExclude_Idempotent verifies that calling exclude twice when autoinstrument=true
// only results in one Set call (second call is a no-op).
func TestRunExclude_Idempotent(t *testing.T) {
	trueVal := true
	// Initial state: autoinstrument=true, no overrides.
	initialResp := buildGetAppResp(t, "c1", "ns", &trueVal, nil)
	// After first exclude: EXCLUDED override is present.
	afterFirstResp := buildGetAppResp(t, "c1", "ns", &trueVal, []map[string]any{
		{"name": "svc", "selection": "SELECTION_EXCLUDED"},
	})

	callCount := 0
	ts := &includeTestServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/discovery.v1.DiscoveryService/RunK8sDiscovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc"},
		}})
	})
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/GetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// First 3 calls (pre-check + 2 RMW reads) use initial state.
		// Subsequent calls (second invocation's pre-check) use post-write state.
		if callCount < 3 {
			_, _ = w.Write([]byte(initialResp))
		} else {
			_, _ = w.Write([]byte(afterFirstResp))
		}
		callCount++
	})
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/SetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		ts.setAppCalled.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := makeIncludeClient(t, srv.URL)

	// First invocation: should call SetApp once.
	var out1 bytes.Buffer
	err1 := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out1)
	require.NoError(t, err1, "first exclude must succeed")
	firstCallCount := ts.setAppCalled.Load()
	assert.Equal(t, int64(1), firstCallCount, "first exclude must call SetApp once")

	// Second invocation: reads post-write state (EXCLUDED already present) → no-op.
	var out2 bytes.Buffer
	err2 := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "c1", "ns", "svc", instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out2)
	require.NoError(t, err2, "second exclude must succeed (exit 0)")
	secondCallCount := ts.setAppCalled.Load()
	assert.Equal(t, firstCallCount, secondCallCount, "second exclude must be a no-op: no additional Set calls")
}

// TestRunExclude_WorkloadNotFound verifies that exclude returns a workload-not-found
// error when RunK8sDiscovery does not contain the requested service, and does NOT
// call GetAppInstrumentation (pre-flight short-circuits the operation).
//
//nolint:dupl // WorkloadNotFound tests for exclude/include/clear are intentionally symmetric.
func TestRunExclude_WorkloadNotFound(t *testing.T) {
	callCounts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCounts[r.URL.Path]++
		switch r.URL.Path {
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
	err := services.RunExclude(context.Background(), services.NewMutationTestIO(t), client, "prod-eu", "checkout", "nonexistent-svc",
		instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Resource not found")
	// Verify GetAppInstrumentation was NOT called (pre-flight short-circuited).
	assert.Zero(t, callCounts["/instrumentation.v1.InstrumentationService/GetAppInstrumentation"],
		"GetAppInstrumentation must not be called after pre-flight failure")
}

// ─── Unit tests for applyExcludeMutation ─────────────────────────────────────

// TestApplyExcludeMutation_AddsExcluded verifies that exclude adds EXCLUDED when autoinstrument=true.
func TestApplyExcludeMutation_AddsExcluded(t *testing.T) {
	trueVal := true
	ns := instrumentation.App{Name: "ns", Autoinstrument: &trueVal}
	result := services.ApplyExcludeMutation(ns, "svc")

	require.Len(t, result.Apps, 1)
	assert.Equal(t, "svc", result.Apps[0].Name)
	assert.Equal(t, "SELECTION_EXCLUDED", result.Apps[0].Selection)
}

// TestApplyExcludeMutation_NoOverrideWhenAutoinstrumentFalse verifies that exclude
// does NOT add EXCLUDED when autoinstrument=false (namespace default is already off).
func TestApplyExcludeMutation_NoOverrideWhenAutoinstrumentFalse(t *testing.T) {
	falseVal := false
	ns := instrumentation.App{Name: "ns", Autoinstrument: &falseVal}
	result := services.ApplyExcludeMutation(ns, "svc")
	assert.Empty(t, result.Apps)
}

// TestApplyExcludeMutation_NoOverrideWhenAutoinstrumentNil verifies that exclude
// does NOT add EXCLUDED when autoinstrument=nil (treated same as false).
func TestApplyExcludeMutation_NoOverrideWhenAutoinstrumentNil(t *testing.T) {
	ns := instrumentation.App{Name: "ns", Autoinstrument: nil}
	result := services.ApplyExcludeMutation(ns, "svc")
	assert.Empty(t, result.Apps)
}

// TestApplyExcludeMutation_RemovesIncluded verifies that exclude removes an INCLUDED override.
func TestApplyExcludeMutation_RemovesIncluded(t *testing.T) {
	trueVal := true
	ns := instrumentation.App{
		Name:           "ns",
		Autoinstrument: &trueVal,
		Apps: []instrumentation.AppOverride{
			{Name: "svc", Selection: "SELECTION_INCLUDED"},
		},
	}
	result := services.ApplyExcludeMutation(ns, "svc")
	// INCLUDED removed; EXCLUDED added (autoinstrument=true).
	require.Len(t, result.Apps, 1)
	assert.Equal(t, "SELECTION_EXCLUDED", result.Apps[0].Selection)
}

// TestApplyExcludeMutation_Idempotent verifies that applying exclude twice returns the same state.
func TestApplyExcludeMutation_Idempotent(t *testing.T) {
	trueVal := true
	ns := instrumentation.App{Name: "ns", Autoinstrument: &trueVal}
	once := services.ApplyExcludeMutation(ns, "svc")
	twice := services.ApplyExcludeMutation(once, "svc")
	// Both should have exactly one EXCLUDED override, no duplicates.
	require.Len(t, twice.Apps, 1)
	assert.Equal(t, "SELECTION_EXCLUDED", twice.Apps[0].Selection)
}

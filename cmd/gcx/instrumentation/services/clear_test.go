package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunClear_RemovesOverride verifies clear removes a per-workload override
// so the service falls back to the namespace default.
func TestRunClear_RemovesOverride(t *testing.T) {
	// Namespace has a SELECTION_INCLUDED override on "frontend".
	resp := buildGetAppResp(t, "c1", "grotshop", nil, []map[string]any{
		{"name": "frontend", "selection": "SELECTION_INCLUDED"},
	})

	ts := &includeTestServer{
		getAppResp: resp,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "grotshop", "name": "frontend"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunClear(context.Background(), client, "c1", "grotshop", "frontend", instrumentation.BackendURLs{}, &out)
	require.NoError(t, err, "clear must succeed")
	assert.Equal(t, int64(1), ts.setAppCalled.Load(), "Set must be called once to remove the override")
}

// TestRunClear_NoOp_NoOverride verifies clear is idempotent — if no override
// exists for the service, no Set call is made.
func TestRunClear_NoOp_NoOverride(t *testing.T) {
	// Namespace has no per-workload overrides.
	resp := buildGetAppResp(t, "c1", "grotshop", nil, nil)

	ts := &includeTestServer{
		getAppResp: resp,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "grotshop", "name": "frontend"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	var out bytes.Buffer
	err := services.RunClear(context.Background(), client, "c1", "grotshop", "frontend", instrumentation.BackendURLs{}, &out)
	require.NoError(t, err, "clear with no override must be a no-op (exit 0)")
	assert.Equal(t, int64(0), ts.setAppCalled.Load(), "no Set call when no override exists")
}

// TestRunClear_NoOp_NamespaceNotFound verifies that clear is a no-op when the namespace
// is not configured in the cluster's app configuration.
// The workload is present in discovery (pre-flight passes), but GetAppInstrumentation
// returns an empty namespace list, so runClear hits the namespace-not-found no-op path.
func TestRunClear_NoOp_NamespaceNotFound(t *testing.T) {
	ts := &includeTestServer{
		getAppResp: `{"cluster":{"name":"c1","namespaces":[]}}`,
		discoveryItems: []map[string]any{
			{"clusterName": "c1", "namespace": "missing-ns", "name": "svc"},
		},
	}
	srv := ts.start(t)
	client := makeIncludeClient(t, srv.URL)

	err := services.RunClear(context.Background(), client, "c1", "missing-ns", "svc", instrumentation.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err, "clear on missing namespace must be a no-op")
	assert.Equal(t, int64(0), ts.setAppCalled.Load(), "no Set call for missing namespace")
}

// TestRunClear_Idempotent_TwoCalls verifies that running clear twice exits 0 both times
// and the second call is a no-op.
func TestRunClear_Idempotent_TwoCalls(t *testing.T) {
	withOverrideResp := buildGetAppResp(t, "c1", "grotshop", nil, []map[string]any{
		{"name": "frontend", "selection": "SELECTION_INCLUDED"},
	})
	withoutOverrideResp := buildGetAppResp(t, "c1", "grotshop", nil, nil)

	callCount := 0
	ts := &includeTestServer{}
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
		// First 3 calls: pre-check + 2 RMW reads use the with-override state.
		// Subsequent calls: without-override state (post-clear).
		if callCount < 3 {
			_, _ = w.Write([]byte(withOverrideResp))
		} else {
			_, _ = w.Write([]byte(withoutOverrideResp))
		}
		callCount++
	})
	mux.HandleFunc(proxyFleetBase+"/instrumentation.v1.InstrumentationService/SetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		ts.setAppCalled.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := makeIncludeClient(t, srv.URL)

	// First clear: removes the INCLUDED override.
	err1 := services.RunClear(context.Background(), client, "c1", "grotshop", "frontend", instrumentation.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err1, "first clear must succeed")
	assert.Equal(t, int64(1), ts.setAppCalled.Load(), "first clear must call Set once")

	// Second clear: no override → no-op.
	err2 := services.RunClear(context.Background(), client, "c1", "grotshop", "frontend", instrumentation.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err2, "second clear must succeed (exit 0)")
	assert.Equal(t, int64(1), ts.setAppCalled.Load(), "second clear must be a no-op (no additional Set)")
}

// TestRunClear_WorkloadNotFound verifies that clear returns a workload-not-found
// error when RunK8sDiscovery does not contain the requested service, and does NOT
// call GetAppInstrumentation (pre-flight short-circuits the operation).
//
//nolint:dupl // WorkloadNotFound tests for exclude/include/clear are intentionally symmetric.
func TestRunClear_WorkloadNotFound(t *testing.T) {
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
	err := services.RunClear(context.Background(), client, "prod-eu", "checkout", "nonexistent-svc",
		instrumentation.BackendURLs{}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Resource not found")
	// Verify GetAppInstrumentation was NOT called (pre-flight short-circuited).
	assert.Zero(t, callCounts["/instrumentation.v1.InstrumentationService/GetAppInstrumentation"],
		"GetAppInstrumentation must not be called after pre-flight failure")
}

// ─── Unit tests for applyClearMutation ───────────────────────────────────────

// TestApplyClearMutation_RemovesIncluded verifies that clear removes an INCLUDED override.
func TestApplyClearMutation_RemovesIncluded(t *testing.T) {
	ns := instrumentation.App{
		Name: "ns",
		Apps: []instrumentation.AppOverride{
			{Name: "svc", Selection: "SELECTION_INCLUDED"},
		},
	}
	result := services.ApplyClearMutation(ns, "svc")
	assert.Empty(t, result.Apps, "INCLUDED override must be removed")
}

// TestApplyClearMutation_RemovesExcluded verifies that clear removes an EXCLUDED override.
func TestApplyClearMutation_RemovesExcluded(t *testing.T) {
	ns := instrumentation.App{
		Name: "ns",
		Apps: []instrumentation.AppOverride{
			{Name: "svc", Selection: "SELECTION_EXCLUDED"},
		},
	}
	result := services.ApplyClearMutation(ns, "svc")
	assert.Empty(t, result.Apps, "EXCLUDED override must be removed")
}

// TestApplyClearMutation_PreservesOtherOverrides verifies that clear only removes
// the targeted service's override, leaving other overrides intact.
func TestApplyClearMutation_PreservesOtherOverrides(t *testing.T) {
	ns := instrumentation.App{
		Name: "ns",
		Apps: []instrumentation.AppOverride{
			{Name: "svc-a", Selection: "SELECTION_INCLUDED"},
			{Name: "svc-b", Selection: "SELECTION_EXCLUDED"},
		},
	}
	result := services.ApplyClearMutation(ns, "svc-a")
	require.Len(t, result.Apps, 1, "only svc-a override must be removed")
	assert.Equal(t, "svc-b", result.Apps[0].Name)
}

// TestApplyClearMutation_Idempotent verifies that clearing twice gives the same result.
func TestApplyClearMutation_Idempotent(t *testing.T) {
	ns := instrumentation.App{
		Name: "ns",
		Apps: []instrumentation.AppOverride{
			{Name: "svc", Selection: "SELECTION_INCLUDED"},
		},
	}
	once := services.ApplyClearMutation(ns, "svc")
	twice := services.ApplyClearMutation(once, "svc")
	assert.Empty(t, twice.Apps, "clearing twice must still result in no overrides")
}

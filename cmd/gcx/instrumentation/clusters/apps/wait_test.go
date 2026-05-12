//nolint:testpackage // tests require access to unexported pollNamespaceStatus and fakeAppsClient
package apps

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation"
)

func TestPollNamespaceStatus(t *testing.T) {
	tests := []struct {
		name          string
		items         []instrumentation.DiscoveryItem
		cluster       string
		namespace     string
		wantOutcome   instrumentation.WaitOutcome
		wantRawStatus instrumentation.InstrumentationStatus
		wantErr       bool
	}{
		{
			name:          "no items → WaitPending",
			items:         nil,
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitPending,
			wantRawStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION",
		},
		{
			name: "items from other cluster/namespace ignored",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "other-cluster", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
				{ClusterName: "c1", Namespace: "other-ns", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitPending, // no items for (c1, grotshop)
			wantRawStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION",
		},
		{
			name: "INSTRUMENTATION_ERROR trumps all — full wire prefix",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitError,
			wantRawStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR",
		},
		{
			name: "pending present → WaitPending — full wire prefix",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitPending,
			wantRawStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION",
		},
		{
			name: "all INSTRUMENTED → WaitSuccess — full wire prefix",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitSuccess,
			wantRawStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED",
		},
		{
			// Case A (wire bug): all workloads report full-prefix PENDING.
			// Old code: compared against shorthand "PENDING_INSTRUMENTATION" → no match
			// → treated as terminal → exited 0 immediately (bug).
			// New code: ClassifyInstrumentationStatus recognises full prefix → WaitPending.
			name: "Case A: all PENDING full-wire-prefix → WaitPending (not Success)",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitPending,
			wantRawStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION",
		},
		{
			// Case B: one workload reports full-prefix INSTRUMENTATION_ERROR → WaitError.
			name: "Case B: INSTRUMENTATION_ERROR full-wire-prefix → WaitError",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitError,
			wantRawStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR",
		},
		{
			// Case C: all workloads report full-prefix INSTRUMENTED → WaitSuccess.
			name: "Case C: all INSTRUMENTED full-wire-prefix → WaitSuccess",
			items: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
			},
			cluster:       "c1",
			namespace:     "grotshop",
			wantOutcome:   instrumentation.WaitSuccess,
			wantRawStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeAppsClient{
				discoverItems: tc.items,
			}

			ctx := t.Context()
			outcome, rawStatus, err := pollNamespaceStatus(ctx, client, instrumentation.PromHeaders{}, tc.cluster, tc.namespace)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if outcome != tc.wantOutcome {
				t.Errorf("outcome: want %v, got %v", tc.wantOutcome, outcome)
			}
			if rawStatus != tc.wantRawStatus {
				t.Errorf("rawStatus: want %q, got %q", tc.wantRawStatus, rawStatus)
			}
		})
	}
}

func TestWaitCmd_Timeout(t *testing.T) {
	// Always returns full-prefix PENDING_INSTRUMENTATION — should timeout quickly.
	// Timeout emits a fused WaitResult (with Error) to stdout and returns
	// ErrWaitTimeoutEmitted (sentinel), not a plain "timed out" error string.
	//
	// Pin agent mode so JSON assertions are stable in CI (where CLAUDECODE is not set).
	// agent.IsAgentMode() uses a cached init()-time value, so ResetForTesting() must
	// be called after t.Setenv to re-run env detection.
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(func() { agent.ResetForTesting() }) // restore after test

	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
		},
	}

	cmd := newWaitCmd(client, instrumentation.PromHeaders{})

	// Use a very short timeout to keep the test fast.
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=100ms"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	start := time.Now()
	err := cmd.Execute()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// timeout now returns the sentinel, not a "timed out" message string.
	if !errors.Is(err, instrumentation.ErrWaitTimeoutEmitted) {
		t.Errorf("expected ErrWaitTimeoutEmitted sentinel, got: %v", err)
	}
	// stdout must have the fused WaitResult with outcome:timeout and error field.
	stdoutStr := stdout.String()
	if !strings.Contains(stdoutStr, `"outcome":"timeout"`) {
		t.Errorf("stdout must contain outcome:timeout, got: %q", stdoutStr)
	}
	if !strings.Contains(stdoutStr, `"error"`) {
		t.Errorf("stdout must contain error field in fused envelope, got: %q", stdoutStr)
	}
	// Sanity: should not have run for more than 10 seconds.
	if elapsed > 10*time.Second {
		t.Errorf("test took too long: %v", elapsed)
	}
}

func TestWaitCmd_ErrorStatus(t *testing.T) {
	// Returns full-prefix INSTRUMENTATION_ERROR immediately.
	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR"},
		},
	}

	cmd := newWaitCmd(client, instrumentation.PromHeaders{})
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on INSTRUMENTATION_ERROR, got nil")
	}
	if !strings.Contains(err.Error(), "INSTRUMENTATION_ERROR") {
		t.Errorf("expected INSTRUMENTATION_ERROR in error, got: %v", err)
	}
}

func TestWaitCmd_Success(t *testing.T) {
	// Returns full-prefix INSTRUMENTED immediately.
	// success output goes to stdout; no progress text bleeds to stdout.
	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
		},
	}

	cmd := newWaitCmd(client, instrumentation.PromHeaders{})
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// stdout: success message with namespace and cluster.
	stdoutStr := stdout.String()
	if !strings.Contains(stdoutStr, "grotshop") {
		t.Errorf("stdout should contain namespace, got: %q", stdoutStr)
	}
	if !strings.Contains(stdoutStr, "INSTRUMENTED") {
		t.Errorf("stdout should contain status, got: %q", stdoutStr)
	}
	// No progress text should bleed into stdout.
	if strings.Contains(stdoutStr, "waiting:") {
		t.Errorf("progress text must not appear on stdout, got: %q", stdoutStr)
	}
}

func TestProbePipelineMsg(t *testing.T) {
	tests := []struct {
		name             string
		cluster          string
		pipelines        []instrumentation.Pipeline
		listPipelinesErr error
		wantContains     string
		wantEmpty        bool
	}{
		{
			name:    "pipeline found → exists message",
			cluster: "prod-k8s",
			pipelines: []instrumentation.Pipeline{
				{Name: "beyla_k8s_appo11y_prod-k8s"},
			},
			wantContains: `"beyla_k8s_appo11y_prod-k8s" exists`,
		},
		{
			name:         "pipeline not found → not found message with hint",
			cluster:      "prod-k8s",
			pipelines:    nil,
			wantContains: `"beyla_k8s_appo11y_prod-k8s" not found`,
		},
		{
			name:    "other pipelines present but not matching → not found message",
			cluster: "prod-k8s",
			pipelines: []instrumentation.Pipeline{
				{Name: "beyla_k8s_appo11y_other-cluster"},
			},
			wantContains: `"beyla_k8s_appo11y_prod-k8s" not found`,
		},
		{
			name:             "ListPipelines error → empty string (no diagnostic noise)",
			cluster:          "prod-k8s",
			listPipelinesErr: errors.New("permission denied"),
			wantEmpty:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeAppsClient{
				pipelines:        tc.pipelines,
				listPipelinesErr: tc.listPipelinesErr,
			}

			got := probePipelineMsg(t.Context(), client, tc.cluster)

			if tc.wantEmpty {
				if got != "" {
					t.Errorf("expected empty string, got: %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantContains) {
				t.Errorf("expected %q in result, got: %q", tc.wantContains, got)
			}
		})
	}
}

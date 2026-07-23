//nolint:testpackage // tests require access to unexported pollNamespaceStatus and fakeAppsClient
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/spf13/cobra"
)

// pinAgentMode forces agent-mode detection on for the duration of the test.
// agent.IsAgentMode() caches an init()-time value, so ResetForTesting() must
// re-run env detection after t.Setenv (and again on cleanup).
func pinAgentMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "true")
	agent.ResetForTesting()
	t.Cleanup(func() { agent.ResetForTesting() })
}

// pinHumanMode forces agent-mode detection off (GCX_AGENT_MODE=false overrides
// any ambient CLAUDECODE/CLAUDE_CODE detection) for the duration of the test.
func pinHumanMode(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(func() { agent.ResetForTesting() })
}

// parseJSONLines fails the test unless every non-empty line of s is one JSON
// object; it returns the parsed documents in order.
func parseJSONLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var docs []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		if line == "" {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line must parse as JSON: %q: %v", line, err)
		}
		docs = append(docs, doc)
	}
	return docs
}

// requireStreamEnd asserts docs is exactly one typed terminal stream_end line
// with the given outcome and returns it.
func requireStreamEnd(t *testing.T, docs []map[string]any, wantOutcome string) map[string]any {
	t.Helper()
	if len(docs) != 1 {
		t.Fatalf("stdout must carry exactly one terminal line, got %d: %v", len(docs), docs)
	}
	doc := docs[0]
	if doc["type"] != cmdio.StreamEndType {
		t.Errorf("terminal type: want %q, got %v", cmdio.StreamEndType, doc["type"])
	}
	if doc["schema_version"] != cmdio.StreamSchemaVersion {
		t.Errorf("terminal schema_version: want %q, got %v", cmdio.StreamSchemaVersion, doc["schema_version"])
	}
	if doc["outcome"] != wantOutcome {
		t.Errorf("terminal outcome: want %q, got %v", wantOutcome, doc["outcome"])
	}
	return doc
}

// failWriter fails every write with err — simulates ENOSPC/EIO on stdout.
type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

// silenceCobra mirrors the production root command, which sets SilenceUsage
// and SilenceErrors and renders errors itself — otherwise a bare test command
// pollutes the captured stdout with cobra's usage text on error.
func silenceCobra(cmd *cobra.Command) *cobra.Command {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

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
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))

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
	// stdout must have exactly one typed terminal line: the fused WaitResult
	// with outcome:timeout and error field.
	doc := requireStreamEnd(t, parseJSONLines(t, stdout.String()), "timeout")
	if doc["error"] == nil {
		t.Errorf("fused terminal must carry the error field, got: %v", doc)
	}
	// Every stderr progress line is a typed, versioned stream event.
	for i, d := range parseJSONLines(t, stderr.String()) {
		if d["type"] != cmdio.StreamEventType {
			t.Errorf("stderr line %d type: want %q, got %v", i, cmdio.StreamEventType, d["type"])
		}
		if d["schema_version"] != cmdio.StreamSchemaVersion {
			t.Errorf("stderr line %d schema_version: want %q, got %v", i, cmdio.StreamSchemaVersion, d["schema_version"])
		}
	}
	// Sanity: should not have run for more than 10 seconds.
	if elapsed > 10*time.Second {
		t.Errorf("test took too long: %v", elapsed)
	}
}

func TestWaitCmd_AgentModeSuccessTyped(t *testing.T) {
	// Agent-mode success: stdout is exactly one typed gcx.stream_end line.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := requireStreamEnd(t, parseJSONLines(t, stdout.String()), "success")
	if doc["status"] != "INSTRUMENTATION_STATUS_INSTRUMENTED" {
		t.Errorf("terminal status: got %v", doc["status"])
	}
}

func TestWaitCmd_AgentModeErrorStatusEmitsTerminal(t *testing.T) {
	// INSTRUMENTATION_ERROR in agent mode must emit exactly one typed
	// gcx.stream_end line (outcome:error) and return an EmittedError so the
	// reporter appends nothing more to stdout.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on INSTRUMENTATION_ERROR, got nil")
	}
	var emitted *gcxerrors.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("expected EmittedError after terminal write, got: %v", err)
	}
	if emitted.Code != gcxerrors.ExitGeneralError {
		t.Errorf("exit code: want %d, got %d", gcxerrors.ExitGeneralError, emitted.Code)
	}

	doc := requireStreamEnd(t, parseJSONLines(t, stdout.String()), "error")
	if doc["error"] == nil {
		t.Errorf("fused terminal must carry the error field, got: %v", doc)
	}
}

func TestWaitCmd_AgentModePollErrorRetriesTyped(t *testing.T) {
	// A transient poll RPC failure must not abort the wait: the loop emits a
	// typed poll_error stream event on stderr and keeps polling until the
	// namespace reaches a stable state.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverErrs: []error{errors.New("transient RPC failure")},
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("transient poll error must not abort the wait, got: %v", err)
	}

	// The retried wait still terminates with the success stream_end line.
	requireStreamEnd(t, parseJSONLines(t, stdout.String()), "success")

	// stderr carries the typed poll_error event; every line stays a typed,
	// versioned stream event.
	var sawPollError bool
	for i, d := range parseJSONLines(t, stderr.String()) {
		if d["type"] != cmdio.StreamEventType {
			t.Errorf("stderr line %d type: want %q, got %v", i, cmdio.StreamEventType, d["type"])
		}
		if d["schema_version"] != cmdio.StreamSchemaVersion {
			t.Errorf("stderr line %d schema_version: want %q, got %v", i, cmdio.StreamSchemaVersion, d["schema_version"])
		}
		if d["event"] == "poll_error" {
			sawPollError = true
			errStr, _ := d["error"].(string)
			if !strings.Contains(errStr, "transient RPC failure") {
				t.Errorf("poll_error event error: want RPC failure text, got %v", d["error"])
			}
		}
	}
	if !sawPollError {
		t.Error("stderr must carry the typed poll_error event")
	}
}

func TestWaitCmd_HumanModePollErrorRetryLine(t *testing.T) {
	// Human mode: the transient poll failure emits the retry line on stderr
	// (byte-identical to the clusters wait sibling) and the wait still succeeds.
	pinHumanMode(t)

	client := &fakeAppsClient{
		discoverErrs: []error{errors.New("transient RPC failure")},
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTED"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("transient poll error must not abort the wait, got: %v", err)
	}

	if want := "  poll error (retrying): transient RPC failure\n"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr must carry the human retry line %q, got: %q", want, stderr.String())
	}
	if !strings.Contains(stdout.String(), "INSTRUMENTED") {
		t.Errorf("stdout should contain the success status, got: %q", stdout.String())
	}
}

func TestWaitCmd_AgentModeCanceledMidPollEmitsTerminal(t *testing.T) {
	// A cancellation arriving mid-poll surfaces as a poll error wrapping
	// context.Canceled — it must route to the canceled gcx.stream_end
	// terminal (with the cancellation exit code), not return a bare poll error.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverErr: fmt.Errorf("rpc: %w", context.Canceled),
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cmd.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("expected error on mid-poll cancellation, got nil")
	}
	var emitted *gcxerrors.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("expected EmittedError after terminal write, got: %v", err)
	}
	if emitted.Code != gcxerrors.ExitCancelled {
		t.Errorf("exit code: want %d, got %d", gcxerrors.ExitCancelled, emitted.Code)
	}

	doc := requireStreamEnd(t, parseJSONLines(t, stdout.String()), "canceled")
	if doc["error"] == nil {
		t.Errorf("fused terminal must carry the error field, got: %v", doc)
	}
}

func TestWaitCmd_AgentModeCanceledEmitsTerminal(t *testing.T) {
	// Context cancellation in agent mode must still emit the terminal
	// gcx.stream_end line (outcome:canceled) and return an EmittedError with
	// the cancellation exit code.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=5m"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cmd.ExecuteContext(ctx)
	if err == nil {
		t.Fatal("expected error on cancellation, got nil")
	}
	var emitted *gcxerrors.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("expected EmittedError after terminal write, got: %v", err)
	}
	if emitted.Code != gcxerrors.ExitCancelled {
		t.Errorf("exit code: want %d, got %d", gcxerrors.ExitCancelled, emitted.Code)
	}

	doc := requireStreamEnd(t, parseJSONLines(t, stdout.String()), "canceled")
	if doc["error"] == nil {
		t.Errorf("fused terminal must carry the error field, got: %v", doc)
	}
}

func TestWaitCmd_TimeoutWriteFailureReturnsWriteError(t *testing.T) {
	// When the fused terminal write fails (ENOSPC/EIO), the sentinel must NOT
	// be returned — the write error itself surfaces.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=50ms"})

	writeErr := errors.New("no space left on device")
	var stderr strings.Builder
	cmd.SetOut(failWriter{err: writeErr})
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, instrumentation.ErrWaitTimeoutEmitted) {
		t.Errorf("sentinel must not be returned when the terminal write failed, got: %v", err)
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("the write error itself must surface, got: %v", err)
	}
}

func TestWaitCmd_ErrorStatus(t *testing.T) {
	// Returns full-prefix INSTRUMENTATION_ERROR immediately.
	client := &fakeAppsClient{
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", InstrumentationStatus: "INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR"},
		},
	}

	cmd := silenceCobra(newWaitCmd(client))
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

	cmd := silenceCobra(newWaitCmd(client))
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

func TestWaitCmd_TimeoutNamesPersistentPollError(t *testing.T) {
	// A persistent poll failure (bad token, DNS) retries until the deadline;
	// the timeout terminal must name the real cause instead of reporting a
	// bare timeout with the cause buried in stderr retry lines.
	pinAgentMode(t)

	client := &fakeAppsClient{
		discoverErr: errors.New("401 unauthorized"),
	}

	cmd := silenceCobra(newWaitCmd(client))
	cmd.SetArgs([]string{"c1", "grotshop", "--timeout=100ms"})

	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if !errors.Is(err, instrumentation.ErrWaitTimeoutEmitted) {
		t.Fatalf("expected ErrWaitTimeoutEmitted sentinel, got: %v", err)
	}

	doc := requireStreamEnd(t, parseJSONLines(t, stdout.String()), "timeout")
	waitErr, ok := doc["error"].(map[string]any)
	if !ok {
		t.Fatalf("fused terminal must carry the error field, got: %v", doc)
	}
	details, _ := waitErr["details"].(string)
	if !strings.Contains(details, "last poll error") || !strings.Contains(details, "401 unauthorized") {
		t.Errorf("timeout details must name the persistent poll error, got: %q", details)
	}
}

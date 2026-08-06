//go:build !windows

// Signals drive these tests, and os.Interrupt cannot be sent to another
// process on Windows. The outcome classification itself is covered portably by
// the buildUsageEvent tests in telemetry_internal_test.go.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	usageEventProcessHelper = "GCX_USAGE_EVENT_PROCESS_HELPER"
	usageEventConfigEnv     = "GCX_USAGE_EVENT_CONFIG"

	// helperRequestPath is the path the helper command asks for, and the only
	// path these tests treat as the command's own request.
	helperRequestPath = "/api/health"
)

// handshakeTimeout bounds each wait for the child to reach the next step. Every
// wait is released by an event (a request arriving), never by a sleep, so this
// only ever fires when the behaviour under test is broken.
const handshakeTimeout = 30 * time.Second

// TestUsageEventProcessHelper is the child process for the tests in this file.
// It runs the real main(), so the signal handling, the exit-code
// classification, and the synchronous usage export are the shipped ones rather
// than a re-creation of them here.
func TestUsageEventProcessHelper(_ *testing.T) {
	if os.Getenv(usageEventProcessHelper) != "1" {
		return
	}

	agent.ResetForTesting()
	os.Args = []string{"gcx", "api", helperRequestPath, "--config", os.Getenv(usageEventConfigEnv)}
	main()
}

// TestCanceledInvocationIsReportedAndSecondSignalTerminates proves both halves
// of the cancellation contract against a real process:
//
//  1. The first Ctrl-C reports the invocation as canceled — with exit code 5, a
//     real duration, and error_kind present but empty — instead of reporting
//     nothing, and writes no result document.
//  2. A second Ctrl-C, sent while the synchronous export is still in flight,
//     terminates the process rather than being swallowed until the export
//     timeout expires.
//
// Both waits are handshakes on a request arriving, so neither signal is sent on
// a timer.
func TestCanceledInvocationIsReportedAndSecondSignalTerminates(t *testing.T) {
	// Deliberately not parallel: it changes this process's signal handling and
	// sends signals to a child.
	hold := make(chan struct{})
	reached := make(chan struct{}, 1)

	// A Grafana that hangs on the command's own request and answers anything
	// else immediately.
	//
	// The interrupt has to land while that one request is in flight and nothing
	// else is, which is why the config pins a stack-id: without one, gcx
	// resolves its namespace by asking the server for /bootdata first
	// (internal/config/stack_id.go), and interrupting *that* is a different
	// test — discovery swallows a cancellation and falls back, so the error the
	// command finally surfaces would depend on which of two requests lost the
	// race. The 404 keeps that true if any other lookup ever appears.
	//
	// The hanging path never answers, not even once the client context is
	// canceled: a response that races the cancellation lets the client read
	// headers and then fail reading the body, which surfaces as a plain error
	// rather than a cancellation.
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != helperRequestPath {
			http.NotFound(w, r)
			return
		}
		select {
		case reached <- struct{}{}:
		default:
		}
		<-hold
	}))
	t.Cleanup(grafana.Close)

	// A usage-stats receiver that captures the event, sends the second signal
	// from inside the handler, and holds the response open while it lands.
	held := newHeldExport(t, hold)

	// Registered last so it runs first: releasing the handlers before the
	// servers close keeps Close from blocking on the held connection.
	t.Cleanup(func() { close(hold) })

	helper := startUsageEventHelper(t, grafana.URL, held.server.URL)
	held.interrupts(helper.cmd.Process)

	recvWithin(t, reached, "the command's own request to reach the Grafana API")
	helper.interrupt(t)

	require.NoError(t, recvWithin(t, held.signals, "the second interrupt to be sent during the export"))
	err := helper.cmd.Wait()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr,
		"the process should have been terminated by the second signal, got %v", err)
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	require.True(t, status.Signaled(),
		"process exited with code %d instead of being terminated: the second SIGINT was swallowed and the export ran to its timeout; stderr=%q",
		exitErr.ExitCode(), helper.stderr.String())
	assert.Equal(t, syscall.SIGINT, status.Signal())

	// A cancellation reports no error on either stream: no fused document on
	// stdout, and no rendered error on stderr either. Without the stderr half,
	// a change that let the converter chain print "Operation cancelled" would
	// keep this test green.
	assert.Empty(t, helper.stdout.String(),
		"a canceled command writes no result document; stderr=%q", helper.stderr.String())
	assert.Empty(t, helper.stderr.String(),
		"a canceled command reports no error")

	fields := decodeEvent(t, recvWithin(t, held.events, "the usage event to be exported"))
	assert.Equal(t, telemetry.OutcomeCanceled, fields["outcome"],
		"a Ctrl-C must be reported as a cancellation, not as a failure; event=%v child stderr=%q",
		fields, helper.stderr.String())
	assert.InDelta(t, float64(gcxerrors.ExitCancelled), fields["exit_code"], 0)
	require.Contains(t, fields, "error_kind",
		"error_kind must stay on the wire for a canceled invocation")
	assert.Empty(t, fields["error_kind"])
	duration, ok := fields["duration_ms"].(float64)
	require.True(t, ok, "duration_ms missing from %v", fields)
	assert.Positive(t, duration, "a canceled invocation must report the time it really ran")

	// A canceled run is not a failure: whatever a probe captured before the
	// interrupt landed, neither failure-depth field may travel. The auth
	// category is not a failure fact and stays — the context was resolved
	// before the hanging request was ever sent.
	assert.NotContains(t, fields, "http_status")
	assert.NotContains(t, fields, "k8s_reason")
	assert.Equal(t, "token", fields["grafana_auth_method"],
		"cancellation must not suppress the auth category")
}

// TestFinishedRunSurvivesInterruptDuringExport pins the other half of the
// signal contract: the escape hatch belongs to an invocation the user is trying
// to abandon, and must not hand a stray interrupt the power to kill one that
// already succeeded.
//
// The command writes its complete result, then waits out the synchronous export
// against a receiver that never answers. An interrupt arriving in that window
// has to be absorbed, so the process still exits 0 and its exit status still
// agrees with the document on stdout. Disarming the signal handler on every
// exit path instead makes this process die by SIGINT with status 130.
func TestFinishedRunSurvivesInterruptDuringExport(t *testing.T) {
	hold := make(chan struct{})

	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != helperRequestPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"database":"ok"}`))
	}))
	t.Cleanup(grafana.Close)

	held := newHeldExport(t, hold)
	t.Cleanup(func() { close(hold) })

	helper := startUsageEventHelper(t, grafana.URL, held.server.URL)
	held.interrupts(helper.cmd.Process)

	require.NoError(t, recvWithin(t, held.signals, "the interrupt to be sent during the export"))
	err := helper.cmd.Wait()

	assert.Equal(t, gcxerrors.ExitSuccess, helper.cmd.ProcessState.ExitCode(),
		"an interrupt during the export must not overwrite the outcome of a command that already succeeded: wait err=%v stdout=%q stderr=%q",
		err, helper.stdout.String(), helper.stderr.String())
	assert.Contains(t, helper.stdout.String(), "database",
		"the result the command already wrote must survive")

	fields := decodeEvent(t, recvWithin(t, held.events, "the usage event to be exported"))
	assert.Equal(t, telemetry.OutcomeOK, fields["outcome"])
}

// TestUsageEventUnchangedForSuccessAndFailure guards the paths that now share
// the cancellation exit funnel: an ordinary success and an ordinary runtime
// error must keep their exit code, their stream ownership, and their outcome.
func TestUsageEventUnchangedForSuccessAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		body          string
		wantExitCode  int
		wantOutcome   string
		wantErrorKind string
	}{
		{
			name: "success", status: http.StatusOK, body: `{"database":"ok"}`,
			wantExitCode: gcxerrors.ExitSuccess, wantOutcome: telemetry.OutcomeOK,
		},
		{
			name: "runtime error", status: http.StatusInternalServerError, body: `{"message":"boom"}`,
			wantExitCode: gcxerrors.ExitGeneralError, wantOutcome: telemetry.OutcomeRuntimeError,
			wantErrorKind: "error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan []byte, 1)

			// Only the command's own path gets the case's response, so the
			// asserted outcome can only come from the command's own request.
			grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != helperRequestPath {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(grafana.Close)

			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					return
				}
				select {
				case events <- body:
				default:
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(receiver.Close)

			helper := startUsageEventHelper(t, grafana.URL, receiver.URL)
			err := helper.cmd.Wait()

			assert.Equal(t, tc.wantExitCode, helper.cmd.ProcessState.ExitCode(),
				"wait err=%v stdout=%q stderr=%q", err, helper.stdout.String(), helper.stderr.String())

			fields := decodeEvent(t, recvWithin(t, events, "the usage event to be exported"))
			assert.Equal(t, tc.wantOutcome, fields["outcome"])
			assert.Equal(t, tc.wantErrorKind, fields["error_kind"])
			assert.InDelta(t, float64(tc.wantExitCode), fields["exit_code"], 0)

			// The child's config selects auth-method token (with GRAFANA_TOKEN
			// set), so the auth category rides every outcome; gcx api talks
			// plain HTTP, so no Kubernetes reason can appear on either.
			assert.Equal(t, "token", fields["grafana_auth_method"],
				"a resolved Grafana context reports its auth category on any outcome")
			assert.NotContains(t, fields, "k8s_reason")

			if tc.wantExitCode == gcxerrors.ExitSuccess {
				assert.NotContains(t, fields, "http_status",
					"a successful request carries no failure status")
				assert.Contains(t, helper.stdout.String(), "database",
					"the response belongs on stdout")
				return
			}
			assert.InDelta(t, float64(tc.status), fields["http_status"], 0,
				"the failing request's transport status must reach the event")
			assert.Empty(t, helper.stdout.String(),
				"a failed command writes no result document in human mode")
			assert.NotEmpty(t, helper.stderr.String(),
				"a human consumer needs the error on stderr")
		})
	}
}

// heldExport is a usage-stats receiver that captures the event, interrupts the
// child while the response is still open, and holds that response until the
// test releases it — so the interrupt provably lands while the child is inside
// its synchronous export.
//
// The interrupt is sent from the handler rather than from the test goroutine
// because the export gives up after one second (internal/telemetry/export.go),
// and that clock starts when the request arrives. A test goroutine descheduled
// for longer than that would signal a child which had already exported and
// exited cleanly, then report the code as having swallowed a signal it never
// saw. From inside the handler there is no such window.
type heldExport struct {
	server  *httptest.Server
	events  chan []byte
	signals chan error

	// child is closed once the process to interrupt is known. The handler waits
	// for it rather than reading the pointer opportunistically: an export that
	// arrived before the test had stored the process would otherwise find no
	// one to signal, skip silently, and leave the test waiting out its whole
	// handshake timeout for a signal that was never going to be sent.
	child   chan struct{}
	process atomic.Pointer[os.Process]
}

// interrupts releases the handler to signal the given process. It must be
// called once, immediately after the child is started.
func (h *heldExport) interrupts(process *os.Process) {
	h.process.Store(process)
	close(h.child)
}

func newHeldExport(t *testing.T, hold <-chan struct{}) *heldExport {
	t.Helper()

	held := &heldExport{
		events:  make(chan []byte, 1),
		signals: make(chan error, 1),
		child:   make(chan struct{}),
	}
	held.server = httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		select {
		case held.events <- body:
		default:
		}
		// Wait for the process, then interrupt it. Reported through a channel
		// rather than asserted: this is not the test goroutine. The hold case
		// keeps a handler from outliving a test that failed before starting a
		// child.
		select {
		case <-held.child:
		case <-hold:
			return
		}
		if process := held.process.Load(); process != nil {
			select {
			case held.signals <- process.Signal(os.Interrupt):
			default:
			}
		}
		<-hold
	}))
	t.Cleanup(held.server.Close)
	return held
}

// usageEventHelper is a started child process plus its captured streams.
type usageEventHelper struct {
	cmd            *exec.Cmd
	stdout, stderr *syncBuffer
}

func (h *usageEventHelper) interrupt(t *testing.T) {
	t.Helper()
	require.NoError(t, h.cmd.Process.Signal(os.Interrupt))
}

// syncBuffer collects a child stream so the test can read it while the child is
// still running — a failing assertion mid-run wants the error the child printed.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startUsageEventHelper starts the helper process against the given Grafana and
// usage-stats endpoints, with telemetry enabled and every ambient credential,
// config, and state path pointed somewhere harmless.
func startUsageEventHelper(t *testing.T, grafanaURL, endpoint string) *usageEventHelper {
	t.Helper()

	// exec resets a caught signal to SIG_DFL in the child but preserves an
	// inherited SIG_IGN, and a background job of a non-interactive shell
	// inherits SIGINT as SIG_IGN. Catching it here therefore gives the child
	// the SIGINT disposition a foreground process in a terminal has, whatever
	// the shell that started `go test` did.
	parent := make(chan os.Signal, 1)
	signal.Notify(parent, os.Interrupt)
	t.Cleanup(func() { signal.Stop(parent) })

	helper := &usageEventHelper{stdout: &syncBuffer{}, stderr: &syncBuffer{}}
	// Re-exec the trusted current test binary so the child runs the real main().
	helper.cmd = exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUsageEventProcessHelper$") //nolint:gosec
	helper.cmd.Stdout = helper.stdout
	helper.cmd.Stderr = helper.stderr
	helper.cmd.Env = append(os.Environ(),
		usageEventProcessHelper+"=1",
		usageEventConfigEnv+"="+writeUsageEventConfig(t, grafanaURL),
		"GCX_TELEMETRY=enabled",
		"GCX_TELEMETRY_ENDPOINT="+endpoint,
		"GCX_AGENT_MODE=false",
		"GCX_NO_UPDATE_NOTIFIER=1",
		"NO_COLOR=1",
		"HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"XDG_CONFIG_DIRS="+t.TempDir(),
		"XDG_CACHE_HOME="+t.TempDir(),
		"XDG_STATE_HOME="+t.TempDir(),
		"GCX_CONFIG=",
		"GRAFANA_SERVER="+grafanaURL,
		"GRAFANA_TOKEN=synthetic-usage-event-token",
		"GRAFANA_USER=",
		"GRAFANA_PASSWORD=",
		"GRAFANA_PROXY_ENDPOINT=",
		"GRAFANA_ORG_ID=",
		"GRAFANA_STACK_ID=",
	)
	require.NoError(t, helper.cmd.Start())
	return helper
}

// writeUsageEventConfig writes the child's config. stack-id is pinned so the
// child makes exactly one request — its own — rather than resolving its
// namespace over the network first; see the comment in the cancellation test.
func writeUsageEventConfig(t *testing.T, grafanaURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := fmt.Sprintf(`version: 1
stacks:
  usage:
    grafana:
      server: %q
      org-id: 1
      stack-id: 12345
      auth-method: token
contexts:
  usage:
    stack: usage
current-context: usage
`, grafanaURL)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func decodeEvent(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var fields map[string]any
	require.NoError(t, json.Unmarshal(payload, &fields), "payload=%q", payload)
	return fields
}

// recvWithin waits for one value, failing with what was expected rather than
// letting the whole test binary time out.
func recvWithin[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(handshakeTimeout):
		t.Fatalf("timed out after %v waiting for %s", handshakeTimeout, what)
		var zero T
		return zero
	}
}

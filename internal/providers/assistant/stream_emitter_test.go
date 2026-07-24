package assistant //nolint:testpackage // exercises the unexported stream emitter directly

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setAgentMode forces agent mode on or off for the duration of the test.
// Must be called BEFORE the emitter under test is constructed: the mode is
// resolved once in newStreamEmitter.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(agent.ResetForTesting)
}

func TestNewStreamEmitterModeResolution(t *testing.T) {
	tests := []struct {
		name      string
		jsonOut   bool
		noStream  bool
		agentMode bool
		want      streamMode
	}{
		{name: "default TTY", want: modeHuman},
		{name: "agent mode", agentMode: true, want: modeAgent},
		{name: "--json", jsonOut: true, want: modeJSONStream},
		{name: "--json --no-stream", jsonOut: true, noStream: true, want: modeJSONDoc},
		// Explicit --json flags always win over agent-mode detection.
		{name: "agent mode with --json keeps legacy NDJSON", jsonOut: true, agentMode: true, want: modeJSONStream},
		{name: "agent mode with --json --no-stream keeps legacy doc", jsonOut: true, noStream: true, agentMode: true, want: modeJSONDoc},
		// --no-stream without --json has never changed the human output.
		{name: "--no-stream alone stays human", noStream: true, want: modeHuman},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, tt.agentMode)
			em := newStreamEmitter(&bytes.Buffer{}, &bytes.Buffer{}, &promptOpts{jsonOut: tt.jsonOut, noStream: tt.noStream})
			assert.Equal(t, tt.want, em.mode)
		})
	}
}

// sseBody builds an SSE response body from raw JSON-RPC result payloads.
func sseBody(results ...string) string {
	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(`data: {"jsonrpc":"2.0","result":` + r + "}\n\n")
	}
	return sb.String()
}

const (
	sseStatusWorking   = `{"kind":"status-update","taskId":"task-1","contextId":"ctx-1","status":{"state":"working"},"final":false}`
	sseToolCall        = `{"kind":"artifact-update","taskId":"task-1","contextId":"ctx-1","artifact":{"kind":"artifact","artifactId":"a1","name":"step.toolCall","parts":[{"kind":"data","data":{"toolName":"run_query"}}]}}`
	sseApproval        = `{"kind":"artifact-update","taskId":"task-1","contextId":"ctx-1","artifact":{"kind":"artifact","artifactId":"a2","name":"step.approval","parts":[{"kind":"data","data":{"id":"approval-1","chatId":"chat-1","tenantId":"t1","userId":"u1","toolName":"write_dashboard","description":"Create dashboard"}}]}}`
	sseMessage         = `{"kind":"artifact-update","taskId":"task-1","contextId":"ctx-1","artifact":{"kind":"artifact","artifactId":"a3","name":"step.message","parts":[{"kind":"text","text":"Here is the answer"}]}}`
	sseStatusCompleted = `{"kind":"status-update","taskId":"task-1","contextId":"ctx-1","status":{"state":"completed"},"final":true}`
)

// newSSEFixture serves the given SSE body from the A2A agent endpoint and
// records any approval submissions. It reuses the real internal/assistant
// client and SSE parser — only the HTTP boundary is faked.
func newSSEFixture(t *testing.T, body string) (*assistant.Client, *[]bool) {
	t.Helper()
	var recorded []bool
	mux := http.NewServeMux()
	base := "/api/plugins/grafana-assistant-app/resources/api/v1"
	mux.HandleFunc(base+"/a2a/agents/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	})
	mux.HandleFunc(base+"/a2a/approval/", func(w http.ResponseWriter, r *http.Request) {
		var resp assistant.ApprovalResponse
		if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
			t.Errorf("failed to decode approval submission: %v", err)
		}
		recorded = append(recorded, resp.Approved)
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return assistant.New(assistant.ClientOptions{
		GrafanaURL: server.URL,
		Token:      "test-token",
		HTTPClient: server.Client(),
	}), &recorded
}

// runStream drives a full prompt-style invocation through the real A2A SSE
// client with the emitter under test, mirroring runPrompt's wiring.
func runStream(t *testing.T, em *streamEmitter, client *assistant.Client) error {
	t.Helper()
	// Redirect the last-context-id state file away from the real home.
	t.Setenv("HOME", t.TempDir())
	streamOpts := assistant.StreamOptions{Timeout: 30, OnEvent: em.onEvent()}
	result := client.ChatWithApproval(t.Context(), "hello", streamOpts, em.approvalHandler(nil))
	return em.finish(result, 30)
}

// TestAgentModeStreamIsTypedJSONL is the agent-output-contract test for the
// A2A stream: in agent mode every stdout line is independently parseable
// JSON with a type discriminator and schema version, the domain event kinds
// ride in "event" with the payload fields verbatim, and the stream ends with
// a terminal gcx.stream_end success line. The interactive approval prompt is
// replaced by a non-blocking auto-decline with a typed stderr warning.
func TestAgentModeStreamIsTypedJSONL(t *testing.T) {
	setAgentMode(t, true)
	client, approvals := newSSEFixture(t, sseBody(sseStatusWorking, sseToolCall, sseApproval, sseMessage, sseStatusCompleted))

	var stdout, stderr bytes.Buffer
	em := newStreamEmitter(&stdout, &stderr, &promptOpts{})
	require.Equal(t, modeAgent, em.mode)

	require.NoError(t, runStream(t, em, client))

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	require.Len(t, lines, 6, "five events plus the terminal line; got stdout:\n%s", stdout.String())

	// Every line must be one independently parseable JSON value.
	docs := make([]map[string]any, len(lines))
	for i, line := range lines {
		require.NoError(t, json.Unmarshal([]byte(line), &docs[i]), "line %d is not valid JSON: %s", i, line)
	}

	wantEvents := []string{"status", "tool_call", "approval", "message", "status"}
	for i, want := range wantEvents {
		assert.Equal(t, StreamEventType, docs[i]["type"], "line %d discriminator", i)
		assert.Equal(t, "1", docs[i]["schema_version"], "line %d schema version", i)
		assert.Equal(t, want, docs[i]["event"], "line %d domain event kind", i)
		assert.Equal(t, "task-1", docs[i]["taskId"], "line %d payload taskId", i)
	}
	assert.Equal(t, "Here is the answer", docs[3]["text"], "message payload text is verbatim")
	assert.Equal(t, "write_dashboard", docs[2]["toolName"], "approval payload toolName is verbatim")

	terminal := docs[len(docs)-1]
	assert.Equal(t, StreamEndType, terminal["type"])
	assert.Equal(t, "1", terminal["schema_version"])
	assert.Equal(t, true, terminal["ok"])
	assert.NotContains(t, terminal, "error")

	// The approval was auto-declined without blocking: the backend received
	// approved=false and stderr carries a typed warning record.
	require.Equal(t, []bool{false}, *approvals, "agent mode must submit an explicit decline")
	var warn map[string]any
	require.NoError(t, json.Unmarshal(stderr.Bytes(), &warn), "agent-mode stderr diagnostic must be JSONL: %s", stderr.String())
	assert.Equal(t, "warning", warn["class"])
	assert.Contains(t, warn["summary"], "write_dashboard")
	assert.Contains(t, warn["summary"], "auto-declined")
}

// TestHumanModeStreamUnchanged pins the default human output: prose response
// block on stdout, byte-identical to the pre-migration rendering.
func TestHumanModeStreamUnchanged(t *testing.T) {
	setAgentMode(t, false)
	client, _ := newSSEFixture(t, sseBody(sseStatusWorking, sseMessage, sseStatusCompleted))

	var stdout, stderr bytes.Buffer
	em := newStreamEmitter(&stdout, &stderr, &promptOpts{})
	require.Equal(t, modeHuman, em.mode)

	require.NoError(t, runStream(t, em, client))

	assert.Equal(t, "\n--- Response ---\n\nHere is the answer\n\n----------------\n", stdout.String())
	assert.Contains(t, stderr.String(), "Completed!")
}

// TestJSONStreamModeUnchanged pins the legacy --json NDJSON event shapes:
// bare StreamEvent lines, no envelope, no terminal line on success.
func TestJSONStreamModeUnchanged(t *testing.T) {
	setAgentMode(t, true) // --json must win even in agent mode
	client, _ := newSSEFixture(t, sseBody(sseStatusWorking, sseMessage, sseStatusCompleted))

	var stdout, stderr bytes.Buffer
	em := newStreamEmitter(&stdout, &stderr, &promptOpts{jsonOut: true})
	require.Equal(t, modeJSONStream, em.mode)

	require.NoError(t, runStream(t, em, client))

	// Byte-level comparison on purpose: the legacy --json NDJSON shape is a
	// shipped machine interface and must stay byte-identical.
	wantLines := []string{
		`{"type":"status","taskId":"task-1","contextId":"ctx-1","state":"working"}`,
		`{"type":"message","taskId":"task-1","contextId":"ctx-1","text":"Here is the answer"}`,
		`{"type":"status","taskId":"task-1","contextId":"ctx-1","state":"completed","final":true}`,
	}
	assert.Equal(t, wantLines, strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n"))
}

// TestJSONDocModeUnchanged pins the legacy --json --no-stream single
// document on success.
func TestJSONDocModeUnchanged(t *testing.T) {
	setAgentMode(t, false)
	client, _ := newSSEFixture(t, sseBody(sseStatusWorking, sseMessage, sseStatusCompleted))

	var stdout, stderr bytes.Buffer
	em := newStreamEmitter(&stdout, &stderr, &promptOpts{jsonOut: true, noStream: true})
	require.Equal(t, modeJSONDoc, em.mode)

	require.NoError(t, runStream(t, em, client))

	want, err := json.MarshalIndent(promptResult{
		TaskID:    "task-1",
		ContextID: "ctx-1",
		Status:    "completed",
		Response:  "Here is the answer",
	}, "", "  ")
	require.NoError(t, err)
	assert.Equal(t, string(want)+"\n", stdout.String())
}

// requireEmittedGeneralError asserts err carries the already-emitted sentinel
// with the general-error exit code, so the top-level reporter exits non-zero
// without appending a second document to stdout.
func requireEmittedGeneralError(t *testing.T, err error) {
	t.Helper()
	requireEmittedCode(t, err, gcxerrors.ExitGeneralError)
}

// requireEmittedCode asserts err carries the already-emitted sentinel with
// the given exit code.
func requireEmittedCode(t *testing.T, err error, code int) {
	t.Helper()
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, code, emitted.Code)
}

// TestFinishFailureOutcomesPerMode is the terminal-outcome matrix: for every
// non-completed stream result, each machine mode writes its complete terminal
// output and returns an EmittedError (exit code without a second stdout
// document), while human mode keeps stdout empty and returns the bare error.
func TestFinishFailureOutcomesPerMode(t *testing.T) {
	result := assistant.StreamResult{TaskID: "task-1", ContextID: "ctx-1"}

	outcomes := []struct {
		name          string
		result        assistant.StreamResult
		reason        string
		wantErr       string
		wantAgentCode int
	}{
		{
			name:          "timeout",
			result:        func() assistant.StreamResult { r := result; r.TimedOut = true; return r }(),
			reason:        "timeout",
			wantErr:       "request timed out after 30s",
			wantAgentCode: gcxerrors.ExitGeneralError,
		},
		{
			name: "failed",
			result: func() assistant.StreamResult {
				r := result
				r.Failed = true
				r.ErrorMessage = "boom"
				return r
			}(),
			reason:        "failed",
			wantErr:       "request failed: boom",
			wantAgentCode: gcxerrors.ExitGeneralError,
		},
		{
			// Cancellation carries ExitCancelled in agent mode, matching the
			// repo-wide convention; the legacy --json modes keep exit 1.
			name:          "canceled",
			result:        func() assistant.StreamResult { r := result; r.Canceled = true; return r }(),
			reason:        "canceled",
			wantErr:       "request was canceled",
			wantAgentCode: gcxerrors.ExitCancelled,
		},
		{
			name:          "unknown",
			result:        result,
			reason:        "unknown",
			wantErr:       "request ended in unknown state",
			wantAgentCode: gcxerrors.ExitGeneralError,
		},
	}

	for _, oc := range outcomes {
		t.Run("agent/"+oc.name, func(t *testing.T) {
			setAgentMode(t, true)
			var stdout, stderr bytes.Buffer
			em := newStreamEmitter(&stdout, &stderr, &promptOpts{})

			err := em.finish(oc.result, 30)
			requireEmittedCode(t, err, oc.wantAgentCode)
			require.ErrorContains(t, err, oc.wantErr)

			// Exactly one terminal JSON line on stdout.
			lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
			require.Len(t, lines, 1)
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(lines[0]), &doc))
			assert.Equal(t, StreamEndType, doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, false, doc["ok"])
			errObj, ok := doc["error"].(map[string]any)
			require.True(t, ok, "terminal error object required: %s", lines[0])
			assert.Equal(t, oc.reason, errObj["reason"])
			assert.NotEmpty(t, errObj["summary"])
			assert.InDelta(t, float64(oc.wantAgentCode), errObj["exitCode"], 0)
		})

		t.Run("human/"+oc.name, func(t *testing.T) {
			setAgentMode(t, false)
			var stdout, stderr bytes.Buffer
			em := newStreamEmitter(&stdout, &stderr, &promptOpts{})

			err := em.finish(oc.result, 30)
			require.EqualError(t, err, oc.wantErr)
			var emitted *gcxerrors.EmittedError
			assert.NotErrorAs(t, err, &emitted, "human mode must return the bare error so the reporter renders it on stderr")
			assert.Empty(t, stdout.String(), "human failure output goes to stderr only")
			assert.NotEmpty(t, stderr.String())
		})

		t.Run("json-doc/"+oc.name, func(t *testing.T) {
			setAgentMode(t, false)
			var stdout, stderr bytes.Buffer
			em := newStreamEmitter(&stdout, &stderr, &promptOpts{jsonOut: true, noStream: true})

			err := em.finish(oc.result, 30)
			requireEmittedGeneralError(t, err)

			var doc map[string]any
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &doc), "exactly one JSON document expected: %s", stdout.String())
			assert.Equal(t, oc.reason, doc["status"], "legacy status naming preserved")
		})
	}
}

// TestFinishJSONStreamFailureShapes pins the legacy --json NDJSON terminal
// error behavior per outcome, now with the exit code carried by EmittedError
// instead of a second JSON document appended by the top-level reporter.
func TestFinishJSONStreamFailureShapes(t *testing.T) {
	setAgentMode(t, false)

	// Byte-level pins on purpose: the legacy --json NDJSON error events are a
	// shipped machine interface and must stay byte-identical.
	t.Run("timeout emits legacy error event", func(t *testing.T) {
		var stdout bytes.Buffer
		em := newStreamEmitter(&stdout, &bytes.Buffer{}, &promptOpts{jsonOut: true})
		err := em.finish(assistant.StreamResult{TimedOut: true}, 5)
		requireEmittedGeneralError(t, err)
		assert.Equal(t,
			[]string{`{"type":"error","error":"request timed out after 5s","timeout":5}`, ""},
			strings.Split(stdout.String(), "\n"))
	})

	t.Run("failed emits legacy error event when not already streamed", func(t *testing.T) {
		var stdout bytes.Buffer
		em := newStreamEmitter(&stdout, &bytes.Buffer{}, &promptOpts{jsonOut: true})
		err := em.finish(assistant.StreamResult{Failed: true, ErrorMessage: "boom", TaskID: "task-1", ContextID: "ctx-1"}, 5)
		requireEmittedGeneralError(t, err)
		assert.Equal(t,
			[]string{`{"type":"error","taskId":"task-1","contextId":"ctx-1","error":"boom"}`, ""},
			strings.Split(stdout.String(), "\n"))
	})

	t.Run("failed emits nothing when error event already streamed", func(t *testing.T) {
		var stdout bytes.Buffer
		em := newStreamEmitter(&stdout, &bytes.Buffer{}, &promptOpts{jsonOut: true})
		err := em.finish(assistant.StreamResult{Failed: true, ErrorMessage: "boom", ErrorEventEmitted: true}, 5)
		requireEmittedGeneralError(t, err)
		assert.Empty(t, stdout.String())
	})

	t.Run("canceled emits nothing extra", func(t *testing.T) {
		var stdout bytes.Buffer
		em := newStreamEmitter(&stdout, &bytes.Buffer{}, &promptOpts{jsonOut: true})
		err := em.finish(assistant.StreamResult{Canceled: true}, 5)
		requireEmittedGeneralError(t, err)
		assert.Empty(t, stdout.String())
	})

	t.Run("unknown emits legacy error event", func(t *testing.T) {
		var stdout bytes.Buffer
		em := newStreamEmitter(&stdout, &bytes.Buffer{}, &promptOpts{jsonOut: true})
		err := em.finish(assistant.StreamResult{}, 5)
		requireEmittedGeneralError(t, err)
		assert.Equal(t,
			[]string{`{"type":"error","error":"stream ended unexpectedly"}`, ""},
			strings.Split(stdout.String(), "\n"))
	})
}

// failingWriter fails every write with err, counting attempts.
type failingWriter struct {
	err    error
	writes int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, w.err
}

// requireBareWriteError asserts err is exactly the write failure — surfaced,
// and NOT wrapped in the EmittedError sentinel (the terminal output never
// reached stdout, so nothing was emitted).
func requireBareWriteError(t *testing.T, err, writeErr error) {
	t.Helper()
	require.ErrorIs(t, err, writeErr, "the stdout write error must surface")
	var emitted *gcxerrors.EmittedError
	assert.NotErrorAs(t, err, &emitted, "EmittedError may only be returned after a successful terminal write")
}

// TestFinishTerminalWriteFailureSurfaces pins the terminal-write contract for
// every machine mode: when the terminal stdout write fails, finish returns
// the write error — never nil (which would exit 0 on a lost gcx.stream_end)
// and never an EmittedError (which would claim the result reached stdout).
func TestFinishTerminalWriteFailureSurfaces(t *testing.T) {
	writeErr := errors.New("broken pipe")
	completed := assistant.StreamResult{TaskID: "task-1", ContextID: "ctx-1", Completed: true, Response: "hi"}
	failed := assistant.StreamResult{TaskID: "task-1", ContextID: "ctx-1", Failed: true, ErrorMessage: "boom"}

	tests := []struct {
		name      string
		agentMode bool
		opts      promptOpts
		result    assistant.StreamResult
	}{
		{name: "agent completed stream_end", agentMode: true, result: completed},
		{name: "agent failed stream_end", agentMode: true, result: failed},
		{name: "json-doc completed document", opts: promptOpts{jsonOut: true, noStream: true}, result: completed},
		{name: "json-doc failed document", opts: promptOpts{jsonOut: true, noStream: true}, result: failed},
		{name: "json-stream failed error event", opts: promptOpts{jsonOut: true}, result: failed},
		{name: "json-stream timeout error event", opts: promptOpts{jsonOut: true}, result: assistant.StreamResult{TimedOut: true}},
		{name: "agent unknown stream_end", agentMode: true, result: assistant.StreamResult{}},
		{name: "agent canceled stream_end", agentMode: true, result: assistant.StreamResult{Canceled: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, tt.agentMode)
			t.Setenv("HOME", t.TempDir()) // keep SaveLastContextID away from the real home
			em := newStreamEmitter(&failingWriter{err: writeErr}, &bytes.Buffer{}, &tt.opts)

			err := em.finish(tt.result, 30)
			requireBareWriteError(t, err, writeErr)
		})
	}
}

// TestStreamEventWriteFailureAbortsStream pins the non-terminal contract: a
// failed stream-event write records the error, cancels the stream loop, stops
// writing to the dead pipe, and finish surfaces the write error without an
// EmittedError and without attempting a terminal line.
func TestStreamEventWriteFailureAbortsStream(t *testing.T) {
	writeErr := errors.New("broken pipe")

	modes := []struct {
		name      string
		agentMode bool
		opts      promptOpts
	}{
		{name: "agent JSONL", agentMode: true},
		{name: "legacy --json NDJSON", opts: promptOpts{jsonOut: true}},
	}
	for _, tt := range modes {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, tt.agentMode)
			w := &failingWriter{err: writeErr}
			em := newStreamEmitter(w, &bytes.Buffer{}, &tt.opts)
			canceled := false
			em.cancel = func() { canceled = true }

			cb := em.onEvent()
			require.NotNil(t, cb)
			cb(assistant.StreamEvent{Type: "status", TaskID: "task-1", State: "working"})
			assert.True(t, canceled, "the first write failure must abort the stream loop")

			// Subsequent events must not touch the broken writer again.
			cb(assistant.StreamEvent{Type: "message", TaskID: "task-1", Text: "hi"})
			assert.Equal(t, 1, w.writes, "no further writes after the pipe broke")

			// finish surfaces the recorded write error without writing more.
			err := em.finish(assistant.StreamResult{Completed: true, Response: "hi"}, 30)
			requireBareWriteError(t, err, writeErr)
			assert.Equal(t, 1, w.writes, "finish must not attempt a terminal line on a broken stream")
		})
	}
}

func TestStreamEmitterNotice(t *testing.T) {
	tests := []struct {
		name      string
		opts      promptOpts
		agentMode bool
		check     func(t *testing.T, stderr string)
	}{
		{
			name: "human gets prose",
			check: func(t *testing.T, stderr string) {
				t.Helper()
				assert.Contains(t, stderr, "resuming a slack conversation")
			},
		},
		{
			name:      "agent gets typed note",
			agentMode: true,
			check: func(t *testing.T, stderr string) {
				t.Helper()
				var note map[string]any
				require.NoError(t, json.Unmarshal([]byte(stderr), &note), "agent notice must be JSONL: %s", stderr)
				assert.Equal(t, "note", note["class"])
				assert.Contains(t, note["summary"], "resuming a slack conversation")
			},
		},
		{
			name: "legacy json mode stays silent",
			opts: promptOpts{jsonOut: true},
			check: func(t *testing.T, stderr string) {
				t.Helper()
				assert.Empty(t, stderr)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, tt.agentMode)
			var stderr bytes.Buffer
			em := newStreamEmitter(&bytes.Buffer{}, &stderr, &tt.opts)
			em.notice("resuming a slack conversation")
			tt.check(t, stderr.String())
		})
	}
}

func TestStreamEmitterNoticeEmptyIsSilent(t *testing.T) {
	setAgentMode(t, true)
	var stderr bytes.Buffer
	em := newStreamEmitter(&bytes.Buffer{}, &stderr, &promptOpts{})
	em.notice("")
	assert.Empty(t, stderr.String())
}

// TestFinishPersistsContextIDDespiteWriteFailure pins that a completed task's
// context ID survives a broken stdout: the conversation happened whether or
// not the consumer read the tail of the stream, so --continue must keep
// working after a broken pipe.
func TestFinishPersistsContextIDDespiteWriteFailure(t *testing.T) {
	setAgentMode(t, true)
	var stdout, stderr bytes.Buffer
	em := newStreamEmitter(&stdout, &stderr, &promptOpts{})

	var saved string
	em.saveContextID = func(id string) error { saved = id; return nil }
	writeErr := errors.New("broken pipe")
	em.writeErr = writeErr

	err := em.finish(assistant.StreamResult{Completed: true, ContextID: "ctx-42"}, 30)

	require.ErrorIs(t, err, writeErr, "the write error stays the honest outcome")
	assert.Equal(t, "ctx-42", saved, "context ID must persist despite the write failure")
	assert.Empty(t, stdout.String(), "broken stdout carries no further output")
}

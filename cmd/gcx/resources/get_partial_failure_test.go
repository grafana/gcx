package resources_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// These tests pin the atomic-stdout contract for `resources get` partial
// failures. Before the fix the three paths disagreed:
//   - plain output returned a bare fmt.Errorf → exit 1 (taxonomy says 4)
//     and, in agent mode, a SECOND error JSON document appended by
//     reportError after the items document;
//   - the agent-mode --json fused envelope returned nil → process exit 0
//     despite the embedded exitCode 4.
//
// All paths now write exactly one document and return EmittedError with
// ExitPartialFailure.
func TestGetPartialFailure_AtomicStdout(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "dashboard.grafana.app/v1alpha1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "alpha"},
	}}

	tests := []struct {
		name      string
		agentMode bool
		jsonField string // --json value; empty = explicit -o json
	}{
		{name: "plain -o json human mode", agentMode: false},
		{name: "plain agent mode", agentMode: true},
		{name: "field-select human mode", agentMode: false, jsonField: "metadata.name"},
		{name: "field-select agent mode (fused envelope)", agentMode: true, jsonField: "metadata.name"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			flags := pflag.NewFlagSet("get", pflag.ContinueOnError)
			opts := resources.NewGetOptsForTest(flags)
			if tc.jsonField != "" {
				if err := flags.Set("json", tc.jsonField); err != nil {
					t.Fatalf("set --json: %v", err)
				}
			} else if err := flags.Set("output", "json"); err != nil {
				t.Fatalf("set -o json: %v", err)
			}
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}

			summary := &remote.OperationSummary{}
			summary.RecordSuccess()
			summary.RecordFailure(nil, errors.New("boom"))
			res := &resources.FetchResponse{PullSummary: summary}
			output := unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}

			var stdout, stderr bytes.Buffer
			opts.IO.ErrWriter = &stderr
			err := resources.WriteGetOutputForTest(&stdout, &stderr, opts, res, output)

			// The error must be an EmittedError carrying ExitPartialFailure:
			// the document on stdout is complete, and reportError must not
			// append a second one.
			var emitted *gcxerrors.EmittedError
			if !errors.As(err, &emitted) {
				t.Fatalf("writeGetOutput() error = %T (%v), want *gcxerrors.EmittedError", err, err)
			}
			if emitted.Code != gcxerrors.ExitPartialFailure {
				t.Fatalf("EmittedError.Code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
			}

			// stdout must hold exactly one JSON value.
			dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			var first any
			if err := dec.Decode(&first); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
			}
			var second any
			if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
				t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\n%s", err, stdout.String())
			}

			// Every agent-mode JSON-family path — plain and field-select —
			// fuses items and error into one gcx.partial_result document.
			if tc.agentMode {
				doc, ok := first.(map[string]any)
				if !ok {
					t.Fatalf("fused envelope is %T, want object", first)
				}
				if doc["type"] != "gcx.partial_result" {
					t.Fatalf("fused envelope type = %v, want gcx.partial_result", doc["type"])
				}
				if _, ok := doc["items"]; !ok {
					t.Fatal("fused envelope missing items")
				}
				if _, ok := doc["error"]; !ok {
					t.Fatal("fused envelope missing error")
				}
			}

			// Human/non-fused paths surface the failure count as a typed
			// stderr diagnostic (advisory stream), not via a second stdout
			// document.
			if !tc.agentMode {
				if !strings.Contains(stderr.String(), "failed to get") {
					t.Fatalf("stderr = %q, want partial-failure diagnostic", stderr.String())
				}
			}
		})
	}
}

// TestGetPartialFailure_JQKeepsShape pins that an active --jq transformation
// is never dropped by the agent-mode fused envelope: the jq output keeps its
// shape (identical to a success run) and the failure travels via the typed
// stderr diagnostic + EmittedError exit 4, like other explicit formats.
func TestGetPartialFailure_JQKeepsShape(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "dashboard.grafana.app/v1alpha1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "alpha"},
	}}

	flags := pflag.NewFlagSet("get", pflag.ContinueOnError)
	opts := resources.NewGetOptsForTest(flags)
	if err := flags.Set("jq", ".items[].metadata.name"); err != nil {
		t.Fatalf("set --jq: %v", err)
	}
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}

	summary := &remote.OperationSummary{}
	summary.RecordSuccess()
	summary.RecordFailure(nil, errors.New("boom"))
	res := &resources.FetchResponse{PullSummary: summary}
	output := unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}

	var stdout, stderr bytes.Buffer
	opts.IO.ErrWriter = &stderr
	err := resources.WriteGetOutputForTest(&stdout, &stderr, opts, res, output)

	var emitted *gcxerrors.EmittedError
	if !errors.As(err, &emitted) {
		t.Fatalf("writeGetOutput() error = %T (%v), want *gcxerrors.EmittedError", err, err)
	}
	if emitted.Code != gcxerrors.ExitPartialFailure {
		t.Fatalf("EmittedError.Code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
	}

	// stdout must be the jq transformation, not a fused envelope.
	got := strings.TrimSpace(stdout.String())
	if got != `"alpha"` {
		t.Fatalf("stdout = %q, want jq output %q", got, `"alpha"`)
	}
	if !strings.Contains(stderr.String(), "failed to get") {
		t.Fatalf("stderr = %q, want partial-failure diagnostic", stderr.String())
	}
}

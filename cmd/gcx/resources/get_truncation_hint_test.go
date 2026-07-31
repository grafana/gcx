package resources_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestGetTruncationHint: the per-resource-type truncation hint must fire on
// every output path of `resources get` — including --json field selection,
// which returns through writeFieldSelect instead of the shared encode tail.
// Regression test for the #387 Track B correction pass: previously the
// JSONFields branch returned before the hint, so `--json <field>` silently
// dropped the truncation notice.
func TestGetTruncationHint(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "dashboard.grafana.app/v1alpha1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "alpha"},
	}}

	tests := []struct {
		name      string
		agentMode bool
		jsonField string // --json value; empty = explicit -o json output
		truncated bool
		wantHint  bool
	}{
		{
			name:      "json field selection + truncation emits JSONL hint in agent mode",
			agentMode: true,
			jsonField: "metadata.name",
			truncated: true,
			wantHint:  true,
		},
		{
			name:      "json field selection + truncation emits hint-prefixed line on a TTY",
			agentMode: false,
			jsonField: "metadata.name",
			truncated: true,
			wantHint:  true,
		},
		{
			name:      "json field selection without truncation stays silent",
			agentMode: true,
			jsonField: "metadata.name",
			truncated: false,
			wantHint:  false,
		},
		{
			name:      "plain -o json + truncation still emits the hint",
			agentMode: false,
			truncated: true,
			wantHint:  true,
		},
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
				t.Fatalf("Validate() = %v, want nil", err)
			}

			summary := &remote.OperationSummary{}
			if tc.truncated {
				summary.RecordTruncated()
			}
			res := &resources.FetchResponse{PullSummary: summary}
			output := unstructured.UnstructuredList{Items: []unstructured.Unstructured{item}}

			var stdout, stderr bytes.Buffer
			opts.IO.ErrWriter = &stderr
			if err := resources.WriteGetOutputForTest(&stdout, &stderr, opts, res, output); err != nil {
				t.Fatalf("writeGetOutput() = %v, want nil", err)
			}
			if stdout.Len() == 0 {
				t.Fatal("stdout empty, want encoded output")
			}

			if !tc.wantHint {
				if stderr.Len() != 0 {
					t.Fatalf("stderr = %q, want empty (no truncation)", stderr.String())
				}
				return
			}

			line := strings.TrimSpace(stderr.String())
			if line == "" {
				t.Fatal("stderr empty, want truncation hint")
			}
			const wantSummary = "showing first 50 items per resource type; use --limit=0 to fetch all"
			if tc.agentMode {
				var event map[string]any
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatalf("agent-mode hint must be JSONL, got %q (%v)", line, err)
				}
				if event["class"] != "hint" {
					t.Fatalf("hint class = %v, want %q", event["class"], "hint")
				}
				if event["summary"] != wantSummary {
					t.Fatalf("hint summary = %v, want %q", event["summary"], wantSummary)
				}
			} else if line != "hint: "+wantSummary {
				t.Fatalf("TTY hint = %q, want %q", line, "hint: "+wantSummary)
			}
		})
	}
}

//nolint:testpackage // white-box tests require access to the unexported IRM table codecs
package irm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type tableEncoder interface {
	Encode(w io.Writer, v any) error
}

// Order is part of the meaning of an escalation step and of a route, so the
// wide list output must show the position. A caller then reads the current
// order before an update-position call, and verifies the result afterwards,
// without a switch to -o json.
func TestTableCodecsShowPositionInWideOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		table     tableEncoder
		wide      tableEncoder
		items     []unstructured.Unstructured
		wantValue string
	}{
		{
			name:  "escalation policies",
			table: &escalationPolicyTableCodec{},
			wide:  &escalationPolicyTableCodec{Wide: true},
			items: []unstructured.Unstructured{{Object: map[string]any{
				"metadata": map[string]any{"name": "EP1"},
				"spec": map[string]any{
					"escalation_chain": "C1",
					"step":             "notify_persons",
					"position":         2,
				},
			}}},
			wantValue: "2",
		},
		{
			name:  "routes",
			table: &routeTableCodec{},
			wide:  &routeTableCodec{Wide: true},
			items: []unstructured.Unstructured{{Object: map[string]any{
				"metadata": map[string]any{"name": "R1"},
				"spec": map[string]any{
					"alert_receive_channel": "INT1",
					"position":              3,
				},
			}}},
			wantValue: "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var narrow bytes.Buffer
			if err := tt.table.Encode(&narrow, tt.items); err != nil {
				t.Fatalf("table codec encode failed: %v", err)
			}
			if strings.Contains(narrow.String(), "POSITION") {
				t.Errorf("the narrow table shows POSITION: %q", narrow.String())
			}

			var buf bytes.Buffer
			if err := tt.wide.Encode(&buf, tt.items); err != nil {
				t.Fatalf("wide codec encode failed: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, "POSITION") {
				t.Errorf("the wide table misses the POSITION column: %q", out)
			}
			if !strings.Contains(out, tt.wantValue) {
				t.Errorf("the wide table misses the position value %q: %q", tt.wantValue, out)
			}
		})
	}
}

package gcxerrors_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestDetailedErrorWriteNotice(t *testing.T) {
	cases := []struct {
		name string
		err  gcxerrors.DetailedError
		want map[string]any
	}{
		{
			name: "summary only",
			err:  gcxerrors.DetailedError{Summary: "Resource not found - code 404"},
			want: map[string]any{"class": "error", "summary": "Resource not found - code 404", "exitCode": float64(1)},
		},
		{
			name: "details and first suggestion",
			err: gcxerrors.DetailedError{
				Summary:     "Invalid filter",
				Details:     "invalid character 's' looking for beginning of value",
				Suggestions: []string{`use --filter '{"field":"status","op":"eq","value":502}'`, "second"},
			},
			want: map[string]any{
				"class":      "error",
				"summary":    "Invalid filter",
				"exitCode":   float64(1),
				"details":    "invalid character 's' looking for beginning of value",
				"suggestion": `use --filter '{"field":"status","op":"eq","value":502}'`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tc.err.WriteNotice(&buf, 1); err != nil {
				t.Fatal(err)
			}
			if strings.Count(buf.String(), "\n") != 1 {
				t.Fatalf("notice must be one line, got %q", buf.String())
			}
			var got map[string]any
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("notice is not JSON: %v; %q", err, buf.String())
			}
			if len(got) != len(tc.want) {
				t.Fatalf("notice = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("notice[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestWriteNoticeClipsLongDetails(t *testing.T) {
	var buf bytes.Buffer
	if err := gcxerrors.WriteNotice(&buf, "failed", strings.Repeat("x", 2000), nil, 1); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	details, ok := got["details"].(string)
	if !ok {
		t.Fatalf("details = %v, want a string", got["details"])
	}
	if n := len([]rune(details)); n != 500 {
		t.Fatalf("clipped details length = %d, want 500", n)
	}
	if !strings.HasSuffix(details, "…") {
		t.Fatalf("clipped details = %q, want an ellipsis suffix", details)
	}
}

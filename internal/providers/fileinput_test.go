package providers_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/grafana/gcx/internal/providers"
)

type scheduleDoc struct {
	Name     string `json:"name"`
	Type     any    `json:"type,omitempty"`
	TimeZone string `json:"time_zone,omitempty"`
	Spec     any    `json:"spec,omitempty"`
}

func TestReadFileOrStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    scheduleDoc
		wantErr string
	}{
		{
			name: "bare object",
			input: `name: my schedule
type: 2
time_zone: Europe/Amsterdam
`,
			want: scheduleDoc{Name: "my schedule", Type: float64(2), TimeZone: "Europe/Amsterdam"},
		},
		{
			name: "kubernetes envelope",
			input: `apiVersion: oncall.ext.grafana.app/v1alpha1
kind: Schedule
metadata:
  name: probe
spec:
  name: my schedule
  type: 2
  time_zone: Europe/Amsterdam
`,
			want: scheduleDoc{Name: "my schedule", Type: float64(2), TimeZone: "Europe/Amsterdam"},
		},
		{
			name:  "json envelope",
			input: `{"apiVersion":"oncall.ext.grafana.app/v1alpha1","kind":"Schedule","spec":{"name":"my schedule","type":"web"}}`,
			want:  scheduleDoc{Name: "my schedule", Type: "web"},
		},
		{
			name: "envelope without kind or apiVersion keeps the whole document",
			input: `name: my schedule
spec:
  name: inner
`,
			want: scheduleDoc{Name: "my schedule", Spec: map[string]any{"name": "inner"}},
		},
		{
			name: "envelope with a null spec keeps the whole document",
			input: `kind: Schedule
name: my schedule
spec: null
`,
			want: scheduleDoc{Name: "my schedule"},
		},
		{
			name: "envelope with a non-object spec keeps the whole document",
			input: `kind: Schedule
name: my schedule
spec: plain
`,
			want: scheduleDoc{Name: "my schedule", Spec: "plain"},
		},
		{
			name:    "empty input",
			input:   "   \n",
			wantErr: "input is empty",
		},
		{
			name:    "malformed input",
			input:   "name: [unterminated",
			wantErr: "failed to parse input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got scheduleDoc
			err := providers.ReadFileOrStdin("-", strings.NewReader(tt.input), &got)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ReadFileOrStdin() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadFileOrStdin() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadFileOrStdin() error = %v, want nil", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ReadFileOrStdin() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
